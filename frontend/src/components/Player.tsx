import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { mGBAEmulator } from "@thenick775/mgba-wasm";
import {
  ArrowLeft,
  FastForward,
  Gauge,
  Play,
  Save as SaveIcon,
  Volume2,
  VolumeX,
} from "lucide-preact";
import { api, bytes, putBytes } from "../api";
import { layout as placeControls, type Key, type Layout } from "../controls";
import { Controls } from "./Controls";
import {
  afterStart,
  captureState,
  emulator,
  replaceSave,
  restoreState,
  resumeAudio,
  screen,
  start,
  stop,
} from "../emulator";
import { ago, deviceName } from "../lib";
import { pending, pendingStates, type Pending } from "../pending";
import { loadROM } from "../roms";
import { digest, send, sendState } from "../sync";
import { quickSlots, type GameDetail, type Save, type Settings, type State } from "../types";

type Phase = "loading" | "ready" | "playing" | "error";

// What the save indicator shows.
type Status =
  { kind: "idle" } | { kind: "saving" } | { kind: "saved"; at: string } | { kind: "offline" };

// A save that couldn't be sent because another device saved first.
interface Clash {
  mine: Pending;
  theirs: Save;
  // Before the game starts, choosing decides which save it starts from.
  beforeStart: boolean;
}

const touch = typeof matchMedia !== "undefined" && matchMedia("(pointer: coarse)").matches;

// Keyboard on desktop. SDL key names.
const keyboard: [string, Key][] = [
  ["X", "A"],
  ["Z", "B"],
  ["A", "L"],
  ["S", "R"],
  ["Return", "Start"],
  ["Backspace", "Select"],
  ["Up", "Up"],
  ["Down", "Down"],
  ["Left", "Left"],
  ["Right", "Right"],
];

