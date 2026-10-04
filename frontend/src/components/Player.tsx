import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import {
  ArrowLeft,
  FastForward,
  LayoutGrid,
  Gauge,
  Play,
  Save as SaveIcon,
  Volume2,
  VolumeX,
} from "lucide-preact";
import { api, bytes, putBytes } from "../api";
import {
  centre,
  customize,
  layout as placeControls,
  loadCustoms,
  MAX_SCALE,
  MIN_SCALE,
  padLayout,
  saveCustoms,
  type Customs,
  type Key,
  type Layout,
} from "../controls";
import { loadBindings, pads, read, type Control } from "../gamepad";
import { Controls } from "./Controls";
import { keyboard, keyName, type Core } from "../core";
import { ejs } from "../ejs";
import { mgba } from "../emulator";
import { ago, deviceName } from "../lib";
import { pending, pendingStates, type Pending } from "../pending";
import { loadROM } from "../roms";
import { digest, send, sendState } from "../sync";
import {
  isRemote,
  maxStreamSpeed,
  savedScale,
  stream,
  streamScales,
  type RemoteCore,
  type StreamEvent,
} from "../stream";
import { system, type System } from "../systems";
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

// The core for a console: mGBA for the GBA, the server for the 3DS,
// EmulatorJS for the rest.
const coreFor = (sys: System, id: number) =>
  sys.stream ? stream(sys, id) : sys.ejs ? ejs(sys) : mgba(id);

// The desktop keyboard's keys, for the hint under the game.
function keyHint(sys: System): string {
  const seen = new Set<string>();
  const parts: string[] = [];
  for (const [code, key] of keyboard(sys.keys)) {
    const name = keyName(code);
    if (name === "Arrows") {
      if (!seen.has(name)) parts.unshift("Arrows");
      seen.add(name);
      continue;
    }
    parts.push(`${name} ${key}`);
  }
  return [...parts, "F fast forward", "Esc menu"].join(" · ");
}

