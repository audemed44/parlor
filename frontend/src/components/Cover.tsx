import { coverText } from "../lib";

// Cover is a game's tile: its custom cover when it has one, or its name set
// large. ROM hacks aren't in any box-art database, so that's the default.
export function Cover({ title, big, src }: { title: string; big?: boolean; src?: string }) {
  if (src) {
    return (
      <div class={"cover image" + (big ? " big" : "")} aria-hidden="true">
        <img src={src} alt="" loading="lazy" />
      </div>
    );
  }
  const text = coverText(title);
  return (
    <div class={"cover" + (big ? " big" : "")} aria-hidden="true">
      <span class="cover-mark" />
      <span class={"cover-text" + (text.length > 18 ? " long" : "")}>{text}</span>
    </div>
  );
}
