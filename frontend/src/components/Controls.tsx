import { useEffect, useRef, useState } from "preact/hooks";
import { pressed, type Action, type Key, type Layout, type Press } from "../controls";

// Controls draws the touch controls and turns touches into button presses.
// Every finger is tracked, so you can hold a direction and press A, and
// slide from one button to the next without lifting.
export function Controls({
  layout,
  onKey,
  onAction,
  toggled,
  fastLabel,
}: {
  layout: Layout;
  onKey: (key: Key, down: boolean) => void;
  // Menu and Fast act once per press, not while held.
  onAction: (action: Action) => void;
  // Actions shown as switched on (fast forward).
  toggled: Set<Action>;
  fastLabel: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const touches = useRef(new Map<number, { x: number; y: number }>());
  const held = useRef(new Set<Press>());
  const [lit, setLit] = useState<Set<Press>>(new Set());
  const shapes = useRef(layout.shapes);
  shapes.current = layout.shapes;
  const handlers = useRef({ onKey, onAction });
  handlers.current = { onKey, onAction };

  function update() {
    const now = pressed(shapes.current, touches.current.values());
    for (const p of held.current) {
      if (!now.has(p) && !isAction(p)) handlers.current.onKey(p, false);
    }
    for (const p of now) {
      if (held.current.has(p)) continue;
      if (isAction(p)) handlers.current.onAction(p);
      else handlers.current.onKey(p, true);
    }
    held.current = now;
    setLit(now);
  }

  useEffect(() => {
    const el = ref.current!;
    const point = (e: PointerEvent) => {
      const box = el.getBoundingClientRect();
      return { x: e.clientX - box.left, y: e.clientY - box.top };
    };
    const down = (e: PointerEvent) => {
      e.preventDefault();
      el.setPointerCapture?.(e.pointerId);
      touches.current.set(e.pointerId, point(e));
      update();
    };
    const move = (e: PointerEvent) => {
      if (!touches.current.has(e.pointerId)) return;
      e.preventDefault();
      touches.current.set(e.pointerId, point(e));
      update();
    };
    const up = (e: PointerEvent) => {
      if (!touches.current.delete(e.pointerId)) return;
      update();
    };
    // Releasing everything when the page loses focus avoids a stuck button.
    const release = () => {
      touches.current.clear();
      update();
    };
    el.addEventListener("pointerdown", down);
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
    el.addEventListener("lostpointercapture", up);
    window.addEventListener("blur", release);
    const block = (e: Event) => e.preventDefault();
    // iOS: no magnifier, callout or double-tap zoom on long presses.
    el.addEventListener("touchstart", block, { passive: false });
    el.addEventListener("contextmenu", block);
    return () => {
      release();
      el.removeEventListener("pointerdown", down);
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", up);
      el.removeEventListener("lostpointercapture", up);
      window.removeEventListener("blur", release);
      el.removeEventListener("touchstart", block);
      el.removeEventListener("contextmenu", block);
    };
  }, []);

  const on = (id: Press) => (lit.has(id) || toggled.has(id as Action) ? " on" : "");
  return (
    <div class={"controls" + (layout.landscape ? " over" : "")} ref={ref} data-testid="controls">
      {layout.shapes.map((s) => {
        if (s.kind === "rect") {
          return (
            <div
              key={s.id}
              data-id={s.id}
              class={`ctl rect ctl-${s.id.toLowerCase()}${on(s.id)}`}
              style={{ left: s.x, top: s.y, width: s.w, height: s.h }}
            >
              {s.id === "Menu" ? "•••" : s.id === "Fast" ? fastLabel : s.id}
            </div>
          );
        }
        if (s.kind === "dpad") {
          const arm = s.r * 0.66;
          return (
            <div
              key="dpad"
              class="ctl dpad"
              style={{ left: s.x - s.r, top: s.y - s.r, width: s.r * 2, height: s.r * 2 }}
            >
              <div class={"arm up" + on("Up")} style={{ width: arm, height: s.r }} />
              <div class={"arm down" + on("Down")} style={{ width: arm, height: s.r }} />
              <div class={"arm left" + on("Left")} style={{ width: s.r, height: arm }} />
              <div class={"arm right" + on("Right")} style={{ width: s.r, height: arm }} />
              <div class="hub" style={{ width: arm, height: arm }} />
            </div>
          );
        }
        return (
          <div
            key={s.id}
            data-id={s.id}
            class={`ctl round${on(s.id as Press)}`}
            style={{ left: s.x - s.r, top: s.y - s.r, width: s.r * 2, height: s.r * 2 }}
          >
            {s.id}
          </div>
        );
      })}
    </div>
  );
}

function isAction(p: Press): p is Action {
  return p === "Menu" || p === "Fast";
}
