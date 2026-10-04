// Saves waiting to reach the server, in IndexedDB, so progress survives
// the app being closed or the network dropping. One per game: a newer save
// replaces an unsent older one. The state taken when you leave a game waits
// the same way.
export interface Pending {
  gameId: number;
  title: string;
  // The server version this save was made on top of.
  base: number;
  data: Uint8Array;
  at: string;
}

export interface PendingState {
  gameId: number;
  data: Uint8Array;
  at: string;
}

const DB = "parlor";

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 2);
    req.onupgradeneeded = () => {
      for (const name of ["pending", "states"]) {
        if (!req.result.objectStoreNames.contains(name)) {
          req.result.createObjectStore(name, { keyPath: "gameId" });
        }
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function run<T>(
  store: string,
  mode: IDBTransactionMode,
  f: (s: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const db = await open();
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = db.transaction(store, mode);
      const req = f(tx.objectStore(store));
      tx.oncomplete = () => resolve(req.result);
      tx.onerror = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}

function queue<T extends { gameId: number; at: string }>(store: string) {
  const q = {
    put: (p: T) => run(store, "readwrite", (s) => s.put(p)).then(() => {}),
    get: (gameId: number) => run<T | undefined>(store, "readonly", (s) => s.get(gameId)),
    all: () => run<T[]>(store, "readonly", (s) => s.getAll()),
    // remove deletes the entry only if it's still the one given, so a newer
    // one queued meanwhile isn't lost.
    remove: async (p: T) => {
      const now = await q.get(p.gameId);
      if (now && now.at === p.at) await run(store, "readwrite", (s) => s.delete(p.gameId));
    },
  };
  return q;
}

export const pending = queue<Pending>("pending");
export const pendingStates = queue<PendingState>("states");
