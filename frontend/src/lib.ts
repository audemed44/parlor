// ago is a short relative time: "just now", "5 min ago", "3 h ago",
// "yesterday", "12 Sep".
export function ago(iso: string, now = Date.now()): string {
  if (!iso) return "never";
  const t = new Date(iso).getTime();
  const s = Math.max(0, (now - t) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  if (s < 2 * 86400) return "yesterday";
  if (s < 7 * 86400) return `${Math.floor(s / 86400)} days ago`;
  const d = new Date(t);
  return d.toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year: d.getFullYear() === new Date(now).getFullYear() ? undefined : "numeric",
  });
}

// when is an absolute date and time for history lists.
export function when(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// duration formats play time: "45 min", "12 h 5 min".
export function duration(seconds: number): string {
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  return m % 60 ? `${h} h ${m % 60} min` : `${h} h`;
}

export function size(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

// deviceName labels this device in the save history.
export function deviceName(ua = navigator.userAgent, touch = navigator.maxTouchPoints): string {
  if (/iPhone/.test(ua)) return "iPhone";
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && touch > 1)) return "iPad";
  if (/Android/.test(ua)) return /Mobile/.test(ua) ? "Android phone" : "Android tablet";
  if (/Steam ?Deck|SteamOS/i.test(ua)) return "Steam Deck";
  if (/Macintosh/.test(ua)) return "Mac";
  if (/Windows/.test(ua)) return "Windows";
  if (/Linux|X11/.test(ua)) return "Linux";
  return "Browser";
}

export const sources: Record<string, string> = {
  play: "Played",
  upload: "Uploaded",
  import: "Imported",
  restore: "Restored",
  carry: "Carried over",
};

// coverText is the big text on a game's tile: the title without a leading
// "Pokémon", which nearly every ROM hack shares.
export function coverText(title: string): string {
  const t = title
    .replace(/\s*[([].*?[)\]]/g, "")
    .replace(/^pok[eé]mon\s*[-:]?\s*/i, "")
    .replace(/[_]+/g, " ")
    .trim();
  return t || title;
}
