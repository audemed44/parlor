// Touch controls: where each control sits, and which GBA buttons a set of
// touches presses. Drawing and hit-testing share the same shapes, so what
// you see is what you press. Hit zones are larger than the drawn controls.

export type Key = "A" | "B" | "L" | "R" | "Start" | "Select" | "Up" | "Down" | "Left" | "Right";
// Not GBA buttons: "Menu" opens Parlor's menu, "Fast" toggles fast forward.
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
}

// layout places the controls for an area of w×h CSS pixels. Portrait: the
// screen on top, controls below, like a GBA SP. Landscape: the screen as
// big as it fits, with the controls laid over it, see-through, at the
// edges where thumbs rest.
export function layout(w: number, h: number): Layout {
  if (w > h) {
    const s = Math.min(h / 390, w / 844, 1.4);
    const screenH = Math.min(h, w / 1.5);
    const screenW = screenH * 1.5;
    const screen = { x: (w - screenW) / 2, y: (h - screenH) / 2, w: screenW, h: screenH };
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
  const screenW = w;
  const screenH = w / 1.5;
  const top = screenH;
  const rest = h - top;
  const s = Math.min(w / 390, rest / 440, 1.4);
  const cy = top + Math.min(rest * 0.42, 190 * s);
  return {
    screen: { x: 0, y: 0, w: screenW, h: screenH },
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

// padLayout is for playing with a controller on a touch screen: the game
// as big as it fits, and only a menu button to touch.
export function padLayout(w: number, h: number): Layout {
  const sw = Math.min(w, h * 1.5);
  const sh = sw / 1.5;
  const portrait = h > w;
  const screen = { x: (w - sw) / 2, y: portrait ? 0 : (h - sh) / 2, w: sw, h: sh };
  return {
    screen,
    landscape: true,
    shapes: [
      {
        kind: "rect",
        id: "Menu",
        x: w - 70,
        y: portrait ? sh + 14 : 10,
        w: 56,
        h: 28,
      },
    ],
  };
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
