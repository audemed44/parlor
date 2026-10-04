import { ArrowLeft } from "lucide-preact";
import type { Game } from "../types";
import { PatchForm } from "./PatchForm";
import { Section } from "./ui";

// PatchPage makes a new game from a patch and a base ROM in the library.
export function PatchPage({ games, onChange }: { games: Game[]; onChange: () => void }) {
  return (
    <div class="page">
      <a class="text-link back" href="#/">
        <ArrowLeft size={14} /> Library
      </a>
      <div class="page-head">
        <span class="eyebrow">
          <span class="accent">Patch</span>
          <span class="slash">/</span>
          IPS · UPS · BPS
        </span>
        <h1>{"Patch a\nROM hack."}</h1>
        <p class="lede">
          Apply a hack's patch to its clean base ROM. The result is saved in Parlor's data folder,
          since the library folder stays read-only. To update a hack you already play and keep its
          save, use Update on that game's page instead.
        </p>
      </div>
      <section>
        <Section index="01" title="New game" />
        <PatchForm games={games} onDone={onChange} />
      </section>
    </div>
  );
}
