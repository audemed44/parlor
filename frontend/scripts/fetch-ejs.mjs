// Downloads EmulatorJS and the RetroArch cores Parlor uses for consoles
// other than the GBA into public/ejs/<version>/, so they're served from
// Parlor itself (cross-origin isolation needs everything same-origin) under
// a path that changes with each version. Downloads are kept in .ejs-cache.
//
// EmulatorJS picks a core build by the browser and the core: "-legacy"
// without WebGL 2. Both builds are kept, so it never falls back to its CDN.
import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { version } from "./ejs-version.mjs";

const cdn = `https://cdn.emulatorjs.org/${version}/data`;
// Each core with its licence and source, for the notice served beside them.
const about = {
  fceumm: ["NES", "GPL-2.0", "https://github.com/libretro/libretro-fceumm"],
  snes9x: ["Super Nintendo", "Snes9x licence (non-commercial)", "https://github.com/libretro/snes9x"],
  gambatte: ["Game Boy, Game Boy Color", "GPL-2.0", "https://github.com/libretro/gambatte-libretro"],
  melonds: ["DS", "GPL-3.0", "https://github.com/libretro/melonDS"],
};
const cores = {
  fceumm: ["", "-legacy"], // NES
  snes9x: ["", "-legacy"], // Super Nintendo
  gambatte: ["", "-legacy"], // Game Boy and Game Boy Color
  melonds: ["", "-legacy"], // DS
};
const files = ["emulator.min.js", "emulator.min.css", "compression/extract7z.js"];
for (const [core, builds] of Object.entries(cores)) {
  files.push(`cores/reports/${core}.json`);
  for (const b of builds) files.push(`cores/${core}${b}-wasm.data`);
}

const cache = `.ejs-cache/${version}`;
for (const f of files) {
  const to = `${cache}/${f}`;
  if (existsSync(to)) continue;
  const res = await fetch(`${cdn}/${f}`);
  if (!res.ok) throw new Error(`EmulatorJS: ${f}: HTTP ${res.status}`);
  mkdirSync(dirname(to), { recursive: true });
  writeFileSync(to, Buffer.from(await res.arrayBuffer()));
}
if (!readFileSync(`${cache}/emulator.min.js`, "utf8").includes(`"${version}"`)) {
  throw new Error(`EmulatorJS: emulator.min.js isn't version ${version}`);
}
rmSync("public/ejs", { recursive: true, force: true });
mkdirSync("public/ejs", { recursive: true });
cpSync(cache, `public/ejs/${version}`, { recursive: true });
const notice = [
  `EmulatorJS ${version}, GPL-3.0: https://github.com/EmulatorJS/EmulatorJS/tree/v${version}`,
  "Its RetroArch cores, built by https://github.com/EmulatorJS/build:",
  ...Object.entries(about).map(([core, [what, licence, src]]) => `  ${core} (${what}), ${licence}: ${src}`),
  "",
].join("\n");
writeFileSync(`public/ejs/${version}/NOTICE.txt`, notice);
console.log(`EmulatorJS ${version} with ${Object.keys(cores).join(", ")}`);
