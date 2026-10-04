import { useEffect, useRef, useState } from "preact/hooks";
import { ArrowLeft, Download, History, Play, Trash2, Upload } from "lucide-preact";
import { api, upload } from "../api";
import { ago, deviceName, duration, size, sources, when } from "../lib";
import type { GameDetail as Detail, Save, State } from "../types";
import { Cover } from "./Cover";
import { ErrorNote, Section } from "./ui";

export function GameDetail({ id, onChange }: { id: number; onChange: () => void }) {
  const [game, setGame] = useState<Detail | null>(null),
    [notes, setNotes] = useState(""),
    [error, setError] = useState(""),
    [version, setVersion] = useState(0);
  const file = useRef<HTMLInputElement>(null);

  useEffect(() => {
    api<Detail>(`games/${id}`)
      .then((g) => {
        setGame(g);
        setNotes(g.notes);
      })
      .catch((e) => setError(e.message));
  }, [id, version]);

  function changed() {
    setVersion((v) => v + 1);
    onChange();
  }

  async function saveNotes() {
    if (!game || notes === game.notes) return;
    try {
      await api(`games/${id}/notes`, { notes });
      setGame({ ...game, notes });
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function restore(s: Save) {
    if (
      !confirm(
        `Make the save from ${when(s.created)} the current one? The current save stays in the history.`,
      )
    )
      return;
    try {
      await api(`saves/${s.id}/restore`, { device: deviceName() });
      changed();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function dropState(v: State) {
    const name = v.slot === 0 ? "where you left off" : `slot ${v.slot}`;
    if (!confirm(`Delete the save state in ${name}? The in-game save isn't affected.`)) return;
    try {
      await api(`games/${id}/states/${v.slot}`, undefined, "DELETE");
      changed();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function uploadSave(f: File) {
    try {
      await upload(`games/${id}/saves`, f, { device: deviceName() });
      changed();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  if (!game) return error ? <ErrorNote error={error} /> : <div class="loading">Loading…</div>;
  const latest = game.saves[0];
  return (
    <div class="page">
      <a class="text-link back" href="#/">
        <ArrowLeft size={14} /> Library
      </a>
      <section class="continue detail">
        <div class="continue-cover">
          <Cover title={game.title} big />
        </div>
        <div class="continue-text">
          <span class="eyebrow">
            <span class="accent">Game Boy Advance</span>
            <span class="slash">/</span>
            <span class="mono">{size(game.size)}</span>
          </span>
          <h1>{game.title}</h1>
          <div class="facts">
            <div>
              <span class="eyebrow">Played</span>
              <strong>{duration(game.play_seconds)}</strong>
              <span class="hint">{game.last_played ? ago(game.last_played) : "Never"}</span>
            </div>
            <div>
              <span class="eyebrow">Save</span>
              <strong>{latest ? ago(latest.created) : "None yet"}</strong>
              <span class="hint">
                {latest ? `${sources[latest.source]} · ${latest.device || "—"}` : "Starts fresh"}
              </span>
            </div>
          </div>
          {game.missing ? (
            <p class="error-text">
              The ROM is missing from the library folder; its saves are kept.
            </p>
          ) : (
            <a class="btn primary big" href={`#/play/${game.id}`}>
              <Play size={18} /> Play
            </a>
          )}
        </div>
      </section>
      <ErrorNote error={error} />

      <section>
        <Section index="01" title="Notes" />
        <textarea
          class="notes"
          placeholder="Hack version, where you are, what to do next…"
          value={notes}
          onInput={(e) => setNotes(e.currentTarget.value)}
          onBlur={saveNotes}
          maxLength={10000}
        />
      </section>

      <section>
        <Section index="02" title="Save history">
          <button class="btn" onClick={() => file.current?.click()}>
            <Upload size={15} /> Upload a save
          </button>
          <input
            ref={file}
            type="file"
            accept=".srm,.sav"
            hidden
            onChange={(e) => {
              const f = e.currentTarget.files?.[0];
              if (f) uploadSave(f);
              e.currentTarget.value = "";
            }}
          />
        </Section>
        <p class="hint section-note">
          Every in-game save is kept: the last 20, and one a day for a month before that. Saves are
          plain <code>.srm</code> files that work in other mGBA-based emulators.
        </p>
        {game.saves.length === 0 ? (
          <p class="muted">No saves yet. Play the game and save from its menu.</p>
        ) : (
          <div class="rows">
            {game.saves.map((s, i) => (
              <div class="row" key={s.id}>
                <History size={16} class="muted" />
                <div class="row-main">
                  <strong>{when(s.created)}</strong>
                  <span class="hint">
                    {sources[s.source]}
                    {s.device ? ` on ${s.device}` : ""}
                    {s.note ? ` · ${s.note}` : ""}
                  </span>
                </div>
                {i === 0 ? <span class="chip current">Current</span> : null}
                <span class="mono hint">{size(s.size)}</span>
                <a class="icon-btn" href={`/api/saves/${s.id}`} title="Download" download>
                  <Download size={16} />
                </a>
                {i > 0 && (
                  <button class="btn small" onClick={() => restore(s)}>
                    Restore
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      <section>
        <Section index="03" title="Save states" />
        <p class="hint section-note">
          Snapshots from the player's menu, and the one taken when you leave, so you carry on from
          the same moment on any device. Loading one leaves the in-game save as it is.
        </p>
        {game.states.length === 0 ? (
          <p class="muted">No save states yet.</p>
        ) : (
          <div class="state-grid">
            {game.states.map((v) => (
              <div class="slot" key={v.slot}>
                <div class="slot-shot">
                  {v.image && (
                    <img
                      src={`/api/games/${id}/states/${v.slot}/image?v=${v.sha256.slice(0, 12)}`}
                      alt=""
                    />
                  )}
                  <span class="slot-n">{v.slot === 0 ? "LEFT OFF" : v.slot}</span>
                </div>
                <div class="row-main">
                  <strong>{when(v.created)}</strong>
                  <span class="hint">
                    {v.device || "—"}
                    {v.note ? ` · ${v.note}` : ""}
                  </span>
                </div>
                <button class="btn small" onClick={() => dropState(v)}>
                  <Trash2 size={13} /> Delete
                </button>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
