import type { AgentId, ToolId } from "./ids.js";

export type Channel = "npm" | "github" | "cargo" | "binary";

export interface RunOpts {
  dryRun?: boolean;
  upgrade?: boolean;
}

export type AgentFnMap = Partial<Record<AgentId, (opts: RunOpts) => Promise<boolean>>>;

export interface ToolManifest {
  id: ToolId;
  label: string;
  description: string;
  homepage: string;
  installHint: string;
  channel: Channel;
  install(opts: RunOpts): Promise<boolean>;
  wireFor: AgentFnMap;
  unwireFor?: AgentFnMap;
  verifyFor?: Partial<Record<AgentId, () => Promise<boolean | null> | boolean | null>>;
  localVersion?(): Promise<string | null>;
  latestVersion?(): Promise<string | null>;
}
