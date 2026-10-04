import { useRef, useState } from "preact/hooks";
import { Wand2 } from "lucide-preact";
import { upload } from "../api";
import { deviceName } from "../lib";
import type { Game } from "../types";
import { ErrorNote } from "./ui";

// patchTitle suggests a name for the patched game from the patch's file
// name: "Pokemon Heart and Soul v2.1.bps" → "Pokemon Heart and Soul v2.1".
export function patchTitle(file: string): string {
  return file
    .replace(/\.(ips|ups|bps)$/i, "")
    .replace(/_/g, " ")
    .trim();
}

// PatchForm applies an .ips, .ups or .bps patch to a base ROM, making a new
// game. From a game's page (from), it's an update: the game's save, notes
// and play time carry over, and it can be hidden afterwards.
export function PatchForm({
  games,
  from,
  onDone,
}: {
  games: Game[];
  from?: Game;
  // The library changed: a game was added, and maybe one hidden.
  onDone: () => void;
}) {
  const [file, setFile] = useState<File | null>(null),
    [title, setTitle] = useState(""),
    [base, setBase] = useState(0),
    [carry, setCarry] = useState(true),
    [hide, setHide] = useState(true),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const ips = !!file && /\.ips$/i.test(file.name);

  function choose(f: File | undefined) {
    if (!f) return;
    setFile(f);
    setTitle(patchTitle(f.name));
    setError("");
  }

  async function apply(e: Event) {
    e.preventDefault();
    if (!file) return;
    setBusy(true);
    setError("");
    try {
      const fields: Record<string, string> = {
        title,
        base: String(base),
        device: deviceName(),
      };
      if (from && carry) {
        fields.carry = String(from.id);
        if (hide) fields.hide = "1";
      }
      const g = await upload<Game>("patch", file, fields);
      onDone();
      location.hash = `#/game/${g.id}`;
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="patch-form" onSubmit={apply}>
      <div class="field">
        <span class="eyebrow">Patch</span>
        <div class="file-pick">
          <button type="button" class="btn" onClick={() => input.current?.click()}>
            Choose .ips, .ups or .bps
          </button>
          <span class="mono hint">{file?.name ?? "None chosen"}</span>
        </div>
        <input
          ref={input}
          type="file"
          accept=".ips,.ups,.bps"
          hidden
          onChange={(e) => {
            choose(e.currentTarget.files?.[0]);
            e.currentTarget.value = "";
          }}
        />
      </div>
      <label class="field">
        <span class="eyebrow">Base ROM</span>
        <select value={base} onChange={(e) => setBase(Number(e.currentTarget.value))}>
          <option value={0}>Find it from the patch (UPS, BPS)</option>
          {games
            .filter((g) => !g.missing)
            .map((g) => (
              <option value={g.id} key={g.id}>
                {g.title}
              </option>
            ))}
        </select>
        <span class="hint">
          {ips && !base
            ? "IPS patches don't say which ROM they're for: choose the clean ROM the hack is made from."
            : "The clean ROM the hack is made from, not an older version of the hack."}
        </span>
      </label>
      <label class="field">
        <span class="eyebrow">Name</span>
        <input
          value={title}
          onInput={(e) => setTitle(e.currentTarget.value)}
          placeholder="Pokémon Heart and Soul (v2.1)"
          maxLength={150}
        />
      </label>
      {from && (
        <div class="checks">
          <label>
            <input
              type="checkbox"
              checked={carry}
              onChange={(e) => setCarry(e.currentTarget.checked)}
            />
            Carry this game's save, notes and play time over
          </label>
          <label class={carry ? "" : "muted"}>
            <input
              type="checkbox"
              checked={hide}
              disabled={!carry}
              onChange={(e) => setHide(e.currentTarget.checked)}
            />
            Hide this version from the library afterwards
          </label>
        </div>
      )}
      <ErrorNote error={error} />
      <button class="btn primary" disabled={!file || !title.trim() || busy || (ips && !base)}>
        <Wand2 size={15} /> {busy ? "Patching…" : "Apply the patch"}
      </button>
    </form>
  );
}
