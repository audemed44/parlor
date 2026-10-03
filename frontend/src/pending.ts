// Saves waiting to reach the server, in IndexedDB, so progress survives
// the app being closed or the network dropping. One per game: a newer save
// replaces an unsent older one.
export interface Pending {
  gameId: number;
  title: string;
  // The server version this save was made on top of.
  base: number;
  data: Uint8Array;
  at: string;
}

const DB = "parlor";
const STORE = "pending";

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE, { keyPath: "gameId" });
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function run<T>(
  mode: IDBTransactionMode,
  f: (s: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const db = await open();
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = db.transaction(STORE, mode);
      const req = f(tx.objectStore(STORE));
      tx.oncomplete = () => resolve(req.result);
      tx.onerror = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}

export const pending = {
  put: (p: Pending) => run("readwrite", (s) => s.put(p)).then(() => {}),
  get: (gameId: number) => run<Pending | undefined>("readonly", (s) => s.get(gameId)),
  all: () => run<Pending[]>("readonly", (s) => s.getAll()),
  // remove deletes the entry only if it's still the one given, so a newer
  // save queued meanwhile isn't lost.
  remove: async (p: Pending) => {
    const now = await pending.get(p.gameId);
    if (now && now.at === p.at) await run("readwrite", (s) => s.delete(p.gameId));
  },
};
