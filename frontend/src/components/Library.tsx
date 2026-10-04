import { useMemo, useState } from "preact/hooks";
import { Play, RefreshCw, Search, Wand2 } from "lucide-preact";
import { api } from "../api";
import { ago, duration, plural } from "../lib";
import type { Game, ScanResult } from "../types";
import { coverURL } from "../covers";
import { Cover } from "./Cover";
import { Empty, ErrorNote, Section } from "./ui";
import { Unsent } from "./Unsent";

export function Library({ games, onChange }: { games: Game[]; onChange: () => void }) {
  const [query, setQuery] = useState(""),
    [busy, setBusy] = useState(false),
    [note, setNote] = useState(""),
    [error, setError] = useState("");
  const [showHidden, setShowHidden] = useState(false);
  const recent = games.find((g) => g.last_played && !g.missing && !g.hidden);
  const hidden = games.filter((g) => g.hidden).length;
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    let list = showHidden ? games : games.filter((g) => !g.hidden);
    if (q) list = list.filter((g) => g.title.toLowerCase().includes(q));
    return [...list].sort((a, b) => a.title.localeCompare(b.title));
  }, [games, query, showHidden]);

  async function scan() {
    setBusy(true);
    setError("");
    try {
      const r = await api<ScanResult>("library/scan", {});
      setNote(
        plural(r.total, "game") +
          (r.added ? ` · ${r.added} new` : "") +
          (r.missing ? ` · ${r.missing} missing` : ""),
      );
      onChange();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="page">
      <Unsent onChange={onChange} />
      {recent && (
        <section class="continue">
          <a class="continue-cover" href={`#/game/${recent.id}`}>
            <Cover title={recent.title} big src={coverURL(recent)} />
          </a>
          <div class="continue-text">
            <span class="eyebrow">
              <span class="accent">Continue</span>
              <span class="slash">/</span>
              {ago(recent.last_played)}
            </span>
            <h1>{recent.title}</h1>
            <p class="lede">
              {duration(recent.play_seconds)} played
              {recent.save
                ? ` · saved ${ago(recent.save.created)} on ${recent.save.device || "the server"}`
                : ""}
            </p>
            <a class="btn primary big" href={`#/play/${recent.id}`}>
              <Play size={18} /> Play
            </a>
          </div>
        </section>
      )}
      {!recent && (
        <div class="page-head">
          <span class="eyebrow">
            <span class="accent">Library</span>
            <span class="slash">/</span>
            Game Boy Advance
          </span>
          <h1>{"Pick a game,\nany game."}</h1>
        </div>
      )}

      <section>
        <Section index="01" title="Library">
          <span class="muted mono small">
            {note || plural(games.filter((g) => !g.missing && !g.hidden).length, "game")}
          </span>
          <a class="icon-btn" href="#/patch" title="Patch a ROM hack">
            <Wand2 size={16} />
          </a>
          <button class="icon-btn" title="Rescan the library folder" onClick={scan} disabled={busy}>
            <RefreshCw size={16} class={busy ? "spin" : ""} />
          </button>
        </Section>
        <ErrorNote error={error} />
        {games.length > 8 && (
          <label class="search">
            <Search size={15} />
            <input
              type="search"
              placeholder="Search games"
              value={query}
              onInput={(e) => setQuery(e.currentTarget.value)}
            />
          </label>
        )}
        {games.length === 0 ? (
          <Empty title="No games yet">
            Put <code>.gba</code> files in the library folder (<code>PARLOR_ROMS</code>), then
            rescan.
          </Empty>
        ) : (
          <div class="grid">
            {shown.map((g) => (
              <a
                class={"tile" + (g.missing || g.hidden ? " missing" : "")}
                href={`#/game/${g.id}`}
                key={g.id}
              >
                <Cover title={g.title} src={coverURL(g)} />
                <strong>{g.title}</strong>
                <span class="hint">
                  {g.missing
                    ? "ROM missing"
                    : g.last_played
                      ? `${ago(g.last_played)} · ${duration(g.play_seconds)}`
                      : g.save
                        ? `Saved ${ago(g.save.created)}`
                        : "Not played yet"}
                </span>
              </a>
            ))}
          </div>
        )}
        {hidden > 0 && (
          <button class="text-link hidden-toggle" onClick={() => setShowHidden(!showHidden)}>
            {showHidden ? "Leave out hidden games" : `Show ${hidden} hidden`}
          </button>
        )}
      </section>
    </div>
  );
}
