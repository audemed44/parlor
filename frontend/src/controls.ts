// Touch controls: where each control sits, and which buttons a set of
// touches presses. Drawing and hit-testing share the same shapes, so what
// you see is what you press. Hit zones are larger than the drawn controls.
import { systems, type Platform, type System } from "./systems";

export type Key =
  "A" | "B" | "X" | "Y" | "L" | "R" | "Start" | "Select" | "Up" | "Down" | "Left" | "Right";
// Not console buttons: "Menu" opens Parlor's menu, "Fast" toggles fast
// forward.
export type Action = "Menu" | "Fast";
export type Press = Key | Action;

export interface Circle {
  kind: "dpad" | "round";
  id: string;
  x: number;
  y: number;
  r: number;
}
export interface Rect {
  kind: "rect";
  id: Press;
  x: number;
  y: number;
  w: number;
  h: number;
}
export type Shape = Circle | Rect;

export interface Layout {
  shapes: Shape[];
  // The screen's box, in the same coordinates (landscape: controls sit
  // around it; portrait: above the controls).
  screen: { x: number; y: number; w: number; h: number };
  // Controls drawn over the game rather than beside it.
  landscape?: boolean;
  // How opaque the controls are, when set in the layout editor.
  opacity?: number;
  // The part of the screen that takes taps (the DS's bottom screen).
  touch?: { x: number; y: number; w: number; h: number };
}

// layout places the controls for an area of w×h CSS pixels. Portrait: the
// screen on top, controls below, like a GBA SP. Landscape: the screen as
// big as it fits, with the controls laid over it, see-through, at the
// edges where thumbs rest. Consoles with X and Y get a diamond of four
// face buttons; ones without L and R don't get them.
export function layout(w: number, h: number, sys: System = systems.gba): Layout {
  const l = sys.keys.includes("X") ? fourButtons(w, h, sys) : twoButtons(w, h, sys);
  l.shapes = l.shapes.filter((v) => v.kind !== "rect" || !isKey(v.id) || sys.keys.includes(v.id));
  return withTouch(l, sys);
}

function isKey(p: Press): p is Key {
  return p !== "Menu" && p !== "Fast";
}

// withTouch marks the screen's touch part, when the console has one.
function withTouch(l: Layout, sys: System): Layout {
  const t = sys.touch;
  if (!t) return l;
  const s = l.screen;
  return { ...l, touch: { x: s.x + t.x * s.w, y: s.y + t.y * s.h, w: t.w * s.w, h: t.h * s.h } };
}

// The screen as big as fits w×h, centred; ratio is width over height.
function fit(w: number, h: number, ratio: number) {
  const sh = Math.min(h, w / ratio);
  const sw = sh * ratio;
  return { x: (w - sw) / 2, y: (h - sh) / 2, w: sw, h: sh };
}

// portraitScreen is the screen across the top: the full width, unless
// that would leave too little room for the controls (the DS's two
// screens are taller than wide).
function portraitScreen(w: number, h: number, ratio: number) {
  const sh = Math.min(w / ratio, h * 0.6);
  const sw = sh * ratio;
  return { x: (w - sw) / 2, y: 0, w: sw, h: sh };
}

const ratioOf = (sys: System) => sys.screen.w / sys.screen.h;

