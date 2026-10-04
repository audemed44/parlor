import { describe, expect, it } from "vitest";
import { bind, defaults, firstPressed, read, type PadLike } from "./gamepad";
import { systems } from "./systems";

function pad(pressed: number[], axes = [0, 0]): PadLike {
  return {
    buttons: Array.from({ length: 17 }, (_, i) => ({
      pressed: pressed.includes(i),
      value: pressed.includes(i) ? 1 : 0,
    })),
    axes,
  };
}

describe("gamepad", () => {
  it("reads buttons through the bindings", () => {
    expect([...read(pad([0, 12]), defaults)].sort()).toEqual(["A", "Up"]);
    expect([...read(pad([7]), defaults)]).toEqual(["Fast"]);
    expect([...read(pad([]), defaults)]).toEqual([]);
  });
  it("uses the left stick as a d-pad", () => {
    expect([...read(pad([], [-0.9, 0.8]), defaults)].sort()).toEqual(["Down", "Left"]);
    expect([...read(pad([], [0.3, -0.2]), defaults)]).toEqual([]);
  });
  it("moves a button from one control to another", () => {
    const b = bind(defaults, "B", 0);
    expect(b.B).toEqual([0]);
    expect(b.A).toEqual([]);
    expect([...read(pad([0]), b)]).toEqual(["B"]);
  });
  it("gives Y to consoles that have one, and Menu to the rest", () => {
    const gba = systems.gba.keys;
    const snes = systems.snes.keys;
    expect([...read(pad([3]), defaults, gba)]).toEqual(["Menu"]);
    expect([...read(pad([3]), defaults, snes)]).toEqual(["Y"]);
    expect([...read(pad([2]), defaults, gba)]).toEqual([]);
    expect([...read(pad([2]), defaults, snes)]).toEqual(["X"]);
    // Home is Menu everywhere.
    expect([...read(pad([16]), defaults, snes)]).toEqual(["Menu"]);
    // A console without L and R ignores them.
    expect([...read(pad([4]), defaults, systems.nes.keys)]).toEqual([]);
  });
  it("finds the pressed button", () => {
    expect(firstPressed(pad([5, 9]))).toBe(5);
    expect(firstPressed(pad([]))).toBe(-1);
  });
});
