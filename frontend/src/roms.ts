// ROMs are kept in the browser's Cache Storage, keyed by checksum, so a
// game downloads once per device (32 MB on a phone adds up).
const CACHE = "parlor-roms";

function key(id: number, sha1: string) {
  return `/api/games/${id}/rom?v=${sha1}`;
}

export async function loadROM(
  id: number,
  sha1: string,
  progress: (fraction: number) => void,
): Promise<Uint8Array> {
  const cache = self.caches ? await caches.open(CACHE).catch(() => null) : null;
  const hit = await cache?.match(key(id, sha1));
  if (hit) {
    progress(1);
    return new Uint8Array(await hit.arrayBuffer());
  }
  const res = await fetch(`/api/games/${id}/rom`);
  if (res.status === 401) window.dispatchEvent(new Event("parlor-unauthorized"));
  if (!res.ok) {
    const data = await res.json().catch(() => ({ error: "Couldn't download the ROM" }));
    throw new Error(data.error);
  }
  const total = Number(res.headers.get("Content-Length") ?? 0);
  const reader = res.body!.getReader();
  const chunks: Uint8Array[] = [];
  let got = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    got += value.length;
    if (total) progress(got / total);
  }
  const rom = new Uint8Array(got);
  let at = 0;
  for (const c of chunks) {
    rom.set(c, at);
    at += c.length;
  }
  if (cache) {
    // Drop older copies of this game, then keep this one.
    for (const req of await cache.keys()) {
      if (new URL(req.url).pathname === `/api/games/${id}/rom`) await cache.delete(req);
    }
    await cache.put(key(id, sha1), new Response(rom as BodyInit)).catch(() => {});
  }
  progress(1);
  return rom;
}
