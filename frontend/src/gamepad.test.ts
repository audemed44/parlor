import { describe, expect, it } from "vitest";
import { bind, defaults, firstPressed, read, type PadLike } from "./gamepad";

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
  it("finds the pressed button", () => {
    expect(firstPressed(pad([5, 9]))).toBe(5);
    expect(firstPressed(pad([]))).toBe(-1);
  });
});
