import type { AgentId } from "./ids.js";

// One self-contained description of an AI coding agent.

export interface AgentManifest {
  id: AgentId;
  label: string;
  homepage: string;
  cliBin: string;
  configDir(): string;
  detect(): { installed: boolean; source: "cli" | "config" | null };
}