export function Player({ id, onExit }: { id: number; onExit: () => void }) {
  const [phase, setPhase] = useState<Phase>("loading");
  const [error, setError] = useState("");
  const [game, setGame] = useState<GameDetail | null>(null);
  const [progress, setProgress] = useState(0);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [menu, setMenu] = useState(false);
  const [clash, setClash] = useState<Clash | null>(null);
  const [fast, setFast] = useState(false);
  const [speed, setSpeed] = useState(2);
  const [muted, setMuted] = useState(false);
  const [area, setArea] = useState<Layout | null>(null);
  const [states, setStates] = useState<State[]>([]);
  const [slotNote, setSlotNote] = useState("");

  const areaRef = useRef<HTMLDivElement>(null);
  const holder = useRef<HTMLDivElement>(null);
  const core = useRef<mGBAEmulator | null>(null);
  const rom = useRef<Uint8Array | null>(null);
  const initial = useRef<Uint8Array | null>(null);
  // The state taken when this game was last left, on any device.
  const resumeFrom = useRef<Uint8Array | null>(null);
  // The server's save version the running game descends from.
  const base = useRef(0);
  const lastHash = useRef("");
  const dirty = useRef(false);
  const timer = useRef<number>(0);
  const chain = useRef<Promise<void>>(Promise.resolve());
  const paused = useRef(false);
  const conflicted = useRef(false);

  // Lay out the screen and controls for the space available.
  useLayoutEffect(() => {
    const el = areaRef.current!;
    const measure = () => {
      const { width, height } = el.getBoundingClientRect();
      setArea(touch ? placeControls(width, height) : desktopLayout(width, height));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    holder.current?.appendChild(screen());
  }, [area !== null]);

  // Load everything; the game starts on a tap.
  useEffect(() => {
    let live = true;
    const boot = emulator();
    api<Settings>("settings")
      .then((s) => live && setSpeed(s.fast_forward))
      .catch(() => {});
    (async () => {
      const g = await api<GameDetail>(`games/${id}`);
      if (!live) return;
      setGame(g);
      if (g.missing) throw new Error("This game's ROM is missing from the library folder.");
      const romLoad = loadROM(g.id, g.sha1, (f) => live && setProgress(f));
      const waiting = await pending.get(id).catch(() => undefined);
      if (waiting) {
        const r = await send(waiting);
        if (!r.ok && "conflict" in r) {
          setClash({ mine: waiting, theirs: r.conflict, beforeStart: true });
        }
      }
      // A state taken when leaving that never reached the server goes
      // first, so it can be carried on from.
      const left = await pendingStates.get(id).catch(() => undefined);
      if (left) await sendState(left);
      const st = await api<State[]>(`games/${id}/states`);
      if (!live) return;
      setStates(st);
      if (st.some((v) => v.slot === 0)) {
        resumeFrom.current = (await bytes(`games/${id}/states/0`))?.data ?? null;
      }
      rom.current = await romLoad;
      core.current = await boot;
      await fetchLatest();
      if (live) setPhase("ready");
    })().catch((e) => {
      if (live) {
        setError(e.message);
        setPhase("error");
      }
    });
    return () => {
      live = false;
    };
  }, [id]);

  async function fetchLatest() {
    const latest = await bytes(`games/${id}/save`);
    initial.current = latest?.data ?? null;
    base.current = latest?.id ?? 0;
    lastHash.current = latest ? await digest(latest.data) : "";
  }

  // queue captures the in-game save and sends it, one at a time.
  function queue() {
    chain.current = chain.current.then(capture).catch(() => setStatus({ kind: "offline" }));
    return chain.current;
  }

  async function capture() {
    const m = core.current;
    if (!m || !dirty.current) return;
    dirty.current = false;
    const data = m.getSave();
    if (!data || !data.length) return;
    const hash = await digest(data);
    if (hash === lastHash.current) return;
    const p: Pending = {
      gameId: id,
      title: game?.title ?? "",
      base: base.current,
      data: data.slice(),
      at: new Date().toISOString(),
    };
    await pending.put(p).catch(() => {});
    lastHash.current = hash;
    await deliver(p);
  }

  async function deliver(p: Pending, force = false) {
    setStatus({ kind: "saving" });
    const r = await send(p, force);
    if (r.ok) {
      base.current = r.save.id;
      setStatus({ kind: "saved", at: r.save.created });
    } else if ("conflict" in r) {
      pause();
      conflicted.current = true;
      setClash({ mine: p, theirs: r.conflict, beforeStart: false });
      setStatus({ kind: "idle" });
    } else {
      setStatus({ kind: "offline" });
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => retry(), 15000);
    }
  }

  async function retry() {
    const p = await pending.get(id).catch(() => undefined);
    if (p) chain.current = chain.current.then(() => deliver(p));
  }

  function pause() {
    paused.current = true;
    core.current?.pauseGame();
  }
  function resume() {
    if (menu || clash) return;
    paused.current = false;
    core.current?.resumeGame();
    if (core.current) resumeAudio(core.current);
  }

  // listen watches for in-game saves. Loading a game resets the core's
  // callbacks, so this follows every load.
  function listen(m: mGBAEmulator) {
    m.addCoreCallbacks({
      saveDataUpdatedCallback: () => {
        dirty.current = true;
        window.clearTimeout(timer.current);
        timer.current = window.setTimeout(queue, 800);
      },
    });
  }

  // begin starts the game, from where it was left when carryOn is set.
  function begin(carryOn: boolean) {
    const m = core.current;
    if (!m || !rom.current || clash || !game) return;
    try {
      for (const [sdl, key] of keyboard) m.bindKey(sdl, key);
      start(m, id, rom.current, initial.current);
      listen(m);
      const state = carryOn ? resumeFrom.current : null;
      if (state) {
        afterStart(m, () => {
          if (!restoreState(m, id, 0, state)) {
            setSlotNote("Couldn't load where you left off; started from the in-game save.");
          }
          if (!paused.current) m.resumeGame();
        });
      }
      rom.current = null;
      resumeFrom.current = null;
      setPhase("playing");
    } catch (e) {
      setError((e as Error).message);
      setPhase("error");
    }
  }

  // While playing: save when the app goes to the background (iOS may kill
  // it there), pause while hidden, keep the screen awake, count play time.
  useEffect(() => {
    if (phase !== "playing") return;
    let lock: WakeLockSentinel | null = null;
    const wake = () =>
      navigator.wakeLock
        ?.request("screen")
        .then((l) => (lock = l))
        .catch(() => {});
    wake();
    const visibility = () => {
      if (document.hidden) {
        queue();
        core.current?.pauseGame();
        // iOS may close the app from here: keep the place.
        keepPlace();
      } else {
        if (!paused.current) core.current?.resumeGame();
        if (core.current) resumeAudio(core.current);
        wake();
        retry();
      }
    };
    const hide = () => queue();
    const online = () => retry();
    // iOS suspends audio after calls and other interruptions; any tap
    // brings it back.
    const tap = () => core.current && resumeAudio(core.current);
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("pagehide", hide);
    window.addEventListener("online", online);
    document.addEventListener("pointerdown", tap);
    let played = 0;
    const clock = window.setInterval(() => {
      if (document.hidden || paused.current) return;
      played += 15;
      if (played >= 60) {
        api(`games/${id}/played`, { seconds: played }).catch(() => {});
        played = 0;
      }
    }, 15000);
    api(`games/${id}/played`, { seconds: 0 }).catch(() => {});
    return () => {
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("pagehide", hide);
      window.removeEventListener("online", online);
      document.removeEventListener("pointerdown", tap);
      window.clearInterval(clock);
      if (played) api(`games/${id}/played`, { seconds: played }).catch(() => {});
      lock?.release().catch(() => {});
    };
  }, [phase]);

  useEffect(() => {
    if (menu || clash) pause();
    else if (phase === "playing") resume();
  }, [menu, clash]);

  // Desktop: Escape opens the menu, F toggles fast forward.
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (phase !== "playing" || clash || e.repeat) return;
      if (e.key === "Escape") setMenu((m) => !m);
      else if ((e.key === "f" || e.key === "F") && !menu) toggleFast();
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [phase, clash, menu, fast, speed]);

  async function quit() {
    setMenu(false);
    const m = core.current;
    if (m && phase === "playing") {
      await queue();
      // Choose which save to keep before leaving.
      if (conflicted.current) return;
      setStatus({ kind: "saving" });
      await keepPlace();
      m.addCoreCallbacks({ saveDataUpdatedCallback: null });
      stop(m);
    }
    onExit();
  }

  // keepPlace takes a state when leaving the game, so it carries on from
  // there next time, on any device. It waits on the device until sent.
  async function keepPlace() {
    const m = core.current;
    if (!m || conflicted.current) return;
    const data = captureState(m, id, 0);
    if (!data) return;
    const p = { gameId: id, data: data.slice(), at: new Date().toISOString() };
    await pendingStates.put(p).catch(() => {});
    await sendState(p);
  }

  async function saveSlot(slot: number) {
    const m = core.current;
    if (!m) return;
    const data = captureState(m, id, slot);
    if (!data) {
      setSlotNote("mGBA couldn't take a save state.");
      return;
    }
    setSlotNote("Saving…");
    try {
      const q = new URLSearchParams({ device: deviceName() });
      const v = await putBytes<State>(`games/${id}/states/${slot}?${q}`, data.slice());
      setStates((list) => [...list.filter((x) => x.slot !== slot), v]);
      setSlotNote(`Saved to slot ${slot}`);
    } catch (e) {
      setSlotNote(`Couldn't save to slot ${slot}: ${(e as Error).message}`);
    }
  }

  async function loadSlot(slot: number) {
    const m = core.current;
    if (!m) return;
    setSlotNote("Loading…");
    try {
      const got = await bytes(`games/${id}/states/${slot}`);
      if (!got || !restoreState(m, id, slot, got.data)) throw new Error("mGBA couldn't load it");
      setSlotNote(`Loaded slot ${slot}`);
      setMenu(false);
    } catch (e) {
      setSlotNote(`Couldn't load slot ${slot}: ${(e as Error).message}`);
    }
  }

  async function keepMine() {
    const c = clash!;
    conflicted.current = false;
    setClash(null);
    if (c.beforeStart) {
      const r = await send(c.mine, true);
      if (!r.ok) {
        setError("Couldn't upload this device's save. Try again when you're online.");
        setPhase("error");
        return;
      }
      await fetchLatest();
      return;
    }
    chain.current = chain.current.then(() => deliver(c.mine, true));
  }

  async function takeTheirs() {
    const c = clash!;
    conflicted.current = false;
    await pending.remove(c.mine).catch(() => {});
    if (c.beforeStart) {
      setClash(null);
      await fetchLatest();
      return;
    }
    const latest = await bytes(`games/${id}/save`).catch(() => null);
    if (latest && core.current) {
      base.current = latest.id;
      lastHash.current = await digest(latest.data);
      replaceSave(core.current, id, latest.data);
      listen(core.current);
      setStatus({ kind: "saved", at: c.theirs.created });
    }
    setClash(null);
  }

  // Dialogs open mid-game, often under a thumb that's still pressing A.
  // Their buttons only take taps that started on the dialog (or keys).
  const armed = useRef(false);
  useEffect(() => {
    armed.current = false;
  }, [menu, clash]);
  const arm = () => (armed.current = true);
  const guard = (f: () => void) => (e: MouseEvent) => {
    if (!armed.current && e.detail !== 0) return;
    f();
  };

  function key(k: Key, down: boolean) {
    const m = core.current;
    if (!m || phase !== "playing") return;
    if (down) m.buttonPress(k);
    else m.buttonUnpress(k);
  }

  function toggleFast() {
    const next = !fast;
    core.current?.setFastForwardMultiplier(next ? speed : 1);
    setFast(next);
  }
  function toggleSound() {
    const next = !muted;
    core.current?.setVolume(next ? 0 : 1);
    setMuted(next);
  }

  const box = area?.screen;
  const left = states.find((v) => v.slot === 0);
  const stateImage = (v: State) =>
    `/api/games/${id}/states/${v.slot}/image?v=${v.sha256.slice(0, 12)}`;
  return (
    <div class={`player ${touch ? "touch" : "desktop"}`}>
      <div class="player-area" ref={areaRef}>
        {box && (
          <div
            class="screen-box"
            ref={holder}
            style={{ left: box.x, top: box.y, width: box.w, height: box.h }}
          />
        )}
        {area && touch && (
          <Controls
            layout={area}
            onKey={key}
            onAction={(a) => (a === "Menu" ? setMenu(true) : toggleFast())}
            toggled={fast ? new Set(["Fast"]) : new Set()}
            fastLabel={`▶▶ ${speed}×`}
          />
        )}
        {!touch && phase === "playing" && (
          <div class="desk-bar">
            <span class="eyebrow">{game?.title}</span>
            <span class="hint mono">
              Arrows · X A · Z B · A L · S R · Enter Start · Backspace Select · F fast forward · Esc
              menu
            </span>
            <button class={"btn" + (fast ? " active" : "")} onClick={toggleFast}>
              <FastForward size={15} /> {speed}×
            </button>
            <button class="btn" onClick={() => setMenu(true)}>
              Menu
            </button>
          </div>
        )}
        {phase === "playing" && <SaveBadge status={status} />}
      </div>

      {phase !== "playing" && (
        <div class="player-overlay">
          <div class="overlay-card">
            <span class="eyebrow">
              <span class="accent">Parlor</span>
              <span class="slash">/</span>
              {game?.save ? `Saved ${ago(game.save.created)}` : "New game"}
            </span>
            <h2>{game?.title ?? "Loading"}</h2>
            {phase === "loading" && (
              <>
                <div class="bar">
                  <div style={{ width: `${Math.round(progress * 100)}%` }} />
                </div>
                <p class="hint">
                  {progress < 1
                    ? `Downloading ROM… ${Math.round(progress * 100)}%`
                    : "Starting mGBA…"}
                </p>
              </>
            )}
            {phase === "ready" && !clash && left && (
              <>
                {left.image && (
                  <img class="state-shot" src={stateImage(left)} alt="Where you left off" />
                )}
                <button class="btn primary big" onClick={() => begin(true)} autoFocus>
                  <Play size={18} /> Continue
                </button>
                <p class="hint">
                  You left off here {ago(left.created)}
                  {left.device ? ` on ${left.device}` : ""}.
                  {game?.save && game.save.created > left.created
                    ? ` The in-game save from ${ago(game.save.created)} is newer.`
                    : ""}
                </p>
                <button class="btn" onClick={() => begin(false)}>
                  {game?.save ? "Start from the in-game save" : "Start a new game"}
                </button>
              </>
            )}
            {phase === "ready" && !clash && !left && (
              <button class="btn primary big" onClick={() => begin(false)} autoFocus>
                <Play size={18} /> Tap to play
              </button>
            )}
            {phase === "error" && <p class="error-text">{error}</p>}
            <button class="text-link" onClick={quit}>
              <ArrowLeft size={14} /> Back to library
            </button>
          </div>
        </div>
      )}

      {menu && (
        <div class="player-overlay" onPointerDown={arm} onClick={guard(() => setMenu(false))}>
          <div class="overlay-card menu" onClick={(e) => e.stopPropagation()}>
            <span class="eyebrow">Paused</span>
            <h2>{game?.title}</h2>
            <SaveBadge status={status} inline />
            <button class="btn primary big" onClick={guard(() => setMenu(false))}>
              <Play size={18} /> Resume
            </button>
            <div class="slots">
              {quickSlots.map((n) => {
                const v = states.find((x) => x.slot === n);
                return (
                  <div class="slot" key={n}>
                    <div class="slot-shot">
                      {v?.image ? (
                        <img src={stateImage(v)} alt="" />
                      ) : (
                        <span>{v ? "" : "Empty"}</span>
                      )}
                      <span class="slot-n">{n}</span>
                    </div>
                    <span class="hint">{v ? ago(v.created) : "—"}</span>
                    <div class="slot-actions">
                      <button class="btn small" onClick={guard(() => saveSlot(n))}>
                        Save
                      </button>
                      <button class="btn small" disabled={!v} onClick={guard(() => loadSlot(n))}>
                        Load
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
            {slotNote && <p class="hint">{slotNote}</p>}
            <div class="menu-row">
              <button class={"btn" + (fast ? " active" : "")} onClick={guard(toggleFast)}>
                <Gauge size={16} /> {fast ? `Fast forward ${speed}× on` : `Fast forward ${speed}×`}
              </button>
              <button class="btn" onClick={guard(toggleSound)}>
                {muted ? <VolumeX size={16} /> : <Volume2 size={16} />} {muted ? "Muted" : "Sound"}
              </button>
            </div>
            <p class="hint">
              Parlor saves to the server whenever the game saves, and keeps your place when you
              leave. Save states are snapshots; the game's own save is the one that counts.
            </p>
            <button class="btn" onClick={guard(quit)}>
              <SaveIcon size={16} /> Quit to library
            </button>
          </div>
        </div>
      )}

      {clash && (
        <div class="player-overlay" onPointerDown={arm}>
          <div class="overlay-card" role="alertdialog">
            <span class="eyebrow accent">Save conflict</span>
            <h2>Another device saved this game</h2>
            <p class="lede">
              {clash.theirs.device || "Another device"} saved {ago(clash.theirs.created)}
              {clash.beforeStart
                ? `, after this ${deviceName()} saved progress that hasn't been uploaded yet.`
                : `, while this game was open here.`}{" "}
              Pick the save to keep. The other one stays in the save history.
            </p>
            <button class="btn primary big" onClick={guard(takeTheirs)}>
              Use the save from {clash.theirs.device || "the other device"}
            </button>
            <button class="btn" onClick={guard(keepMine)}>
              Keep this {deviceName()}'s progress
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function SaveBadge({ status, inline }: { status: Status; inline?: boolean }) {
  if (status.kind === "idle") return null;
  const text =
    status.kind === "saving"
      ? "Saving…"
      : status.kind === "offline"
        ? "Offline · will retry"
        : `Saved ${ago(status.at)}`;
  return (
    <div class={`save-badge ${status.kind}${inline ? " inline" : ""}`} data-testid="save-status">
      <span
        class={`dot${status.kind === "offline" ? " warn" : status.kind === "saving" ? " off" : ""}`}
      />
      {text}
    </div>
  );
}

// desktopLayout fits the screen in the window, at a whole multiple of the
// GBA's 240×160 when there's room, with a bar underneath.
function desktopLayout(w: number, h: number): Layout {
  const room = h - 70;
  let scale = Math.min(w / 240, room / 160);
  if (scale >= 2) scale = Math.floor(scale);
  const sw = 240 * scale;
  const sh = 160 * scale;
  return { shapes: [], screen: { x: (w - sw) / 2, y: Math.max(0, (room - sh) / 2), w: sw, h: sh } };
}
