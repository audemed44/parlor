import { describe, expect, it } from "vitest";
import { dpadKeys, hit, layout, pressed, type Circle } from "./controls";

// An iPhone 15 in portrait, below the status bar.
const portrait = layout(393, 780);
const find = (id: string) => portrait.shapes.find((s) => s.id === id)! as Circle;

describe("d-pad", () => {
  it("maps angles to directions with wide cardinals", () => {
    expect(dpadKeys(50, 0, 60)).toEqual(["Right"]);
    expect(dpadKeys(0, -50, 60)).toEqual(["Up"]);
    expect(dpadKeys(-50, 0, 60)).toEqual(["Left"]);
    expect(dpadKeys(0, 50, 60)).toEqual(["Down"]);
    expect(dpadKeys(40, -40, 60)).toEqual(["Up", "Right"]);
    expect(dpadKeys(-40, 40, 60)).toEqual(["Down", "Left"]);
    // 20° off the axis is still a cardinal.
    expect(dpadKeys(50, -18, 60)).toEqual(["Right"]);
  });
  it("ignores the centre", () => {
    expect(dpadKeys(3, 3, 60)).toEqual([]);
  });
});

describe("hit", () => {
  it("presses the d-pad a little outside what's drawn", () => {
    const d = find("dpad");
    expect(hit(portrait.shapes, d.x + d.r * 1.2, d.y)).toEqual(["Right"]);
    expect(hit(portrait.shapes, d.x + d.r * 1.6, d.y)).not.toContain("Right");
  });
  it("presses A or B, and both between them", () => {
    const a = find("A");
    const b = find("B");
    expect(hit(portrait.shapes, a.x, a.y)).toEqual(["A"]);
    expect(hit(portrait.shapes, b.x, b.y)).toEqual(["B"]);
    expect(hit(portrait.shapes, (a.x + b.x) / 2, (a.y + b.y) / 2).sort()).toEqual(["A", "B"]);
  });
  it("presses padded rectangles", () => {
    const start = portrait.shapes.find((s) => s.id === "Start")!;
    if (start.kind !== "rect") throw new Error("rect");
    expect(hit(portrait.shapes, start.x - 6, start.y + 4)).toEqual(["Start"]);
    expect(hit(portrait.shapes, start.x + start.w / 2, start.y - 40)).toEqual([]);
  });
  it("leaves the screen alone", () => {
    expect(hit(portrait.shapes, 196, 100)).toEqual([]);
  });
});

describe("pressed", () => {
  it("combines touches: d-pad and A together", () => {
    const d = find("dpad");
    const a = find("A");
    const keys = pressed(portrait.shapes, [
      { x: d.x, y: d.y - d.r * 0.8 },
      { x: a.x, y: a.y },
    ]);
    expect([...keys].sort()).toEqual(["A", "Up"]);
  });
});

describe("layout", () => {
  it("keeps everything inside the area in both orientations", () => {
    for (const [w, h] of [
      [393, 780],
      [375, 600],
      [844, 360],
      [1280, 720],
    ]) {
      const l = layout(w, h);
      for (const s of l.shapes) {
        const [x0, y0, x1, y1] =
          s.kind === "rect"
            ? [s.x, s.y, s.x + s.w, s.y + s.h]
            : [s.x - s.r, s.y - s.r, s.x + s.r, s.y + s.r];
        expect(x0, `${s.id} at ${w}×${h}`).toBeGreaterThanOrEqual(0);
        expect(y0, `${s.id} at ${w}×${h}`).toBeGreaterThanOrEqual(0);
        expect(x1, `${s.id} at ${w}×${h}`).toBeLessThanOrEqual(w);
        expect(y1, `${s.id} at ${w}×${h}`).toBeLessThanOrEqual(h);
      }
      expect(l.screen.w / l.screen.h).toBeCloseTo(1.5);
    }
  });
  it("fills the height in landscape, with the controls over the game", () => {
    const l = layout(734, 372);
    expect(l.landscape).toBe(true);
    expect(l.screen.h).toBe(372);
    const dpad = l.shapes.find((s) => s.id === "dpad")! as Circle;
    const b = l.shapes.find((s) => s.id === "B")! as Circle;
    // Both thumbs' controls overlap the picture.
    expect(dpad.x + dpad.r).toBeGreaterThan(l.screen.x);
    expect(b.x - b.r).toBeLessThan(l.screen.x + l.screen.w);
  });
});
