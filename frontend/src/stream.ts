// The 3DS plays on the server (parlor-stream) and streams here over
// WebRTC: the picture and sound come in as a video, buttons and touches go
// back on the "input" data channel, and everything else (pause, fast
// forward, states, quitting) on "control". Saves never pass through the
// browser: the server's player keeps them in Parlor itself, and keeps the
// player's place when the game ends.
import { api } from "./api";
import type { Key } from "./controls";
import { keyboard, type Core } from "./core";
import { deviceName } from "./lib";
import type { System } from "./systems";
import type { State } from "./types";

// libretro's joypad buttons, as bit numbers.
const ids: Record<Key, number> = {
  B: 0,
  Y: 1,
  Select: 2,
  Start: 3,
  Up: 4,
  Down: 5,
  Left: 6,
  Right: 7,
  A: 8,
  X: 9,
  L: 10,
  R: 11,
};

// The server plays at up to 4× when fast forwarding.
export const maxStreamSpeed = 4;
export const streamScales = [1, 2, 3, 4];

// Messages from the server's player.
export type StreamEvent =
  | { type: "started"; scale: number }
  | { type: "saving" }
  | { type: "saved"; at: string }
  | { type: "save_failed"; error: string }
  | { type: "state_saved"; slot: number; state: State }
  | { type: "state_loaded"; slot: number }
  | { type: "state_failed"; slot: number; error: string }
  | { type: "carry_on_failed"; error: string }
  | { type: "error"; error: string }
  | { type: "ended" }
  // The connection dropped. The server keeps the game a few minutes.
  | { type: "lost" };

export interface RemoteCore extends Core {
  readonly remote: true;
  // begin connects and starts the game, from where it was left when
  // carryOn is set. Call it within the tap that starts the game: iOS only
  // plays sound started by one.
  begin(carryOn: boolean, scale: number): Promise<void>;
  onEvent(f: (e: StreamEvent) => void): void;
  saveState(slot: number): Promise<State>;
  loadState(slot: number): Promise<void>;
  // setScale changes the internal resolution, as a multiple of the 3DS's.
  setScale(scale: number): void;
  // quit ends the game; the server keeps the player's place and save.
  quit(): Promise<void>;
}

export function isRemote(c: Core | null): c is RemoteCore {
  return !!c && "remote" in c;
}

// The resolution this device last chose.
export function savedScale(): number {
  const n = Number(localStorage.getItem("parlor.stream.scale"));
  return streamScales.includes(n) ? n : 0;
}

