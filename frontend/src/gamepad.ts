// Bluetooth controllers through the Gamepad API. Xbox, PlayStation and MFi
// controllers all use the browser's "standard" button layout, so one set of
// bindings fits them all. Bindings are kept per device, since that's where
// the controller is paired.
import type { Key } from "./controls";

// Actions besides the console's buttons.
export type PadAction = "Fast" | "Menu" | "SaveState" | "LoadState";
export type Control = Key | PadAction;
export type Bindings = Record<Control, number[]>;

export const controls: [Control, string][] = [
  ["A", "A"],
  ["B", "B"],
  ["X", "X (SNES, DS)"],
  ["Y", "Y (SNES, DS)"],
  ["L", "L"],
  ["R", "R"],
  ["Start", "Start"],
  ["Select", "Select"],
  ["Up", "Up"],
  ["Down", "Down"],
  ["Left", "Left"],
  ["Right", "Right"],
  ["Fast", "Fast forward"],
  ["Menu", "Menu"],
  ["SaveState", "Save state to slot 1"],
  ["LoadState", "Load state from slot 1"],
];

// The standard layout's buttons, by index.
const names = [
  "A / Cross",
  "B / Circle",
  "X / Square",
  "Y / Triangle",
  "LB / L1",
  "RB / R1",
  "LT / L2",
  "RT / R2",
  "View / Share",
  "Menu / Options",
  "Left stick",
  "Right stick",
  "D-pad up",
  "D-pad down",
  "D-pad left",
  "D-pad right",
  "Home",
];

export function buttonName(i: number): string {
  return names[i] ?? `Button ${i + 1}`;
}

// The defaults go by the labels: the controller's A is the console's A.
// Y doubles as Menu on consoles without a Y.
export const defaults: Bindings = {
  A: [0],
  B: [1],
  X: [2],
  Y: [3],
  L: [4],
  R: [5],
  Start: [9],
  Select: [8],
  Up: [12],
  Down: [13],
  Left: [14],
  Right: [15],
  Fast: [7],
  Menu: [16, 3],
  SaveState: [],
  LoadState: [],
};

const STORAGE = "parlor-pad";

export function loadBindings(): Bindings {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE) ?? "{}");
    const out = { ...defaults };
    for (const [c] of controls) {
      const v = saved[c];
      if (Array.isArray(v) && v.every((n) => Number.isInteger(n) && n >= 0)) out[c] = v;
    }
    return out;
  } catch {
    return { ...defaults };
  }
}

export function saveBindings(b: Bindings) {
  localStorage.setItem(STORAGE, JSON.stringify(b));
}

// bind gives a control a button, taking it from any control that had it.
export function bind(b: Bindings, control: Control, button: number): Bindings {
  const out = { ...b };
  for (const [c] of controls) out[c] = out[c].filter((n) => n !== button);
  out[control] = [button];
  return out;
}

// How far a stick has to lean to press a direction.
const STICK = 0.5;

export interface PadLike {
  buttons: readonly { pressed: boolean; value: number }[];
  axes: readonly number[];
}

// read is what a controller is pressing, for a console with keys (all of
// them when left out). A button bound to one of the console's keys only
// presses that key, so Y is Y on the SNES and Menu on the GBA. The left
// stick always works as a d-pad too.
export function read(pad: PadLike, b: Bindings, keys?: readonly Key[]): Set<Control> {
  const out = new Set<Control>();
  const down = (i: number) => {
    const btn = pad.buttons[i];
    return !!btn && (btn.pressed || btn.value > 0.5);
  };
  const has = (c: Control) => !keys || !isKey(c) || keys.includes(c);
  const claimed = new Set<number>();
  for (const [c] of controls) if (isKey(c) && has(c)) for (const n of b[c]) claimed.add(n);
  for (const [c] of controls) {
    if (!has(c)) continue;
    const buttons = isKey(c) ? b[c] : b[c].filter((n) => !claimed.has(n));
    if (buttons.some(down)) out.add(c);
  }
  const [x = 0, y = 0] = pad.axes;
  if (x < -STICK) out.add("Left");
  if (x > STICK) out.add("Right");
  if (y < -STICK) out.add("Up");
  if (y > STICK) out.add("Down");
  return out;
}

function isKey(c: Control): c is Key {
  return c !== "Fast" && c !== "Menu" && c !== "SaveState" && c !== "LoadState";
}

// pads lists the connected controllers. iOS only reports one after a button
// on it is pressed.
export function pads(): Gamepad[] {
  if (!navigator.getGamepads) return [];
  return navigator.getGamepads().filter((p): p is Gamepad => !!p && p.connected);
}

// firstPressed is the lowest button a controller is pressing, or -1.
export function firstPressed(pad: PadLike): number {
  return pad.buttons.findIndex((btn) => btn.pressed || btn.value > 0.5);
}
