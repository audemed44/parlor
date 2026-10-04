import { describe, expect, it } from "vitest";
import { gameCode, overrideConfig } from "./emulator";

describe("overrides", () => {
  it("reads the game code from the ROM header", () => {
    const rom = new Uint8Array(0x200);
    rom.set([0x42, 0x50, 0x45, 0x45], 0xac); // BPEE
    expect(gameCode(rom)).toBe("BPEE");
    rom[0xac] = 0;
    expect(gameCode(rom)).toBe("");
  });
  it("writes mGBA's override section", () => {
    expect(overrideConfig("BPRE", { saveType: "", rtc: "" })).toBe("");
    expect(overrideConfig("", { saveType: "FLASH1M", rtc: "on" })).toBe("");
    expect(overrideConfig("BPRE", { saveType: "FLASH1M", rtc: "on" })).toBe(
      "[override.BPRE]\nsavetype=FLASH1M\nhardware=1\n",
    );
    expect(overrideConfig("BPEE", { saveType: "", rtc: "off" })).toBe(
      "[override.BPEE]\nhardware=0\n",
    );
  });
});
