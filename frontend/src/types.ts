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
}

export interface Candidate {
  path: string;
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
