import { useEffect, useState } from "preact/hooks";
import { ArrowLeft, Check, FileDown } from "lucide-preact";
import { api } from "../api";
import { size, when } from "../lib";
import { quickSlots, type Candidate, type Game } from "../types";
import { Empty, ErrorNote, Section } from "./ui";

// ImportPage brings in saves from the import folder (RomM's saves, copied
// RetroDECK saves), matched to games by name. Each is added to the game's
// history dated by the file, so an old file never replaces a newer save.
// Save states go into a slot: each game's newest into "where you left off",
// so the player offers to carry on from it, and older ones into slots 1-4.
export function defaultSlots(files: Candidate[]): Record<string, number> {
  const next: Record<number, number> = {};
  const out: Record<string, number> = {};
  for (const f of files) {
    if (f.kind !== "state") continue;
    const game = f.imported || f.suggested;
    const n = next[game] ?? 0;
    out[f.path] = Math.min(n, quickSlots.length);
    next[game] = n + 1;
  }
  return out;
}

export function ImportPage({ games, onChange }: { games: Game[]; onChange: () => void }) {
  const [files, setFiles] = useState<Candidate[] | null>(null),
    [choice, setChoice] = useState<Record<string, number>>({}),
    [slots, setSlots] = useState<Record<string, number>>({}),
    [busy, setBusy] = useState(""),
    [error, setError] = useState("");

  async function load() {
    try {
      const c = await api<Candidate[]>("imports");
      setFiles(c);
      setChoice(Object.fromEntries(c.map((f) => [f.path, f.imported || f.suggested])));
      setSlots((s) => ({ ...defaultSlots(c), ...s }));
    } catch (e) {
      setError((e as Error).message);
    }
  }
  useEffect(() => {
    load();
  }, []);

  async function run(paths: string[]) {
    setError("");
    for (const path of paths) {
      setBusy(path);
      try {
        await api("imports", { path, game_id: choice[path], slot: slots[path] });
      } catch (e) {
        setError(`${path}: ${(e as Error).message}`);
        break;
      }
    }
    setBusy("");
    await load();
    onChange();
  }

  const ready = (files ?? []).filter((f) => !f.imported && choice[f.path]);
  return (
    <div class="page">
      <a class="text-link back" href="#/">
        <ArrowLeft size={14} /> Library
      </a>
      <div class="page-head">
        <span class="eyebrow">
          <span class="accent">Import</span>
          <span class="slash">/</span>
          One-time
        </span>
        <h1>{"Bring your\nsaves along."}</h1>
        <p class="lede">
          In-game saves (<code>.srm</code>, <code>.sav</code>) and save states (<code>.state</code>,{" "}
          <code>.ss1</code>) found in the import folder, matched to games by name. Check the
          matches, then import. A state imported into "Left off" is offered when the game starts.
        </p>
      </div>
      <ErrorNote error={error} />
      <section>
        <Section index="01" title="Save files">
          {ready.length > 1 && (
            <button
              class="btn primary"
              disabled={!!busy}
              onClick={() => run(ready.map((f) => f.path))}
            >
              Import {ready.length}
            </button>
          )}
        </Section>
        {files === null ? (
          <div class="loading">Looking…</div>
        ) : files.length === 0 ? (
          <Empty title="Nothing to import">
            No saves (.srm, .sav) or save states (.state, .ss1) in the import folder.
          </Empty>
        ) : (
          <div class="rows">
            {files.map((f) => (
              <div class={"row import-row" + (f.imported ? " done" : "")} key={f.path}>
                <FileDown size={16} class="muted" />
                <div class="row-main">
                  <strong class="path">{f.path}</strong>
                  <span class="hint">
                    {f.kind === "state" ? "Save state" : "In-game save"} · {when(f.modified)} ·{" "}
                    {size(f.size)}
                  </span>
                </div>
                {f.kind === "state" && (
                  <select
                    class="slot-select"
                    aria-label="Slot"
                    value={slots[f.path] ?? 0}
                    disabled={!!f.imported}
                    onChange={(e) =>
                      setSlots({ ...slots, [f.path]: Number(e.currentTarget.value) })
                    }
                  >
                    <option value={0}>Left off</option>
                    {quickSlots.map((n) => (
                      <option value={n} key={n}>
                        Slot {n}
                      </option>
                    ))}
                  </select>
                )}
                <select
                  value={choice[f.path] || 0}
                  disabled={!!f.imported}
                  onChange={(e) =>
                    setChoice({ ...choice, [f.path]: Number(e.currentTarget.value) })
                  }
                >
                  <option value={0}>Choose a game…</option>
                  {games.map((g) => (
                    <option value={g.id} key={g.id}>
                      {g.title}
                    </option>
                  ))}
                </select>
                {f.imported ? (
                  <span class="chip current">
                    <Check size={12} /> Imported
                  </span>
                ) : (
                  <button
                    class="btn small"
                    disabled={!choice[f.path] || !!busy}
                    onClick={() => run([f.path])}
                  >
                    {busy === f.path ? "Importing…" : "Import"}
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
