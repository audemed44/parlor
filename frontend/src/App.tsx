import { useEffect, useState } from "preact/hooks";
import { ArrowUpRight, FileDown, LogOut, Settings as SettingsIcon } from "lucide-preact";
import { api } from "./api";
import { GameDetail } from "./components/GameDetail";
import { ImportPage } from "./components/ImportPage";
import { Library } from "./components/Library";
import { Login } from "./components/Login";
import { PatchPage } from "./components/PatchPage";
import { Player } from "./components/Player";
import { SettingsPage } from "./components/SettingsPage";
import { ErrorNote } from "./components/ui";
import { playURL, system } from "./systems";
import type { Config, Game } from "./types";

// Routes live in the URL hash: #/, #/game/3, #/play/3, #/import,
// #/settings, #/patch.
type Route =
  | { page: "library" }
  | { page: "game" | "play"; id: number }
  | { page: "import" }
  | { page: "settings" }
  | { page: "patch" };

export function route(hash: string): Route {
  const m = hash.match(/^#\/(game|play)\/(\d+)$/);
  if (m) return { page: m[1] as "game" | "play", id: Number(m[2]) };
  if (hash === "#/import") return { page: "import" };
  if (hash === "#/settings") return { page: "settings" };
  if (hash === "#/patch") return { page: "patch" };
  return { page: "library" };
}

export function App() {
  const [signed, setSigned] = useState<boolean | null>(null),
    [error, setError] = useState(""),
    [games, setGames] = useState<Game[]>([]),
    [config, setConfig] = useState<Config | null>(null),
    [at, setAt] = useState<Route>(route(location.hash)),
    [version, setVersion] = useState(0);

  useEffect(() => {
    const out = () => setSigned(false);
    const hash = () => setAt(route(location.hash));
    window.addEventListener("parlor-unauthorized", out);
    window.addEventListener("hashchange", hash);
    return () => {
      window.removeEventListener("parlor-unauthorized", out);
      window.removeEventListener("hashchange", hash);
    };
  }, []);

  useEffect(() => {
    let live = true;
    Promise.all([api<Game[]>("games"), api<Config>("config")])
      .then(([g, c]) => {
        if (!live) return;
        setGames(g);
        setConfig(c);
        setSigned(true);
        setError("");
      })
      .catch((e) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, [version, signed]);

  const refresh = () => setVersion((v) => v + 1);

  if (signed === false) return <Login onDone={() => setSigned(true)} />;
  if (signed === null) return error ? <ErrorNote error={error} /> : null;

  if (at.page === "play") {
    const platform = games.find((g) => g.id === at.id)?.platform ?? "gba";
    // Games EmulatorJS runs only play on /play (see playURL).
    if (system(platform).ejs && location.pathname !== "/play") {
      location.replace(playURL({ id: at.id, platform }));
      return null;
    }
    return (
      <Player
        key={at.id}
        id={at.id}
        platform={platform}
        onExit={() => {
          refresh();
          location.hash = `#/game/${at.id}`;
        }}
      />
    );
  }

  async function logout() {
    await api("logout", {}).catch(() => {});
    setSigned(false);
  }

  return (
    <div class="shell">
      <header class="topbar">
        {config?.foyer_url && (
          <a class="home-link" href={config.foyer_url} title="Back to Foyer">
            <ArrowUpRight size={12} />
            <span class="home-link-text">FOYER</span>
          </a>
        )}
        <a class="brand" href="#/">
          <span class="brand-mark" />
          PARLOR
        </a>
        <span class="brand-sub">GBA LIBRARY</span>
        <div class="spacer" />
        {config?.imports && (
          <a class="icon-btn" href="#/import" title="Import saves">
            <FileDown size={17} />
          </a>
        )}
        <a class="icon-btn" href="#/settings" title="Settings">
          <SettingsIcon size={17} />
        </a>
        <button class="icon-btn" title="Sign out" onClick={logout}>
          <LogOut size={17} />
        </button>
      </header>
      <ErrorNote error={error} />
      {at.page === "game" ? (
        <GameDetail key={at.id} id={at.id} games={games} onChange={refresh} />
      ) : at.page === "settings" ? (
        <SettingsPage />
      ) : at.page === "patch" ? (
        <PatchPage games={games} onChange={refresh} />
      ) : at.page === "import" ? (
        <ImportPage games={games} onChange={refresh} />
      ) : (
        <Library games={games} onChange={refresh} />
      )}
      <footer>
        <span>PARLOR · SAVES ON YOUR SERVER</span>
        <a href={`/core/${__CORE_VERSION__}/README.md`}>mGBA {__CORE_VERSION__} · MPL-2.0</a>
        <a href={`/ejs/${__EJS_VERSION__}/NOTICE.txt`}>EmulatorJS {__EJS_VERSION__} · GPL-3.0</a>
      </footer>
    </div>
  );
}
