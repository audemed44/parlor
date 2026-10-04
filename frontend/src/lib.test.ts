import { describe, expect, it } from "vitest";
import { route } from "./App";
import { ago, coverText, deviceName, duration } from "./lib";

describe("route", () => {
  it("reads the hash", () => {
    expect(route("#/play/12")).toEqual({ page: "play", id: 12 });
    expect(route("#/game/3")).toEqual({ page: "game", id: 3 });
    expect(route("#/import")).toEqual({ page: "import" });
    expect(route("#/settings")).toEqual({ page: "settings" });
    expect(route("#/patch")).toEqual({ page: "patch" });
    expect(route("")).toEqual({ page: "library" });
    expect(route("#/play/x")).toEqual({ page: "library" });
  });
});

describe("formatting", () => {
  const now = Date.parse("2026-10-04T12:00:00Z");
  it("says how long ago", () => {
    expect(ago("2026-10-04T11:59:30Z", now)).toBe("just now");
    expect(ago("2026-10-04T11:15:00Z", now)).toBe("45 min ago");
    expect(ago("2026-10-04T09:00:00Z", now)).toBe("3 h ago");
    expect(ago("2026-10-03T09:00:00Z", now)).toBe("yesterday");
    expect(ago("", now)).toBe("never");
  });
  it("formats play time", () => {
    expect(duration(59)).toBe("0 min");
    expect(duration(45 * 60)).toBe("45 min");
    expect(duration(12 * 3600 + 5 * 60)).toBe("12 h 5 min");
    expect(duration(2 * 3600)).toBe("2 h");
  });
  it("drops the shared Pokémon prefix from covers", () => {
    expect(coverText("Pokémon Heart and Soul (v2.0.4)")).toBe("Heart and Soul");
    expect(coverText("Pokemon - FireRed Version (USA, Europe)")).toBe("FireRed Version");
    expect(coverText("Pokemon Elysium_A")).toBe("Elysium A");
    expect(coverText("Pokemon")).toBe("Pokemon");
  });
  it("names devices", () => {
    expect(deviceName("Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)", 5)).toBe("iPhone");
    expect(deviceName("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", 5)).toBe("iPad");
    expect(deviceName("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", 0)).toBe("Mac");
    expect(deviceName("Mozilla/5.0 (X11; Linux x86_64)", 0)).toBe("Linux");
  });
});