function twoButtons(w: number, h: number, sys: System): Layout {
  const ratio = ratioOf(sys);
  if (w > h) {
    const s = Math.min(h / 390, w / 844, 1.4);
    const screen = fit(w, h, ratio);
    const pad = 84 * s; // centre of the d-pad and of A/B from the side
    return {
      screen,
      landscape: true,
      shapes: [
        { kind: "rect", id: "L", x: 10 * s, y: 10 * s, w: 120 * s, h: 40 * s },
        { kind: "rect", id: "R", x: w - 130 * s, y: 10 * s, w: 120 * s, h: 40 * s },
        { kind: "rect", id: "Menu", x: w / 2 - 62 * s, y: 6 * s, w: 56 * s, h: 26 * s },
        { kind: "rect", id: "Fast", x: w / 2 + 6 * s, y: 6 * s, w: 56 * s, h: 26 * s },
        { kind: "dpad", id: "dpad", x: pad, y: h * 0.58, r: 66 * s },
        { kind: "round", id: "A", x: w - pad + 36 * s, y: h * 0.52, r: 32 * s },
        { kind: "round", id: "B", x: w - pad - 38 * s, y: h * 0.52 + 36 * s, r: 32 * s },
        { kind: "rect", id: "Select", x: 14 * s, y: h - 40 * s, w: 70 * s, h: 28 * s },
        { kind: "rect", id: "Start", x: w - 84 * s, y: h - 40 * s, w: 70 * s, h: 28 * s },
      ],
    };
  }
  const screen = portraitScreen(w, h, ratio);
  const top = screen.h;
  const rest = h - top;
  const s = Math.min(w / 390, rest / 440, 1.4);
  const cy = top + Math.min(rest * 0.42, 190 * s);
  return {
    screen,
    shapes: [
      { kind: "rect", id: "L", x: 14 * s, y: top + 14 * s, w: 110 * s, h: 40 * s },
      { kind: "rect", id: "R", x: w - 124 * s, y: top + 14 * s, w: 110 * s, h: 40 * s },
      { kind: "rect", id: "Menu", x: w / 2 - 60 * s, y: top + 20 * s, w: 52 * s, h: 28 * s },
      { kind: "rect", id: "Fast", x: w / 2 + 8 * s, y: top + 20 * s, w: 52 * s, h: 28 * s },
      { kind: "dpad", id: "dpad", x: 96 * s, y: cy, r: 72 * s },
      { kind: "round", id: "A", x: w - 54 * s, y: cy - 22 * s, r: 34 * s },
      { kind: "round", id: "B", x: w - 136 * s, y: cy + 16 * s, r: 34 * s },
      {
        kind: "rect",
        id: "Select",
        x: w / 2 - 82 * s,
        y: Math.min(h - 58 * s, cy + 150 * s),
        w: 70 * s,
        h: 28 * s,
      },
      {
        kind: "rect",
        id: "Start",
        x: w / 2 + 12 * s,
        y: Math.min(h - 58 * s, cy + 150 * s),
        w: 70 * s,
        h: 28 * s,
      },
    ],
  };
}

// fourButtons is twoButtons with X, Y, A and B in a diamond, as on the
// SNES and the DS: X on top, A right, B below, Y left.
function fourButtons(w: number, h: number, sys: System): Layout {
  const l = twoButtons(w, h, sys);
  const a = l.shapes.find((v) => v.id === "A") as Circle;
  const b = l.shapes.find((v) => v.id === "B") as Circle;
  const r = a.r * 0.82;
  const d = r * 1.25;
  const cx = (a.x + b.x) / 2 + (l.landscape ? -4 : 6) * (r / 28);
  const cy = (a.y + b.y) / 2;
  const others = l.shapes.filter((v) => v.id !== "A" && v.id !== "B");
  return {
    ...l,
    shapes: [
      ...others,
      { kind: "round", id: "X", x: cx, y: cy - d, r },
      { kind: "round", id: "A", x: cx + d, y: cy, r },
      { kind: "round", id: "B", x: cx, y: cy + d, r },
      { kind: "round", id: "Y", x: cx - d, y: cy, r },
    ],
  };
}

// A layout of your own, per orientation, made in the layout editor: where
// each control's centre is (as a fraction of the area, so it survives the
// area changing size), how much bigger or smaller it is, and how opaque the
// controls are. Controls not in it stay where the default layout puts them.
export interface Custom {
  opacity?: number;
  shapes: Record<string, { x: number; y: number; scale: number }>;
}
export type Customs = { portrait?: Custom; landscape?: Custom };

export const MIN_SCALE = 0.6;
export const MAX_SCALE = 1.8;

export function centre(s: Shape): { x: number; y: number } {
  return s.kind === "rect" ? { x: s.x + s.w / 2, y: s.y + s.h / 2 } : { x: s.x, y: s.y };
}

// customize applies a custom layout to the default one for a w×h area.
// Controls are kept inside the area.
export function customize(base: Layout, c: Custom | undefined, w: number, h: number): Layout {
  if (!c) return base;
  const shapes = base.shapes.map((s): Shape => {
    const o = c.shapes[s.id];
    if (!o) return s;
    const k = Math.min(MAX_SCALE, Math.max(MIN_SCALE, o.scale));
    if (s.kind === "rect") {
      const sw = s.w * k;
      const sh = s.h * k;
      const x = clamp(o.x * w - sw / 2, 0, w - sw);
      const y = clamp(o.y * h - sh / 2, 0, h - sh);
      return { ...s, x, y, w: sw, h: sh };
    }
    const r = s.r * k;
    return { ...s, x: clamp(o.x * w, r, w - r), y: clamp(o.y * h, r, h - r), r };
  });
  return { ...base, shapes, opacity: c.opacity };
}

function clamp(v: number, lo: number, hi: number) {
  return Math.min(Math.max(v, lo), Math.max(lo, hi));
}

