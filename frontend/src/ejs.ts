// EmulatorJS, for every console but the GBA: it unpacks a RetroArch core
// and runs it. Parlor keeps its own controls, saves and states, so
// EmulatorJS's interface is hidden and it's driven from here:
//
//   - the ROM goes straight into the core's file system (one copy in
//     memory, which matters for a 280 MiB DS game on a phone), with the
//     server's save beside it, before the game starts;
//   - its saves folder lives in memory, not in IndexedDB, so a local copy
//     can never bring old progress back;
//   - its keyboard and controller handling is off; Parlor's sends input.
//
// An EmulatorJS game can't be swapped for another, so leaving one reloads
// the page (Core.reusable).
import type { Key } from "./controls";
import { keyboard, type Core } from "./core";
import { isPNG, unwrap, wrap } from "./png";
import type { System } from "./systems";

const base = `/ejs/${__EJS_VERSION__}/`;

// RetroArch's joypad buttons, as EmulatorJS numbers them.
const buttons: Record<Key, number> = {
  B: 0,
  Y: 1,
  Select: 2,
  Start: 3,
  Up: 4,
  Down: 5,
  Left: 6,
  Right: 7,
  A: 8,
  X: 9,
  L: 10,
  R: 11,
};

// Core options: the DS's screens one above the other, taps as touches.
const options: Record<string, Record<string, string>> = {
  melonds: { melonds_screen_layout: "Top/Bottom", melonds_touch_mode: "Touch" },
};

interface FS {
  writeFile(path: string, data: Uint8Array, opts?: { canOwn?: boolean }): void;
  mkdirTree(path: string): void;
  filesystems: Record<string, unknown>;
}
interface GameManager {
  FS: FS;
  getState(): Uint8Array;
  loadState(state: Uint8Array): void;
  screenshot(): Promise<Uint8Array>;
  simulateInput(player: number, index: number, value: number): void;
  getSaveFile(save?: boolean): Uint8Array | null;
  getSaveFilePath(): string;
  loadSaveFiles(): void;
  restart(): void;
  setFastForwardRatio(ratio: number): void;
  toggleFastForward(active: number): void;
  getFrameNum(): number;
}
interface EJS {
  gameManager: GameManager;
  Module: { FS: FS; AL?: { currentCtx?: { audioCtx?: AudioContext } } };
  config: Record<string, unknown>;
  canvas: HTMLCanvasElement;
  started: boolean;
  failedToStart: boolean;
  download(url: unknown, type: unknown): Promise<unknown>;
  downloadFiles(): void;
  on(event: string, f: (data?: unknown) => void): void;
  pause(dontUpdate?: boolean): void;
  play(dontUpdate?: boolean): void;
  setVolume(volume: number): void;
}
type EJSClass = { new (element: string, config: Record<string, unknown>): EJS; prototype: object };

let loaded: Promise<EJSClass> | null = null;

// load fetches EmulatorJS and its stylesheet (which sizes the canvas), and
// switches off what Parlor does itself.
function load(): Promise<EJSClass> {
  if (!loaded) {
    loaded = (async () => {
      const css = document.createElement("link");
      css.rel = "stylesheet";
      css.href = `${base}emulator.min.css`;
      document.head.appendChild(css);
      const mod = await import(/* @vite-ignore */ `${base}emulator.min.js`);
      const C = mod.default as EJSClass;
      const p = C.prototype as Record<string, unknown>;
      const nothing = () => {};
      // Its own input (Parlor's sends it), update check (a request to its
      // site) and "tap to resume sound" popup (Parlor resumes on any tap).
      p.keyChange = nothing;
      p.gamepadEvent = nothing;
      p.checkForUpdates = nothing;
      p.checkStarted = nothing;
      return C;
    })();
    loaded.catch(() => (loaded = null));
  }
  return loaded;
}

