# Parlor

Instructions for coding agents working in this repository. `CLAUDE.md`
imports this file.

## Project

A GBA, Game Boy, NES, SNES and DS library you can play from any browser,
iPhone first, with every game's save kept on the server. GBA games run on
mGBA compiled to WebAssembly (`@thenick775/mgba-wasm`); the rest on
RetroArch cores through EmulatorJS (`frontend/scripts/fetch-ejs.mjs`
downloads a pinned release into `/ejs/<version>/`). The emulators run in
the browser, and the server only serves files and keeps saves. A Go server
(`cmd/parlor`, `internal/`) serves a JSON API and the Preact + TypeScript frontend
(`frontend/`), built into `web/dist` and embedded in the binary. State is
SQLite at `/data/parlor.db` plus files: each save version under
`/data/saves/<game>/<version>.srm`, save states under
`/data/states/<game>/<slot>.ss`, patched ROMs in `/data/roms` and covers in
`/data/covers`.

- `internal/library`: finding ROMs and their console (by extension),
  trimming DS ROMs' padding (`Used`), and matching save file names to
  games (within a console when the save's folder names one).
- `internal/store`: games (scanned from the read-only ROM folder and
  `/data/roms`, whose paths start `parlor:`), save versions with conflict
  detection and thinning, save state slots, imports, patched games, covers.
- `internal/patch`: applying IPS, UPS and BPS patches, with UPS/BPS
  checksum checks.
- `internal/server`: HTTP API, auth, cross-origin isolation headers, the
  Foyer widget (`/api/foyer/widget`).
- `internal/testrom`: tiny homebrew ROMs. The GBA, GB, NES and SNES ones
  count A presses in battery RAM; the DS one turns the top screen green
  while the bottom one is touched. `go run ./cmd/testrom OUT.gba` (or
  `.gb`, `.gbc`, `.nes`, `.sfc`, `.nds`) writes one.
- `frontend/src/systems.ts`: each console's emulator, screen and buttons.
  `core.ts` is what the player needs from an emulator; `emulator.ts`
  implements it with mGBA (states, per-game overrides), `ejs.ts` with
  EmulatorJS (its UI hidden, input, saves and states driven by Parlor).
- `frontend/src/controls.ts`: touch control layout per console, the layout
  editor's custom layouts, and hit-testing (shared by drawing and input).
  `gamepad.ts` reads controllers. `sync.ts` and `pending.ts` upload saves
  and the "left off" state through an IndexedDB queue.

## Constraints

- **Never use real ROMs** in tests, fixtures, screenshots or CI. Use the
  homebrew test ROM. Don't read the user's library or saves to build things.
- The ROM folder is read-only. Saves are never overwritten or deleted except
  by the version thinning; restoring adds a new version.
- Only in-game saves (`.srm`) are canonical. A save made on top of an older
  version than the server's latest is a conflict for the user to resolve.
  Loading a save state never touches the in-game save (mGBA's
  `loadStateSlot` default flags leave save data out). RetroArch's states
  carry the save; after loading one the player takes the save as it is as
  the baseline (`rebase`), so it's only uploaded once the game saves again.
- mGBA starts its emulation thread on the tick after `loadGame`, and
  starting resets the machine: load states after the first frame
  (`afterStart`). Per-game overrides go in mGBA's `config.ini` as
  `[override.<game code>]` before the game loads.
- The cores need cross-origin isolation (COOP/COEP) and load from
  `/core/<version>/` and `/ejs/<version>/`; keep every resource
  same-origin (`fetch-ejs.mjs` keeps both WebGL builds of each core, so
  EmulatorJS never falls back to its CDN).
- EmulatorJS games play on `/play`, served with a looser CSP
  (`'unsafe-eval'`, `blob:` connects, inline styles) that the RetroArch
  cores need; the rest of the app keeps the strict one. An EmulatorJS
  instance can't load a second game: leaving one navigates back to `/`.
- EmulatorJS details: the server's save is written to
  `/data/saves/<core's folder>/game.<ext>` before the game starts (see
  `systems.ts`); its saves folder is swapped to memory (never IndexedDB);
  RetroArch has no save callback, so the save is checked every 2 s; states
  are wrapped in a PNG of the screen (`png.ts`, chunk `prLs`); mouse
  positions on the canvas are corrected for the pixel ratio (RetroArch
  assumes device pixels).
- Every `/api/` call needs the token (bearer or the derived session
  cookie), and state-changing requests from another origin are refused.
- **Low memory is a feature.** The server idles at a few MB. Direct Go
  dependency: modernc.org/sqlite (pure Go, cgo-free). Justify any new one.
- UI style is Foyer's: Swiss editorial, always dark, heavy Inter headlines,
  tracked uppercase eyebrows, 2px rules over numbered headings, square
  corners, one accent (#2563ff). Check phone width, portrait and landscape.

## Commits

Conventional Commits: `<type>(<scope>): <summary>`, e.g. `feat(player): ...`.

## Checks before pushing

```sh
test -z "$(gofmt -l .)" && go vet ./... && go test -race ./...   # needs web/dist
cd frontend && npm run format:check && npm run typecheck && npm test && npm run build
docker build -t parlor:dev .
```
