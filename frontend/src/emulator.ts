// The mGBA core (WebAssembly). It's created once per page with its own
// canvas, which the player moves into place, and reused from game to game.
import type { mGBAEmulator } from "@thenick775/mgba-wasm";

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
export function emulator(): Promise<mGBAEmulator> {
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

function remove(m: mGBAEmulator, path: string) {
  if (m.FS.analyzePath(path).exists) m.FS.unlink(path);
}

// start loads a game with its save (null for a new game). It's synchronous
// so it can run inside the tap that starts the game, which iOS requires for
// sound.
export function start(m: mGBAEmulator, id: number, rom: Uint8Array, save: Uint8Array | null) {
  // Only one game's files at a time: ROMs are big, and this is memory.
  for (const dir of ["/data/games", "/data/saves"]) {
    for (const f of m.FS.readdir(dir)) if (f !== "." && f !== "..") remove(m, `${dir}/${f}`);
  }
  m.FS.writeFile(romPath(id), rom);
  if (save) m.FS.writeFile(savePath(id), save);
  if (!m.loadGame(romPath(id), savePath(id))) throw new Error("mGBA couldn't load this ROM");
  m.toggleInput(true);
  resumeAudio(m);
}

// replaceSave restarts the running game from another save. The ROM is
// still in place from start.
export function replaceSave(m: mGBAEmulator, id: number, save: Uint8Array) {
  m.quitGame();
  m.FS.writeFile(savePath(id), save);
  if (!m.loadGame(romPath(id), savePath(id))) throw new Error("mGBA couldn't reload the game");
  resumeAudio(m);
}

export function stop(m: mGBAEmulator) {
  m.toggleInput(false);
  m.quitGame();
  for (const dir of ["/data/games", "/data/saves"]) {
    for (const f of m.FS.readdir(dir)) if (f !== "." && f !== "..") remove(m, `${dir}/${f}`);
  }
}

// resumeAudio wakes the audio context; iOS suspends it until a tap and
// after interruptions.
export function resumeAudio(m: mGBAEmulator) {
  const ctx = m.SDL2?.audioContext;
  if (ctx && ctx.state !== "running") ctx.resume().catch(() => {});
}