// ejs gets a console's core ready: downloaded, unpacked and waiting for
// the game. The returned core starts it.
export async function ejs(sys: System): Promise<Core> {
  const info = sys.ejs!;
  if (!self.crossOriginIsolated) {
    throw new Error("This page isn't cross-origin isolated, so the emulator can't start.");
  }
  const C = await load();
  const holder = document.createElement("div");
  holder.className = "ejs-screen";
  const inner = document.createElement("div");
  inner.id = "parlor-ejs";
  holder.appendChild(inner);
  // Off screen until the player places it.
  holder.style.position = "fixed";
  holder.style.left = "-10000px";
  holder.style.width = "512px";
  holder.style.height = "480px";
  document.body.appendChild(holder);

  const emu = new C("#parlor-ejs", {
    gameUrl: "parlor-rom",
    dataPath: base,
    system: info.system,
    startOnLoad: true,
    disableDatabases: true,
    disableLocalStorage: true,
    cacheConfig: { enabled: false },
    noAutoFocus: true,
    defaultOptions: options[info.core] ?? {},
    buttonOpts: {},
    volume: 1,
  });
  (emu as unknown as { gamepad?: { terminate(): void } }).gamepad?.terminate();

  // The game starts once start() hands the ROM over; until then
  // EmulatorJS waits in its download of it.
  let give: (files: { files: { filename: string }[] }) => void = () => {};
  let waited = 0;
  const ready = new Promise<void>((resolve, reject) => {
    const download = emu.download.bind(emu);
    emu.download = (url, type) => {
      if (url !== "parlor-rom") return download(url, type);
      window.clearInterval(waited);
      resolve();
      return new Promise((r) => (give = r));
    };
    // Saves in memory only: swap IndexedDB's file system for the memory
    // one before EmulatorJS mounts its saves folder.
    const files = emu.downloadFiles.bind(emu);
    emu.downloadFiles = () => {
      const fs = emu.Module.FS.filesystems;
      fs.IDBFS = fs.MEMFS;
      files();
    };
    waited = window.setInterval(() => {
      if (emu.failedToStart) {
        window.clearInterval(waited);
        reject(new Error("EmulatorJS couldn't load the core."));
      }
    }, 250);
  });
  await ready.catch((e) => {
    holder.remove();
    throw e;
  });

  const gm = () => emu.gameManager;
  const savePath = `/data/saves/${info.saveDir}/game.${info.saveExt}`;
  let started = false;
  let onStart: (() => void)[] = [];
  emu.on("start", () => {
    started = true;
    for (const f of onStart) f();
    onStart = [];
  });
  // RetroArch scales a mouse position on the canvas by the pixel ratio,
  // as if the canvas had one pixel per device pixel; EmulatorJS gives it
  // one per CSS pixel. Every mouse event goes in corrected, or a tap on a
  // phone (3×) lands three times as far from the corner.
  const mouse = (type: string, x: number, y: number, buttons: number) => {
    const c = emu.canvas;
    const r = c.getBoundingClientRect();
    const k = r.width ? c.width / (r.width * devicePixelRatio) : 1;
    const at = { clientX: r.left + (x - r.left) * k, clientY: r.top + (y - r.top) * k };
    c.dispatchEvent(new MouseEvent(type, { ...at, button: 0, buttons, bubbles: true }));
  };
  for (const type of ["mousedown", "mousemove", "mouseup"] as const) {
    emu.canvas.addEventListener(
      type,
      (e: MouseEvent) => {
        if (!e.isTrusted) return;
        e.stopImmediatePropagation();
        mouse(type, e.clientX, e.clientY, e.buttons);
      },
      true,
    );
  }
  let saveTimer = 0;
  // RetroArch takes screenshots on a frame it runs, so pausing waits for
  // one first; a state taken while paused uses it.
  let paused = false;
  let pauseShot: Promise<Uint8Array | null> = Promise.resolve(null);
  const shot = () =>
    Promise.race([
      gm()
        .screenshot()
        .catch(() => null),
      new Promise<null>((r) => setTimeout(() => r(null), 1500)),
    ]);
  let keys: ((e: KeyboardEvent) => void) | null = null;
  const map = new Map(keyboard(sys.keys));

  return {
    screen: holder,
    reusable: false,
    start(rom, save) {
      const fs = gm().FS;
      const name = `game${extension(sys)}`;
      // canOwn: the file system keeps this array instead of copying it.
      fs.writeFile(`/${name}`, rom, { canOwn: true });
      if (save) {
        fs.mkdirTree(savePath.slice(0, savePath.lastIndexOf("/")));
        fs.writeFile(savePath, save);
      }
      give({ files: [{ filename: name }] });
      holder.style.position = "";
      holder.style.left = "";
      holder.style.width = "";
      holder.style.height = "";
      keys = (e) => {
        const k = map.get(e.code);
        if (!k || e.repeat || e.metaKey || e.ctrlKey || e.altKey) return;
        e.preventDefault();
        if (started) gm().simulateInput(0, buttons[k], e.type === "keydown" ? 1 : 0);
      };
      window.addEventListener("keydown", keys);
      window.addEventListener("keyup", keys);
    },
    afterStart(f) {
      // States load once the core has run a couple of frames.
      const go = () => {
        const first = gm().getFrameNum();
        const tick = () => (gm().getFrameNum() > first + 1 ? f() : requestAnimationFrame(tick));
        requestAnimationFrame(tick);
      };
      if (started) go();
      else onStart.push(go);
    },
    // RetroArch doesn't say when a game saves; look every two seconds. The
    // player only uploads a save that changed.
    onSave(f) {
      window.clearInterval(saveTimer);
      if (f) saveTimer = window.setInterval(() => started && f(), 2000);
    },
    getSave: () => (started ? gm().getSaveFile() : null),
    replaceSave(save) {
      gm().FS.writeFile(gm().getSaveFilePath(), save);
      gm().loadSaveFiles();
      gm().restart();
    },
    async captureState() {
      if (!started) return null;
      let state: Uint8Array;
      try {
        state = gm().getState();
      } catch {
        return null;
      }
      const png = await (paused ? pauseShot : shot());
      return png && isPNG(png) ? wrap(png, state) : state;
    },
    async restoreState(_slot, data) {
      const state = unwrap(data);
      if (!started || !state) return false;
      gm().loadState(state.slice());
      // RetroArch applies it on its next frame.
      await new Promise((r) => setTimeout(r, 300));
      return true;
    },
    press(key, down) {
      if (started) gm().simulateInput(0, buttons[key], down ? 1 : 0);
    },
    touch(type, x, y) {
      if (!started) return;
      // RetroArch takes the position from moves: move there first.
      if (type === "mousedown") mouse("mousemove", x, y, 0);
      mouse(type, x, y, type === "mouseup" ? 0 : 1);
    },
    pause() {
      if (paused) return;
      paused = true;
      if (!started) return emu.pause(true);
      pauseShot = shot();
      pauseShot.then(() => paused && emu.pause(true));
    },
    resume() {
      paused = false;
      emu.play(true);
    },
    setSpeed(n) {
      if (!started) return;
      if (n > 1) gm().setFastForwardRatio(n);
      gm().toggleFastForward(n > 1 ? 1 : 0);
    },
    setVolume: (v) => emu.setVolume(v),
    resumeAudio() {
      const ctx = emu.Module?.AL?.currentCtx?.audioCtx;
      if (ctx && ctx.state !== "running") ctx.resume().catch(() => {});
    },
    stop() {
      window.clearInterval(saveTimer);
      if (keys) {
        window.removeEventListener("keydown", keys);
        window.removeEventListener("keyup", keys);
      }
      try {
        emu.pause(true);
      } catch {
        // Not started.
      }
    },
  };
}

function extension(sys: System): string {
  return { gb: ".gb", gbc: ".gbc", nes: ".nes", snes: ".sfc", nds: ".nds", gba: ".gba" }[sys.id];
}