export function Player({
  id,
  platform,
  onExit,
}: {
  id: number;
  platform: string;
  onExit: () => void;
}) {
  const sys = system(platform);
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
  // A controller is connected: the touch controls step aside.
  const [padOn, setPadOn] = useState(() => pads().length > 0);
  const [toast, setToast] = useState("");
  // The layout editor: this device's own control layouts.
  const [editing, setEditing] = useState(false);
  const [customs, setCustoms] = useState<Customs>(() => loadCustoms(sys.id));
  const [selected, setSelected] = useState<string | null>(null);
  const size = useRef({ w: 0, h: 0 });
  // Streamed games: the resolution, whether the server is still starting
  // the game, and whether it's already playing this one (another tab or
  // device), so it can be carried on from there.
  const [scale, setScale] = useState(0);
  const [starting, setStarting] = useState(false);
  const [running, setRunning] = useState(false);

  const areaRef = useRef<HTMLDivElement>(null);
  const holder = useRef<HTMLDivElement>(null);
  const core = useRef<Core | null>(null);
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
      size.current = { w: width, h: height };
      if (!touch) setArea(desktopLayout(width, height, sys));
      else if (padOn) setArea(padLayout(width, height, sys));
      else {
        const custom = customs[width > height ? "landscape" : "portrait"];
        setArea(customize(placeControls(width, height, sys), custom, width, height));
      }
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [padOn, customs]);

  useEffect(() => {
    const update = () => setPadOn(pads().length > 0);
    window.addEventListener("gamepadconnected", update);
    window.addEventListener("gamepaddisconnected", update);
    return () => {
      window.removeEventListener("gamepadconnected", update);
      window.removeEventListener("gamepaddisconnected", update);
    };
  }, []);

  // The core's screen goes into the box once both exist.
  const [booted, setBooted] = useState(false);
  useEffect(() => {
    if (core.current) holder.current?.appendChild(core.current.screen);
  }, [area !== null, booted]);

  // A message over the game for a moment (quick save and load).
  useEffect(() => {
    if (!toast) return;
    const t = window.setTimeout(() => setToast(""), 2200);
    return () => window.clearTimeout(t);
  }, [toast]);

  // Load everything; the game starts on a tap.
  useEffect(() => {
    let live = true;
    const boot = coreFor(sys, id);
    api<Settings>("settings")
      .then((s) => live && setSpeed(s.fast_forward))
      .catch(() => {});
    (async () => {
      const g = await api<GameDetail>(`games/${id}`);
      if (!live) return;
      setGame(g);
      if (g.missing) throw new Error("This game's ROM is missing from the library folder.");
      if (sys.stream) {
        const settings = await api<Settings>("settings");
        if (!settings.stream) {
          throw new Error(
            "3DS games play on the server, through parlor-stream, which isn't set up. See Parlor's README.",
          );
        }
        const [st, playing] = await Promise.all([
          api<State[]>(`games/${id}/states`),
          api<{ game_id: number }>("stream").catch(() => ({ game_id: 0 })),
        ]);
        if (!live) return;
        setStates(st);
        setRunning(playing.game_id === id);
        core.current = await boot;
        setBooted(true);
        setPhase("ready");
        return;
      }
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
      setBooted(true);
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
    core.current?.pause();
  }
  function resume() {
    if (menu || clash || editing) return;
    paused.current = false;
    core.current?.resume();
    core.current?.resumeAudio();
  }

  // listen watches for in-game saves. Loading a game resets mGBA's
  // callbacks, so this follows every load.
  function listen(m: Core) {
    m.onSave(() => {
      dirty.current = true;
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(queue, 800);
    });
  }

  // rebase takes the save as it is now as the one the server has. Loading
  // a state puts its own copy of the save in RetroArch's cores; that's
  // not the game saving, so it isn't uploaded until the game saves again.
  async function rebase() {
    const data = core.current?.getSave();
    if (data?.length) lastHash.current = await digest(data);
  }

  // begin starts the game, from where it was left when carryOn is set.
  function begin(carryOn: boolean) {
    const m = core.current;
    if (isRemote(m)) return beginRemote(m, carryOn);
    if (!m || !rom.current || clash || !game) return;
    try {
      m.start(rom.current, initial.current, { saveType: game.save_type, rtc: game.rtc });
      listen(m);
      const state = carryOn ? resumeFrom.current : null;
      if (state) {
        m.afterStart(async () => {
          if (!(await m.restoreState(0, state))) {
            setSlotNote("Couldn't load where you left off; started from the in-game save.");
          }
          await rebase();
          if (!paused.current) m.resume();
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

  // beginRemote starts a streamed game. The server loads the save, and
  // the state when carrying on, itself.
  function beginRemote(m: RemoteCore, carryOn: boolean) {
    m.onEvent(streamEvent);
    setStarting(true);
    setPhase("playing");
    m.begin(carryOn, savedScale()).catch((e: Error) => {
      setError(`Couldn't start the game on the server: ${e.message}`);
      setPhase("error");
    });
  }

  function streamEvent(e: StreamEvent) {
    switch (e.type) {
      case "started":
        setStarting(false);
        setScale(e.scale);
        break;
      case "saving":
        setStatus({ kind: "saving" });
        break;
      case "saved":
        setStatus({ kind: "saved", at: e.at });
        break;
      case "save_failed":
        setStatus({ kind: "offline" });
        break;
      case "carry_on_failed":
        setToast("Couldn't load where you left off; started from the in-game save.");
        break;
      case "error":
        setError(e.error);
        setPhase("error");
        break;
      case "lost":
        setError(
          "Lost the connection to the server. The game waits there a few minutes: open it again to carry on.",
        );
        setPhase("error");
        break;
    }
  }

  function changeScale(n: number) {
    const m = core.current;
    if (!isRemote(m)) return;
    m.setScale(n);
    setScale(n);
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
        core.current?.pause();
        // iOS may close the app from here: keep the place.
        keepPlace();
      } else {
        if (!paused.current) core.current?.resume();
        core.current?.resumeAudio();
        wake();
        retry();
      }
    };
    const hide = () => queue();
    const online = () => retry();
    // iOS suspends audio after calls and other interruptions; any tap
    // brings it back.
    const tap = () => core.current?.resumeAudio();
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
    if (menu || clash || editing) pause();
    else if (phase === "playing") resume();
  }, [menu, clash, editing]);

  // Controllers are read every frame while the game is up. The newest
  // handlers are kept in a ref, since the loop outlives renders.
  const latest = useRef({ menu, clash, editing, toggleFast, saveSlot, loadSlot });
  latest.current = { menu, clash, editing, toggleFast, saveSlot, loadSlot };
  useEffect(() => {
    if (phase !== "playing") return;
    const bindings = loadBindings();
    let held = new Set<Control>();
    let frame = 0;
    const tick = () => {
      frame = requestAnimationFrame(tick);
      const now = new Set<Control>();
      for (const p of pads()) for (const c of read(p, bindings, sys.keys)) now.add(c);
      if (now.size) setPadOn(true);
      const ui = latest.current;
      for (const c of held) if (!now.has(c) && isKey(c)) key(c, false);
      for (const c of now) {
        if (held.has(c)) continue;
        if (isKey(c)) {
          if (!ui.menu && !ui.clash && !ui.editing) key(c, true);
        } else if (c === "Menu") {
          if (!ui.clash && !ui.editing) setMenu((m) => !m);
        } else if (!ui.menu && !ui.clash) {
          if (c === "Fast") ui.toggleFast();
          else if (c === "SaveState") ui.saveSlot(1, true);
          else if (c === "LoadState") ui.loadSlot(1, true);
        }
      }
      held = now;
    };
    frame = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(frame);
      for (const c of held) if (isKey(c)) key(c, false);
    };
  }, [phase]);

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
    // The server keeps the place and the save of a streamed game.
    if (isRemote(m)) {
      if (phase === "playing") {
        setStatus({ kind: "saving" });
        await m.quit();
      }
      m.stop();
      onExit();
      return;
    }
    if (m && phase === "playing") {
      await queue();
      // Choose which save to keep before leaving.
      if (conflicted.current) return;
      setStatus({ kind: "saving" });
      await keepPlace();
      m.onSave(null);
      m.stop();
    }
    // EmulatorJS can't start another game, and its page (/play) has a
    // looser content policy: leave it for the app's own page, which frees
    // the game's memory too.
    if (sys.ejs) {
      location.replace(`/#/game/${id}`);
      return;
    }
    onExit();
  }

  // keepPlace takes a state when leaving the game, so it carries on from
  // there next time, on any device. It waits on the device until sent.
  async function keepPlace() {
    const m = core.current;
    if (!m || conflicted.current || isRemote(m)) return;
    const data = await m.captureState(0);
    if (!data) return;
    const p = { gameId: id, data: data.slice(), at: new Date().toISOString() };
    await pendingStates.put(p).catch(() => {});
    await sendState(p);
  }

  // saveSlot and loadSlot report in the menu, or over the game when a
  // controller button did it (quick).
  async function saveSlot(slot: number, quick = false) {
    const m = core.current;
    if (!m) return;
    const note = quick ? setToast : setSlotNote;
    if (isRemote(m)) {
      note("Saving…");
      try {
        const v = await m.saveState(slot);
        setStates((list) => [...list.filter((x) => x.slot !== slot), v]);
        note(`Saved to slot ${slot}`);
      } catch (e) {
        note(`Couldn't save to slot ${slot}: ${(e as Error).message}`);
      }
      return;
    }
    const data = await m.captureState(slot);
    if (!data) {
      note("The emulator couldn't take a save state.");
      return;
    }
    note("Saving…");
    try {
      const q = new URLSearchParams({ device: deviceName() });
      const v = await putBytes<State>(`games/${id}/states/${slot}?${q}`, data.slice());
      setStates((list) => [...list.filter((x) => x.slot !== slot), v]);
      note(`Saved to slot ${slot}`);
    } catch (e) {
      note(`Couldn't save to slot ${slot}: ${(e as Error).message}`);
    }
  }

  async function loadSlot(slot: number, quick = false) {
    const m = core.current;
    if (!m) return;
    const note = quick ? setToast : setSlotNote;
    note("Loading…");
    if (isRemote(m)) {
      try {
        await m.loadState(slot);
        note(`Loaded slot ${slot}`);
        if (quick && !paused.current) m.resume();
        setMenu(false);
      } catch (e) {
        note(`Couldn't load slot ${slot}: ${(e as Error).message}`);
      }
      return;
    }
    try {
      const got = await bytes(`games/${id}/states/${slot}`);
      if (!got || !(await m.restoreState(slot, got.data))) {
        throw new Error("the emulator couldn't load it");
      }
      await rebase();
      note(`Loaded slot ${slot}`);
      if (quick && !paused.current) m.resume();
      setMenu(false);
    } catch (e) {
      note(`Couldn't load slot ${slot}: ${(e as Error).message}`);
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
      core.current.replaceSave(latest.data);
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
    m.press(k, down);
  }

  // touchScreen hands a finger on the DS's bottom screen to the core.
  function touchScreen(type: "mousedown" | "mousemove" | "mouseup", x: number, y: number) {
    core.current?.touch?.(type, x, y);
  }
  function toggleFast() {
    const next = !fast;
    core.current?.setSpeed(next ? ffSpeed : 1);
    setFast(next);
  }
  function toggleSound() {
    const next = !muted;
    core.current?.setVolume(next ? 0 : 1);
    setMuted(next);
  }

  // The editor changes the layout for the orientation in use.
  const orientation = size.current.w > size.current.h ? "landscape" : "portrait";
  const custom = customs[orientation] ?? { shapes: {} };
  function changeLayout(next: Customs[typeof orientation]) {
    const all = { ...customs, [orientation]: next };
    if (!next) delete all[orientation];
    saveCustoms(all, sys.id);
    setCustoms(all);
  }
  function moveControl(id: string, x: number, y: number) {
    const { w, h } = size.current;
    const scale = custom.shapes[id]?.scale ?? 1;
    changeLayout({ ...custom, shapes: { ...custom.shapes, [id]: { x: x / w, y: y / h, scale } } });
  }
  function resizeControl(id: string, scale: number) {
    const s = area?.shapes.find((v) => v.id === id);
    if (!s) return;
    const { w, h } = size.current;
    const c = centre(s);
    const o = custom.shapes[id] ?? { x: c.x / w, y: c.y / h };
    changeLayout({ ...custom, shapes: { ...custom.shapes, [id]: { ...o, scale } } });
  }

  // The editor's bar keeps out of the way of the control being moved.
  const picked = selected ? area?.shapes.find((v) => v.id === selected) : undefined;
  const barAtBottom = !!picked && centre(picked).y < size.current.h / 2;

  // The server fast forwards at up to 4×.
  const ffSpeed = sys.stream ? Math.min(speed, maxStreamSpeed) : speed;
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
            fastLabel={`▶▶ ${ffSpeed}×`}
            editing={editing ? { selected, onSelect: setSelected, onMove: moveControl } : undefined}
            onTouchScreen={sys.touch && phase === "playing" ? touchScreen : undefined}
          />
        )}
        {editing && (
          <div class={`editor-bar ${barAtBottom ? "bottom" : "top"}`}>
            <div class="editor-head">
              <span class="eyebrow">
                <span class="accent">Layout</span>
                <span class="slash">/</span>
                {orientation} · this device
              </span>
              <span class="hint">
                {selected
                  ? `Drag to move · ${selected === "dpad" ? "D-pad" : selected}`
                  : "Drag a control to move it"}
              </span>
            </div>
            <label class="editor-range">
              <span class="eyebrow">Size</span>
              <input
                type="range"
                min={MIN_SCALE}
                max={MAX_SCALE}
                step={0.05}
                disabled={!selected}
                value={selected ? (custom.shapes[selected]?.scale ?? 1) : 1}
                onInput={(e) => selected && resizeControl(selected, Number(e.currentTarget.value))}
              />
            </label>
            <label class="editor-range">
              <span class="eyebrow">Opacity</span>
              <input
                type="range"
                min={0.15}
                max={1}
                step={0.05}
                value={custom.opacity ?? (orientation === "landscape" ? 0.55 : 1)}
                onInput={(e) => changeLayout({ ...custom, opacity: Number(e.currentTarget.value) })}
              />
            </label>
            <div class="editor-actions">
              <button
                class="btn"
                onClick={() => {
                  changeLayout(undefined);
                  setSelected(null);
                }}
              >
                Reset
              </button>
              <button
                class="btn primary"
                onClick={() => {
                  setEditing(false);
                  setSelected(null);
                }}
              >
                Done
              </button>
            </div>
          </div>
        )}
        {!touch && phase === "playing" && (
          <div class="desk-bar">
            <span class="eyebrow">{game?.title}</span>
            <span class="hint mono">{keyHint(sys)}</span>
            <button class={"btn" + (fast ? " active" : "")} onClick={toggleFast}>
              <FastForward size={15} /> {ffSpeed}×
            </button>
            <button class="btn" onClick={() => setMenu(true)}>
              Menu
            </button>
          </div>
        )}
        {phase === "playing" && <SaveBadge status={status} />}
        {toast && <div class="toast">{toast}</div>}
        {phase === "playing" && starting && !toast && (
          <div class="toast">Starting the game on the server…</div>
        )}
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
                  {sys.stream
                    ? "Getting ready…"
                    : progress < 1
                      ? `Downloading ROM… ${Math.round(progress * 100)}%`
                      : sys.ejs
                        ? `Starting the ${sys.short} emulator…`
                        : "Starting mGBA…"}
                </p>
              </>
            )}
            {phase === "ready" && !clash && (left || running) && (
              <>
                {left?.image && !running && (
                  <img class="state-shot" src={stateImage(left)} alt="Where you left off" />
                )}
                <button class="btn primary big" onClick={() => begin(true)} autoFocus>
                  <Play size={18} /> Continue
                </button>
                {running ? (
                  <p class="hint">
                    This game is playing on the server now. Continue picks it up here; it's saved
                    where it is first.
                  </p>
                ) : (
                  left && (
                    <p class="hint">
                      You left off here {ago(left.created)}
                      {left.device ? ` on ${left.device}` : ""}.
                      {game?.save && game.save.created > left.created
                        ? ` The in-game save from ${ago(game.save.created)} is newer.`
                        : ""}
                    </p>
                  )
                )}
                <button class="btn" onClick={() => begin(false)}>
                  {game?.save ? "Start from the in-game save" : "Start a new game"}
                </button>
              </>
            )}
            {phase === "ready" && !clash && !left && !running && (
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
            {sys.stream && (
              <div class="menu-row scales">
                <span class="eyebrow">Resolution</span>
                {streamScales.map((n) => (
                  <button
                    key={n}
                    class={"btn small" + (scale === n ? " active" : "")}
                    onClick={guard(() => changeScale(n))}
                  >
                    {n}×
                  </button>
                ))}
              </div>
            )}
            <div class="menu-row">
              <button class={"btn" + (fast ? " active" : "")} onClick={guard(toggleFast)}>
                <Gauge size={16} />{" "}
                {fast ? `Fast forward ${ffSpeed}× on` : `Fast forward ${ffSpeed}×`}
              </button>
              <button class="btn" onClick={guard(toggleSound)}>
                {muted ? <VolumeX size={16} /> : <Volume2 size={16} />} {muted ? "Muted" : "Sound"}
              </button>
              {touch && !padOn && (
                <button
                  class="btn"
                  onClick={guard(() => {
                    setMenu(false);
                    setEditing(true);
                  })}
                >
                  <LayoutGrid size={16} /> Edit layout
                </button>
              )}
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

function isKey(c: Control): c is Key {
  return c !== "Fast" && c !== "Menu" && c !== "SaveState" && c !== "LoadState";
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
// console's when there's room, with a bar underneath.
function desktopLayout(w: number, h: number, sys: System): Layout {
  const room = h - 70;
  const { w: nw, h: nh } = sys.screen;
  let scale = Math.min(w / nw, room / nh);
  if (scale >= 2) scale = Math.floor(scale);
  const sw = nw * scale;
  const sh = nh * scale;
  return { shapes: [], screen: { x: (w - sw) / 2, y: Math.max(0, (room - sh) / 2), w: sw, h: sh } };
}
