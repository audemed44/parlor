// The consoles Parlor plays, and what each needs: which emulator, the
// screen's size, which buttons it has. The GBA runs on mGBA; the 3DS on
// the server, streamed (stream.ts); the rest on RetroArch cores through
// EmulatorJS.
import type { Key } from "./controls";

export type Platform = "gba" | "gb" | "gbc" | "nes" | "snes" | "nds" | "3ds";

export interface System {
  id: Platform;
  short: string;
  name: string;
  // EmulatorJS's name for the console and the core it runs; none for the
  // GBA, which is mGBA's.
  ejs?: { system: string; core: string; saveDir: string; saveExt: string };
  // Played on the server (parlor-stream) and streamed to the browser.
  stream?: boolean;
  // The screen in CSS pixels at 1×: both DS screens, one above the other.
  screen: { w: number; h: number };
  keys: Key[];
  // The DS's and 3DS's bottom screen takes taps: its box within the screen, as
  // fractions of it.
  touch?: { x: number; y: number; w: number; h: number };
}

const dirs: Key[] = ["Up", "Down", "Left", "Right"];
const small: Key[] = ["A", "B", "Start", "Select", ...dirs];
const big: Key[] = ["A", "B", "X", "Y", "L", "R", "Start", "Select", ...dirs];

// saveDir is the folder RetroArch keeps each core's saves in, named after
// the core; Parlor writes the server's save there before the game starts.
export const systems: Record<Platform, System> = {
  gba: {
    id: "gba",
    short: "GBA",
    name: "Game Boy Advance",
    screen: { w: 240, h: 160 },
    keys: ["A", "B", "L", "R", "Start", "Select", ...dirs],
  },
  gb: {
    id: "gb",
    short: "GB",
    name: "Game Boy",
    ejs: { system: "gb", core: "gambatte", saveDir: "Gambatte", saveExt: "srm" },
    screen: { w: 160, h: 144 },
    keys: small,
  },
  gbc: {
    id: "gbc",
    short: "GBC",
    name: "Game Boy Color",
    ejs: { system: "gb", core: "gambatte", saveDir: "Gambatte", saveExt: "srm" },
    screen: { w: 160, h: 144 },
    keys: small,
  },
  nes: {
    id: "nes",
    short: "NES",
    name: "NES",
    ejs: { system: "nes", core: "fceumm", saveDir: "FCEUmm", saveExt: "srm" },
    // 256×224 visible, with the NES's wide pixels (8:7).
    screen: { w: 292, h: 224 },
    keys: small,
  },
  snes: {
    id: "snes",
    short: "SNES",
    name: "Super Nintendo",
    ejs: { system: "snes", core: "snes9x", saveDir: "Snes9x", saveExt: "srm" },
    screen: { w: 256, h: 224 },
    keys: big,
  },
  nds: {
    id: "nds",
    short: "DS",
    name: "Nintendo DS",
    ejs: { system: "nds", core: "melonds", saveDir: "melonDS", saveExt: "sav" },
    screen: { w: 256, h: 384 },
    keys: big,
    touch: { x: 0, y: 0.5, w: 1, h: 0.5 },
  },
  "3ds": {
    id: "3ds",
    short: "3DS",
    name: "Nintendo 3DS",
    stream: true,
    // The top screen (400×240) over the bottom one (320×240), centred.
    screen: { w: 400, h: 480 },
    keys: big,
    touch: { x: 0.1, y: 0.5, w: 0.8, h: 0.5 },
  },
};

export function system(p: string): System {
  return systems[p as Platform] ?? systems.gba;
}

// playURL opens a game. EmulatorJS's games play on /play, a page of the
// app with the looser content policy its cores need.
export function playURL(g: { id: number; platform: string }): string {
  return system(g.platform).ejs ? `/play#/play/${g.id}` : `/#/play/${g.id}`;
}