export function stream(sys: System, id: number): RemoteCore {
  const video = document.createElement("video");
  video.className = "screen stream-screen";
  video.playsInline = true;
  video.autoplay = true;
  video.disablePictureInPicture = true;
  const media = new MediaStream();
  video.srcObject = media;

  let pc: RTCPeerConnection | null = null;
  let input: RTCDataChannel | null = null;
  let control: RTCDataChannel | null = null;
  const outbox: string[] = [];
  const listeners: ((e: StreamEvent) => void)[] = [];
  const states = new Map<number, { ok: (v: State | void) => void; fail: (e: Error) => void }>();
  let ended: (() => void) | null = null;
  let buttons = 0;
  let touching = false;
  let tx = 0;
  let ty = 0;
  let ticker = 0;

  const emit = (e: StreamEvent) => listeners.forEach((f) => f(e));
  const tell = (msg: object) => {
    const s = JSON.stringify(msg);
    if (control?.readyState === "open") control.send(s);
    else outbox.push(s);
  };
  const sendInput = () => {
    if (input?.readyState !== "open") return;
    const b = new DataView(new ArrayBuffer(16));
    b.setUint16(0, buttons, true);
    b.setUint8(10, touching ? 1 : 0);
    b.setUint16(12, tx, true);
    b.setUint16(14, ty, true);
    input.send(b.buffer);
  };

  const onMessage = (m: MessageEvent) => {
    let e: StreamEvent;
    try {
      e = JSON.parse(m.data);
    } catch {
      return;
    }
    if (e.type === "state_saved" || e.type === "state_loaded" || e.type === "state_failed") {
      const p = states.get(e.slot);
      states.delete(e.slot);
      if (e.type === "state_failed") p?.fail(new Error(e.error));
      else p?.ok(e.type === "state_saved" ? e.state : undefined);
    }
    if (e.type === "ended") ended?.();
    emit(e);
  };

  // On a desktop the mouse is the stylus. (Touch screens go through the
  // player's controls, which call touch.)
  const mouse = (e: MouseEvent) => {
    if (e.type === "mousemove" && !touching) return;
    if (e.type === "mouseup" && !touching) return;
    core.touch!(e.type as "mousedown" | "mousemove" | "mouseup", e.clientX, e.clientY);
  };
  video.addEventListener("mousedown", mouse);
  video.addEventListener("mousemove", mouse);

  const map = new Map(keyboard(sys.keys));
  const keys = (e: KeyboardEvent) => {
    const k = map.get(e.code);
    if (!k || e.repeat || e.metaKey || e.ctrlKey || e.altKey) return;
    e.preventDefault();
    core.press(k, e.type === "keydown");
  };

  // stateCall waits for the server to answer about a slot.
  const stateCall = <T>(type: string, slot: number) =>
    new Promise<T>((ok, fail) => {
      states.get(slot)?.fail(new Error("replaced"));
      states.set(slot, { ok: ok as (v: State | void) => void, fail });
      tell({ type, slot });
      setTimeout(() => {
        if (states.get(slot)?.fail === fail) {
          states.delete(slot);
          fail(new Error("the server didn't answer"));
        }
      }, 60000);
    });

  const core: RemoteCore = {
    remote: true,
    screen: video,
    reusable: true,
    start() {
      throw new Error("streamed games start with begin");
    },
    async begin(carryOn, scale) {
      video.play().catch(() => {});
      window.addEventListener("keydown", keys);
      window.addEventListener("keyup", keys);
      window.addEventListener("mouseup", mouse);
      pc = new RTCPeerConnection({ bundlePolicy: "max-bundle" });
      pc.addTransceiver("video", { direction: "recvonly" });
      pc.addTransceiver("audio", { direction: "recvonly" });
      input = pc.createDataChannel("input", { ordered: false, maxRetransmits: 0 });
      control = pc.createDataChannel("control");
      control.onopen = () => outbox.splice(0).forEach((s) => control!.send(s));
      control.onmessage = onMessage;
      pc.ontrack = (e) => {
        // As little buffering as the browser allows: it's a game.
        const r = e.receiver as RTCRtpReceiver & {
          jitterBufferTarget?: number;
          playoutDelayHint?: number;
        };
        try {
          r.jitterBufferTarget = 0;
          r.playoutDelayHint = 0;
        } catch {
          // Not supported here.
        }
        media.addTrack(e.track);
        video.play().catch(() => {});
      };
      pc.onconnectionstatechange = () => {
        if (pc?.connectionState === "failed") emit({ type: "lost" });
      };
      await pc.setLocalDescription(await pc.createOffer());
      await gathered(pc);
      const answer = await api<{ sdp: string }>(`games/${id}/stream`, {
        sdp: pc.localDescription!.sdp,
        carry_on: carryOn,
        device: deviceName(),
        scale,
      });
      await pc.setRemoteDescription({ type: "answer", sdp: answer.sdp });
      ticker = window.setInterval(sendInput, 250);
    },
    onEvent: (f) => listeners.push(f),
    saveState: (slot) => stateCall<State>("save_state", slot),
    loadState: (slot) => stateCall<void>("load_state", slot),
    setScale(scale) {
      localStorage.setItem("parlor.stream.scale", String(scale));
      tell({ type: "scale", value: scale });
    },
    quit() {
      if (!pc || pc.connectionState !== "connected") return Promise.resolve();
      return new Promise<void>((ok) => {
        ended = ok;
        tell({ type: "quit" });
        setTimeout(ok, 30000);
      });
    },
    afterStart: (f) => f(),
    onSave() {},
    getSave: () => null,
    replaceSave() {},
    captureState: async () => null,
    restoreState: async () => false,
    press(key, down) {
      const bit = 1 << ids[key];
      const next = down ? buttons | bit : buttons & ~bit;
      if (next === buttons) return;
      buttons = next;
      sendInput();
    },
    // A finger on the bottom screen: its place across the whole picture.
    touch(type, x, y) {
      const r = video.getBoundingClientRect();
      if (!r.width || !r.height) return;
      const clamp = (v: number) => Math.round(Math.min(Math.max(v, 0), 1) * 65535);
      touching = type !== "mouseup";
      tx = clamp((x - r.left) / r.width);
      ty = clamp((y - r.top) / r.height);
      sendInput();
    },
    pause: () => tell({ type: "pause" }),
    resume: () => tell({ type: "resume" }),
    setSpeed: (n) => tell({ type: "speed", value: Math.min(n, maxStreamSpeed) }),
    setVolume(v) {
      video.muted = v === 0;
    },
    resumeAudio() {
      if (video.paused) video.play().catch(() => {});
    },
    stop() {
      window.clearInterval(ticker);
      window.removeEventListener("keydown", keys);
      window.removeEventListener("keyup", keys);
      window.removeEventListener("mouseup", mouse);
      pc?.close();
      pc = null;
      media.getTracks().forEach((t) => t.stop());
    },
  };
  return core;
}

// gathered waits for the browser's candidates: the offer carries them all.
function gathered(pc: RTCPeerConnection): Promise<void> {
  if (pc.iceGatheringState === "complete") return Promise.resolve();
  return new Promise((ok) => {
    const done = () => {
      if (pc.iceGatheringState !== "complete") return;
      pc.removeEventListener("icegatheringstatechange", done);
      ok();
    };
    pc.addEventListener("icegatheringstatechange", done);
    setTimeout(ok, 2000);
  });
}
