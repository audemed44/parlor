# Parlor

Instructions for coding agents working in this repository. `CLAUDE.md`
imports this file.

## Project

A GBA library you can play from any browser, iPhone first, with every
game's save kept on the server. The emulator is mGBA compiled to
WebAssembly (`@thenick775/mgba-wasm`); it runs in the browser, and the
server only serves files and keeps saves. A Go server (`cmd/parlor`,
`internal/`) serves a JSON API and the Preact + TypeScript frontend
(`frontend/`), built into `web/dist` and embedded in the binary. State is
SQLite at `/data/parlor.db` plus each save version under
`/data/saves/<game>/<version>.srm`.

- `internal/library`: finding ROMs, and matching save file names to games.
- `internal/store`: games (scanned from the read-only ROM folder), save
  versions with conflict detection and thinning, imports.
- `internal/server`: HTTP API, auth, cross-origin isolation headers.
- `internal/testrom`: a tiny homebrew ROM that counts A presses in SRAM;
  `go run ./cmd/testrom OUT.gba` writes it.
- `frontend/src/controls.ts`: touch control layout and hit-testing (shared
  by drawing and input). `emulator.ts` wraps the core, `sync.ts` and
  `pending.ts` upload saves through an IndexedDB queue.

## Constraints

- **Never use real ROMs** in tests, fixtures, screenshots or CI. Use the
  homebrew test ROM. Don't read the user's library or saves to build things.
- The ROM folder is read-only. Saves are never overwritten or deleted except
  by the version thinning; restoring adds a new version.
- Only in-game saves (`.srm`) are canonical. A save made on top of an older
  version than the server's latest is a conflict for the user to resolve.
- The core needs cross-origin isolation (COOP/COEP) and loads from
  `/core/<version>/`; keep every resource same-origin.
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
