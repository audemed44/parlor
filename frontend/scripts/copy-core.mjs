// Copies the mGBA WebAssembly core into public/core/<version>/, so it's
// served from Parlor itself under a path that changes with each version
// (cached forever). The core starts its threads from its own script URL,
// so it can't go through the bundler.
import { cpSync, mkdirSync, readFileSync, rmSync } from "node:fs";

const from = "node_modules/@thenick775/mgba-wasm";
const { version } = JSON.parse(readFileSync(`${from}/package.json`, "utf8"));
rmSync("public/core", { recursive: true, force: true });
mkdirSync(`public/core/${version}`, { recursive: true });
for (const f of ["mgba.js", "mgba.wasm"]) cpSync(`${from}/dist/${f}`, `public/core/${version}/${f}`);
cpSync(`${from}/README.md`, `public/core/${version}/README.md`);
console.log(`mGBA core ${version} copied`);
