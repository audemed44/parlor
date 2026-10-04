import { useEffect, useState } from "preact/hooks";
import { ArrowLeft } from "lucide-preact";
import { api } from "../api";
import { fastForwardSpeeds, type Settings } from "../types";
import { ControllerSettings } from "./ControllerSettings";
import { ErrorNote, Section } from "./ui";

// SettingsPage holds settings shared by every device, and this device's
// controller.
export function SettingsPage() {
  const [settings, setSettings] = useState<Settings | null>(null),
    [error, setError] = useState(""),
    [saved, setSaved] = useState(false);

  useEffect(() => {
    api<Settings>("settings")
      .then(setSettings)
      .catch((e) => setError(e.message));
  }, []);

  async function choose(speed: number) {
    setError("");
    setSaved(false);
    try {
      setSettings(await api<Settings>("settings", { ...settings, fast_forward: speed }));
      setSaved(true);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  return (
    <div class="page">
      <a class="text-link back" href="#/">
        <ArrowLeft size={14} /> Library
      </a>
      <div class="page-head">
        <span class="eyebrow">
          <span class="accent">Settings</span>
          <span class="slash">/</span>
          Every device and this one
        </span>
        <h1>Settings</h1>
      </div>
      <ErrorNote error={error} />
      <section>
        <Section index="01" title="Fast forward">
          {saved && <span class="muted small">Saved</span>}
        </Section>
        <p class="hint section-note">
          How fast the game runs while fast forward is on. Toggle it with the ▶▶ button next to the
          menu button, or F on a keyboard.
        </p>
        {settings && (
          <div class="segmented" role="radiogroup" aria-label="Fast forward speed">
            {fastForwardSpeeds.map((n) => (
              <button
                key={n}
                role="radio"
                aria-checked={settings.fast_forward === n}
                class={settings.fast_forward === n ? "on" : ""}
                onClick={() => choose(n)}
              >
                {n}×
              </button>
            ))}
          </div>
        )}
      </section>
      <ControllerSettings index="02" />
    </div>
  );
}
