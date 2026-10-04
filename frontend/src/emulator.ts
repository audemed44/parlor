// The mGBA core (WebAssembly), for GBA games. It's created once per page
// with its own canvas, which the player moves into place, and reused from
// game to game.
import type { mGBAEmulator } from "@thenick775/mgba-wasm";
import type { Key } from "./controls";
import { keyboard, type Core, type Overrides } from "./core";

let canvas: HTMLCanvasElement | null = null;
let core: Promise<mGBAEmulator> | null = null;

export function screen(): HTMLCanvasElement {
  if (!canvas) {
    canvas = document.createElement("canvas");
    canvas.width = 240;
    canvas.height = 160;
    canvas.className = "screen";
    // The core listens for keys on the page; keep it from stealing taps.
    canvas.tabIndex = -1;
  }
  return canvas;
}

// emulator loads the core on first use. It needs cross-origin isolation
// (threads share memory).
function emulator(): Promise<mGBAEmulator> {
  if (!core) {
    if (!self.crossOriginIsolated) {
      return Promise.reject(
        new Error("This page isn't cross-origin isolated, so the emulator can't start."),
      );
    }
    core = (async () => {
      const url = `/core/${__CORE_VERSION__}/mgba.js`;
      const mod = await import(/* @vite-ignore */ url);
      const m: mGBAEmulator = await mod.default({ canvas: screen() });
      await m.FSInit();
      m.setCoreSettings({
        // Saves live on the server. Local auto-states could bring back old
        // progress on top of a newer save from another device.
        autoSaveStateEnable: false,
        restoreAutoSaveStateOnLoad: false,
        rewindEnable: false,
      });
      m.toggleInput(false);
      return m;
    })();
    core.catch(() => (core = null));
  }
  return core;
}

const romPath = (id: number) => `/data/games/parlor-${id}.gba`;
const savePath = (id: number) => `/data/saves/parlor-${id}.sav`;
// mGBA names a slot's state after the ROM: <ROM name>.ss<slot>.
const statePath = (id: number, slot: number) => `/data/states/parlor-${id}.ss${slot}`;
// mGBA reads per-game overrides from its config file when a game loads.
const configPath = "/home/web_user/.config/mgba/config.ini";
const scratch = ["/data/games", "/data/saves", "/data/states"];

function remove(m: mGBAEmulator, path: string) {
  if (m.FS.analyzePath(path).exists) m.FS.unlink(path);
}

function clear(m: mGBAEmulator) {
  for (const dir of scratch) {
    for (const f of m.FS.readdir(dir)) if (f !== "." && f !== "..") remove(m, `${dir}/${f}`);
  }
}

// gameCode is the four-letter code in the ROM header (BPEE for Emerald),
// which mGBA keys overrides by; ROM hacks keep their base game's.
export function gameCode(rom: Uint8Array): string {
  const code = String.fromCharCode(...rom.subarray(0xac, 0xb0));
  return /^[A-Z0-9]{4}$/.test(code) ? code : "";
}

// overrideConfig is the config file that applies overrides to a game.
export function overrideConfig(code: string, o: Overrides): string {
  if (!code || (!o.saveType && !o.rtc)) return "";
  let out = `[override.${code}]\n`;
  if (o.saveType) out += `savetype=${o.saveType}\n`;
  if (o.rtc) out += `hardware=${o.rtc === "on" ? 1 : 0}\n`;
  return out;
}

function configure(m: mGBAEmulator, rom: Uint8Array, o: Overrides) {
  const text = overrideConfig(gameCode(rom), o);
  m.FS.mkdirTree(configPath.slice(0, configPath.lastIndexOf("/")));
  m.FS.writeFile(configPath, text);
}

// start loads a game with its save (null for a new game). It's synchronous
// so it can run inside the tap that starts the game, which iOS requires for
// sound.
function start(
  m: mGBAEmulator,
  id: number,
  rom: Uint8Array,
  save: Uint8Array | null,
  overrides: Overrides,
) {
  // Only one game's files at a time: ROMs are big, and this is memory.
  clear(m);
  configure(m, rom, overrides);
  m.FS.writeFile(romPath(id), rom);
  if (save) m.FS.writeFile(savePath(id), save);
  if (!m.loadGame(romPath(id), savePath(id))) throw new Error("mGBA couldn't load this ROM");
  m.toggleInput(true);
  resumeAudio(m);
}

