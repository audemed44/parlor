import { useEffect, useState } from "preact/hooks";
import { CloudOff } from "lucide-preact";
import { ago } from "../lib";
import { pending, type Pending } from "../pending";
import { send } from "../sync";
import type { Save } from "../types";

// Unsent lists saves this device made but couldn't upload (offline, or the
// app closed first), retries them, and asks what to do when another device
// saved the same game in the meantime.
export function Unsent({ onChange }: { onChange: () => void }) {
  const [items, setItems] = useState<{ p: Pending; conflict?: Save }[]>([]);

  async function retry() {
    const all = await pending.all().catch(() => [] as Pending[]);
    const out: { p: Pending; conflict?: Save }[] = [];
    let sent = false;
    for (const p of all) {
      const r = await send(p).catch(() => null);
      if (r?.ok) sent = true;
      else out.push({ p, conflict: r && "conflict" in r ? r.conflict : undefined });
    }
    setItems(out);
    if (sent) onChange();
  }

  useEffect(() => {
    retry();
    window.addEventListener("online", retry);
    return () => window.removeEventListener("online", retry);
  }, []);

  if (!items.length) return null;
  return (
    <div class="unsent">
      {items.map(({ p, conflict }) => (
        <div class="notice" key={p.gameId}>
          <CloudOff size={16} />
          <div class="spacer">
            <strong>{p.title}</strong>: a save from {ago(p.at)} hasn't reached the server.
            {conflict &&
              ` ${conflict.device || "Another device"} has saved since (${ago(conflict.created)}).`}
          </div>
          {conflict ? (
            <>
              <button
                class="btn"
                onClick={async () => {
                  await send(p, true).catch(() => null);
                  retry();
                  onChange();
                }}
              >
                Upload anyway
              </button>
              <button
                class="btn"
                onClick={async () => {
                  await pending.remove(p);
                  retry();
                }}
              >
                Discard
              </button>
            </>
          ) : (
            <button class="btn" onClick={retry}>
              Retry
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