// shapeAt is the control drawn under (x, y), for picking one up in the
// editor; a little slack makes small ones easier to grab.
export function shapeAt(shapes: Shape[], x: number, y: number): Shape | null {
  for (const s of [...shapes].reverse()) {
    if (s.kind === "rect") {
      if (x >= s.x - 8 && x <= s.x + s.w + 8 && y >= s.y - 8 && y <= s.y + s.h + 8) return s;
    } else if (Math.hypot(x - s.x, y - s.y) <= s.r + 8) return s;
  }
  return null;
}

// Each console has its own layouts; the GBA keeps the original key.
const storage = (p: Platform) => (p === "gba" ? "parlor-layout" : `parlor-layout-${p}`);

export function loadCustoms(p: Platform = "gba"): Customs {
  try {
    const v = JSON.parse(localStorage.getItem(storage(p)) ?? "{}");
    return v && typeof v === "object" ? v : {};
  } catch {
    return {};
  }
}

export function saveCustoms(c: Customs, p: Platform = "gba") {
  localStorage.setItem(storage(p), JSON.stringify(c));
}

// padLayout is for playing with a controller on a touch screen: the game
// as big as it fits, and only a menu button to touch.
export function padLayout(w: number, h: number, sys: System = systems.gba): Layout {
  const ratio = ratioOf(sys);
  const sw = Math.min(w, h * ratio);
  const sh = sw / ratio;
  const portrait = h > w;
  const screen = { x: (w - sw) / 2, y: portrait ? 0 : (h - sh) / 2, w: sw, h: sh };
  return withTouch(
    {
      screen,
      landscape: true,
      shapes: [
        {
          kind: "rect",
          id: "Menu",
          x: w - 70,
          y: portrait && sh + 56 < h ? sh + 14 : 10,
          w: 56,
          h: 28,
        },
      ],
    },
    sys,
  );
}

// How much bigger hit zones are than the drawn controls.
const ROUND_HIT = 1.4;
const DPAD_HIT = 1.35;
const RECT_PAD = 12;
// The d-pad's centre presses nothing.
const DEAD = 0.18;

// dpadKeys maps a touch on the d-pad to directions. Cardinals get 60° and
// diagonals 30°, so walking a grid in Pokémon doesn't slip diagonally.
export function dpadKeys(dx: number, dy: number, r: number): Key[] {
  const dist = Math.hypot(dx, dy);
  if (dist < r * DEAD) return [];
  // 0° = right, counter-clockwise, screen y pointing down.
  const deg = ((Math.atan2(-dy, dx) * 180) / Math.PI + 360) % 360;
  const sectors: [number, Key[]][] = [
    [30, ["Right"]],
    [60, ["Up", "Right"]],
    [120, ["Up"]],
    [150, ["Up", "Left"]],
    [210, ["Left"]],
    [240, ["Down", "Left"]],
    [300, ["Down"]],
    [330, ["Down", "Right"]],
    [360, ["Right"]],
  ];
  for (const [end, keys] of sectors) if (deg < end) return keys;
  return ["Right"];
}

// hit returns what one touch at (x, y) presses: the closest control whose
// hit zone holds it. Between A and B, both.
export function hit(shapes: Shape[], x: number, y: number): Press[] {
  let best: Press[] = [];
  let bestDist = Infinity;
  const rounds: { id: Key; d: number; r: number }[] = [];
  for (const s of shapes) {
    if (s.kind === "rect") {
      const dx = Math.max(s.x - x, 0, x - (s.x + s.w));
      const dy = Math.max(s.y - y, 0, y - (s.y + s.h));
      const d = Math.hypot(dx, dy);
      if (d <= RECT_PAD && d < bestDist) {
        best = [s.id];
        bestDist = d;
      }
      continue;
    }
    const d = Math.hypot(x - s.x, y - s.y);
    if (s.kind === "dpad") {
      if (d <= s.r * DPAD_HIT && d - s.r < bestDist) {
        best = dpadKeys(x - s.x, y - s.y, s.r);
        bestDist = Math.max(0, d - s.r);
      }
      continue;
    }
    if (d <= s.r * ROUND_HIT) rounds.push({ id: s.id as Key, d, r: s.r });
  }
  if (rounds.length) {
    rounds.sort((a, b) => a.d - b.d);
    const [first, second] = rounds;
    const edge = Math.max(0, first.d - first.r);
    if (edge <= bestDist) {
      // A thumb resting across both A and B presses both.
      if (second && second.d - first.d < first.r * 0.35) return [first.id, second.id];
      return [first.id];
    }
  }
  return best;
}

// pressed is everything held by all touches together.
export function pressed(shapes: Shape[], touches: Iterable<{ x: number; y: number }>): Set<Press> {
  const out = new Set<Press>();
  for (const t of touches) for (const p of hit(shapes, t.x, t.y)) out.add(p);
  return out;
}