// replaceSave restarts the running game from another save. The ROM is
// still in place from start.
function replaceSave(m: mGBAEmulator, id: number, save: Uint8Array) {
  m.quitGame();
  m.FS.writeFile(savePath(id), save);
  if (!m.loadGame(romPath(id), savePath(id))) throw new Error("mGBA couldn't reload the game");
  resumeAudio(m);
}

function stop(m: mGBAEmulator) {
  m.toggleInput(false);
  m.quitGame();
  clear(m);
}

// captureState takes a save state of the running game: a PNG of the screen
// with the state inside, which is how mGBA writes them.
function captureState(m: mGBAEmulator, id: number, slot: number): Uint8Array | null {
  const path = statePath(id, slot);
  try {
    if (!m.saveState(slot)) return null;
    return m.FS.readFile(path);
  } finally {
    remove(m, path);
  }
}

// afterStart runs f once the game is running. mGBA starts its emulation
// thread on the tick after a game loads, and starting resets the machine,
// so a state loaded before then would be lost.
function afterStart(m: mGBAEmulator, f: () => void) {
  let done = false;
  m.addCoreCallbacks({
    videoFrameEndedCallback: () => {
      if (done) return;
      done = true;
      // Not from inside the core's callback.
      setTimeout(() => {
        m.addCoreCallbacks({ videoFrameEndedCallback: null });
        f();
      });
    },
  });
}

// restoreState loads a save state into the running game. The game keeps
// its in-game save (the server's), so a state never rolls a save back.
function restoreState(m: mGBAEmulator, id: number, slot: number, data: Uint8Array) {
  const path = statePath(id, slot);
  m.FS.writeFile(path, data);
  // mGBA's thread must be stopped while a state loads.
  m.pauseGame();
  try {
    // The default flags restore everything except the save data.
    return m.loadStateSlot(slot);
  } finally {
    remove(m, path);
  }
}

// resumeAudio wakes the audio context; iOS suspends it until a tap and
// after interruptions.
function resumeAudio(m: mGBAEmulator) {
  const ctx = m.SDL2?.audioContext;
  if (ctx && ctx.state !== "running") ctx.resume().catch(() => {});
}

// SDL's names for the keyboard keys Parlor uses.
function sdlName(code: string): string {
  if (code.startsWith("Key")) return code.slice(3);
  if (code.startsWith("Arrow")) return code.slice(5);
  return code === "Enter" ? "Return" : code;
}

const gbaKeys: Key[] = ["A", "B", "L", "R", "Start", "Select", "Up", "Down", "Left", "Right"];

// mgba is the core as the player uses it, with the game's ID naming its
// files.
export async function mgba(id: number): Promise<Core> {
  const m = await emulator();
  return {
    screen: screen(),
    reusable: true,
    start(rom, save, overrides) {
      for (const [code, key] of keyboard(gbaKeys)) m.bindKey(sdlName(code), key);
      start(m, id, rom, save, overrides);
    },
    afterStart: (f) => afterStart(m, f),
    // Loading a game resets the core's callbacks, so this follows every
    // load.
    onSave: (f) => m.addCoreCallbacks({ saveDataUpdatedCallback: f }),
    getSave: () => m.getSave(),
    replaceSave: (save) => replaceSave(m, id, save),
    captureState: async (slot) => captureState(m, id, slot),
    restoreState: async (slot, data) => restoreState(m, id, slot, data),
    press: (key, down) => (down ? m.buttonPress(key) : m.buttonUnpress(key)),
    pause: () => m.pauseGame(),
    resume: () => m.resumeGame(),
    setSpeed: (n) => m.setFastForwardMultiplier(n),
    setVolume: (v) => m.setVolume(v),
    resumeAudio: () => resumeAudio(m),
    stop: () => stop(m),
  };
}
