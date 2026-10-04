import { describe, expect, it } from "vitest";
import { isPNG, unwrap, wrap } from "./png";

// The smallest PNG: signature, a 1×1 IHDR, an IDAT and IEND.
const png = Uint8Array.from(
  atob(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  ),
  (c) => c.charCodeAt(0),
);

describe("states in PNGs", () => {
  it("round-trips a state", () => {
    const state = new TextEncoder().encode("RASTATE\x01 some state");
    const out = wrap(png, state);
    expect(isPNG(out)).toBe(true);
    expect(Array.from(out.subarray(out.length - 8, out.length - 4))).toEqual([73, 69, 78, 68]);
    expect(Array.from(unwrap(out)!)).toEqual(Array.from(state));
  });
  it("passes bare states through and finds none in a plain PNG", () => {
    const bare = new TextEncoder().encode("RASTATE");
    expect(unwrap(bare)).toEqual(bare);
    expect(unwrap(png)).toBeNull();
  });
});
