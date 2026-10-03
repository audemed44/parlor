import { coverText } from "../lib";

// Cover is a game's tile: its name set large. ROM hacks aren't in any
// box-art database, so every game gets the same treatment.
export function Cover({ title, big }: { title: string; big?: boolean }) {
  const text = coverText(title);
  return (
    <div class={"cover" + (big ? " big" : "")} aria-hidden="true">
      <span class="cover-mark" />
      <span class={"cover-text" + (text.length > 18 ? " long" : "")}>{text}</span>
    </div>
  );
}
