// What the player needs from an emulator, whichever it is: mGBA for the
// GBA (emulator.ts), a RetroArch core through EmulatorJS for the rest
// (ejs.ts).
import type { Key } from "./controls";

// Overrides are what mGBA should use instead of detecting it: the save
// type (as mGBA names them) and whether the cartridge has a clock. Only
// GBA games have them.
export interface Overrides {
  saveType: string;
  rtc: "" | "on" | "off";
}

export interface Core {
  // The element showing the game, which the player moves into place.
  readonly screen: HTMLElement;
  // Whether the core can play another game after this one. EmulatorJS
  // can't: leaving one of its games reloads the page.
  readonly reusable: boolean;
  // start loads a game with its in-game save (null for a new game). It's
  // synchronous so it can run inside the tap that starts the game, which
  // iOS requires for sound.
  start(rom: Uint8Array, save: Uint8Array | null, overrides: Overrides): void;
  // afterStart runs f once the game is running, when states can load.
  afterStart(f: () => void): void;
  // onSave calls f whenever the game may have written its save; null
  // stops.
  onSave(f: (() => void) | null): void;
  // getSave is the game's in-game save as it is now.
  getSave(): Uint8Array | null;
  // replaceSave puts another save in the running game and restarts it.
  replaceSave(save: Uint8Array): void;
  // captureState is a save state of the running game, as a PNG of the
  // screen with the state inside; null when the core couldn't take one.
  captureState(slot: number): Promise<Uint8Array | null>;
  // restoreState loads a state; false when the core couldn't.
  restoreState(slot: number, data: Uint8Array): Promise<boolean>;
  press(key: Key, down: boolean): void;
  // touch puts the stylus on the touch screen (the DS's), at a point on
  // the page; the core maps it to the screen.
  touch?(type: "mousedown" | "mousemove" | "mouseup", x: number, y: number): void;
  pause(): void;
  resume(): void;
  // setSpeed runs the game at a multiple of its speed; 1 is normal.
  setSpeed(multiple: number): void;
  setVolume(volume: number): void;
  // resumeAudio wakes the sound; iOS suspends it until a tap and after
  // interruptions.
  resumeAudio(): void;
  stop(): void;
}

// The desktop keyboard, by KeyboardEvent.code. Consoles with X and Y move
// L and R up a row.
export function keyboard(keys: readonly Key[]): [string, Key][] {
  const map: [string, Key][] = [
    ["KeyX", "A"],
    ["KeyZ", "B"],
    ["Enter", "Start"],
    ["Backspace", "Select"],
    ["ArrowUp", "Up"],
    ["ArrowDown", "Down"],
    ["ArrowLeft", "Left"],
    ["ArrowRight", "Right"],
  ];
  if (keys.includes("X")) {
    map.push(["KeyS", "X"], ["KeyA", "Y"], ["KeyQ", "L"], ["KeyW", "R"]);
  } else if (keys.includes("L")) {
    map.push(["KeyA", "L"], ["KeyS", "R"]);
  }
  return map.filter(([, k]) => keys.includes(k));
}

// keyName is how the desktop hint shows a key code.
export function keyName(code: string): string {
  if (code.startsWith("Key")) return code.slice(3);
  if (code.startsWith("Arrow")) return "Arrows";
  return code;
}
