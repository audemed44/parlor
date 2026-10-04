import { useEffect, useRef, useState } from "preact/hooks";
import { Gamepad2 } from "lucide-preact";
import {
  bind,
  buttonName,
  controls,
  defaults,
  firstPressed,
  loadBindings,
  pads,
  saveBindings,
  type Bindings,
  type Control,
} from "../gamepad";
import { Section } from "./ui";

// ControllerSettings remaps a Bluetooth controller's buttons, for this
// device. Press Change, then the button on the controller.
export function ControllerSettings({ index }: { index: string }) {
  const [bindings, setBindings] = useState<Bindings>(loadBindings);
  const [listening, setListening] = useState<Control | null>(null);
  const [pad, setPad] = useState("");
  const wait = useRef(listening);
  wait.current = listening;

  useEffect(() => {
    let frame = 0;
    let before = -1;
    const tick = () => {
      frame = requestAnimationFrame(tick);
      const p = pads()[0];
      setPad(p?.id ?? "");
      const now = p ? firstPressed(p) : -1;
      // A new press, not one held since before Change was tapped.
      if (wait.current && now >= 0 && now !== before) {
        const next = bind(loadBindings(), wait.current, now);
        saveBindings(next);
        setBindings(next);
        setListening(null);
      }
      before = now;
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, []);

  function reset() {
    saveBindings(defaults);
    setBindings({ ...defaults });
    setListening(null);
  }

  return (
    <section>
      <Section index={index} title="Controller">
        <span class="muted small">This device</span>
      </Section>
      <p class="hint section-note">
        Xbox, PlayStation and MFi controllers work once paired with this device. While one is
        connected, the touch controls step aside, leaving a menu button. The left stick works as the
        d-pad too.
      </p>
      <div class="notice">
        <Gamepad2 size={16} />
        <div class="spacer">
          {pad ? (
            <>
              <strong>Connected</strong>: {pad}
            </>
          ) : (
            "No controller yet. Pair one, then press any of its buttons."
          )}
        </div>
        <button class="btn small" onClick={reset}>
          Reset
        </button>
      </div>
      <div class="rows bindings">
        {controls.map(([c, label]) => (
          <div class="row" key={c}>
            <div class="row-main">
              <strong>{label}</strong>
            </div>
            <span class="mono hint">
              {listening === c ? "Press a button…" : bindings[c].map(buttonName).join(", ") || "—"}
            </span>
            <button
              class={"btn small" + (listening === c ? " active" : "")}
              onClick={() => setListening(listening === c ? null : c)}
            >
              {listening === c ? "Cancel" : "Change"}
            </button>
          </div>
        ))}
      </div>
    </section>
  );
}
