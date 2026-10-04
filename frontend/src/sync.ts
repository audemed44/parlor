// Sending in-game saves to the server.
import { HTTPError, putBytes } from "./api";
import { deviceName } from "./lib";
import { pending, pendingStates, type Pending, type PendingState } from "./pending";
import type { Save, State } from "./types";

export type Upload =
  { ok: true; save: Save } | { ok: false; conflict: Save } | { ok: false; offline: true };

// send uploads a queued save. On success the queue entry goes; on a
// conflict or a network failure it stays, to be resolved or retried.
export async function send(p: Pending, force = false): Promise<Upload> {
  const q = new URLSearchParams({ base: String(p.base), device: deviceName() });
  if (force) q.set("force", "1");
  try {
    const save = await putBytes<Save>(`games/${p.gameId}/save?${q}`, p.data);
    await pending.remove(p);
    return { ok: true, save };
  } catch (e) {
    if (e instanceof HTTPError && e.status === 409) {
      return { ok: false, conflict: e.data.latest as Save };
    }
    if (e instanceof HTTPError && e.status !== 0 && e.status < 500) {
      // The server refused it for good (game gone, bad save): drop it.
      await pending.remove(p);
      throw e;
    }
    return { ok: false, offline: true };
  }
}

// sendState uploads the state taken when leaving a game. The server keeps
// whichever is newer, so a late upload can't replace a later state from
// another device. It returns false when it should be retried.
export async function sendState(p: PendingState): Promise<boolean> {
  const q = new URLSearchParams({ device: deviceName(), at: p.at });
  try {
    await putBytes<State>(`games/${p.gameId}/states/0?${q}`, p.data);
  } catch (e) {
    if (!(e instanceof HTTPError) || e.status >= 500) return false;
  }
  await pendingStates.remove(p);
  return true;
}

// flushStates retries every state still waiting.
export async function flushStates() {
  for (const p of await pendingStates.all().catch(() => [] as PendingState[])) await sendState(p);
}

// digest is a short fingerprint for "has the save changed?".
export async function digest(data: Uint8Array): Promise<string> {
  if (!crypto.subtle) {
    let h = 0;
    for (let i = 0; i < data.length; i++) h = (h * 31 + data[i]) | 0;
    return `${data.length}:${h}`;
  }
  const d = await crypto.subtle.digest("SHA-256", data as BufferSource);
  return Array.from(new Uint8Array(d), (b) => b.toString(16).padStart(2, "0")).join("");
}
