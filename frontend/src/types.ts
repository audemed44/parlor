export interface Save {
  id: number;
  game_id: number;
  created: string;
  size: number;
  sha256: string;
  source: "play" | "upload" | "import" | "restore";
  device: string;
  note: string;
}

export interface State {
  id: number;
  game_id: number;
  // 0 is the state taken when you left the game; 1 to 4 are quick slots.
  slot: number;
  created: string;
  size: number;
  sha256: string;
  device: string;
  note: string;
  image: boolean;
}

export const quickSlots = [1, 2, 3, 4];

export interface Game {
  id: number;
  path: string;
  title: string;
  size: number;
  sha1: string;
  missing: boolean;
  added: string;
  last_played: string;
  play_seconds: number;
  notes: string;
  save: Save | null;
}

export interface GameDetail extends Game {
  saves: Save[];
  states: State[];
}

export interface Candidate {
  path: string;
  kind: "save" | "state";
  size: number;
  modified: string;
  sha256: string;
  suggested: number;
  imported: number;
}

export interface Config {
  imports: boolean;
  foyer_url: string;
}

export interface Settings {
  fast_forward: number;
}

export const fastForwardSpeeds = [2, 3, 4, 6, 8];

export interface ScanResult {
  added: number;
  updated: number;
  missing: number;
  total: number;
}
