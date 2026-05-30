import * as fs from "node:fs";
import { agentPaths, ensureDir, readFileSafe, writeFile } from "../util/paths.js";
import { tryParseJsonc, stringifyJson } from "../util/jsonc.js";
import { pickMcpSpawn } from "../util/mcp-spawn.js";
import { which } from "../util/exec.js";
import type { AgentManifest } from "../core/agent-manifest.js";

interface OpenCodeLocalMcp {
  type: "local";
  command: string[];
  environment?: Record<string, string>;
  enabled?: boolean;
}

interface OpenCodeConfig {
  $schema?: string;
  mcp?: Record<string, OpenCodeLocalMcp>;
  plugin?: string[];
  instructions?: string[];
  [k: string]: unknown;
}

export function configureOpenCodeMcp(toolId: "codegraph" | "context-mode"): { changed: boolean; file: string } {
  const p = agentPaths.opencode();
  ensureDir(p.dir);
  const raw = readFileSafe(p.config);
  const cfg: OpenCodeConfig = raw ? tryParseJsonc<OpenCodeConfig>(raw) ?? {} : {};
  cfg.$schema ??= "https://opencode.ai/config.json";
  cfg.mcp ??= {};
  const spawn =
    toolId === "codegraph"
      ? pickMcpSpawn("codegraph", ["serve", "--mcp"])
      : pickMcpSpawn("context-mode");
  const desired: OpenCodeLocalMcp = { type: "local", command: [spawn.command, ...spawn.args], enabled: true };
  const existing = cfg.mcp[toolId];
  if (existing && arrEq(existing.command, desired.command) && existing.enabled !== false) {
    return { changed: false, file: p.config };
  }
  cfg.mcp[toolId] = desired;
  writeFile(p.config, stringifyJson(cfg));
  return { changed: true, file: p.config };
}

export function removeOpenCodeMcp(toolId: string): boolean {
  const p = agentPaths.opencode();
  const raw = readFileSafe(p.config);
  if (!raw) return false;
  const cfg = tryParseJsonc<OpenCodeConfig>(raw);
  if (!cfg?.mcp?.[toolId]) return false;
  delete cfg.mcp[toolId];
  writeFile(p.config, stringifyJson(cfg));
  return true;
}

function arrEq(a: string[] = [], b: string[] = []): boolean {
  return a.length === b.length && a.every((x, i) => x === b[i]);
}

const opencode: AgentManifest = {
  id: "opencode",
  label: "OpenCode",
  homepage: "https://opencode.ai",
  cliBin: "opencode",
  configDir: () => agentPaths.opencode().dir,
  detect: () => {
    if (which("opencode")) return { installed: true, source: "cli" };
    if (fs.existsSync(agentPaths.opencode().dir)) return { installed: true, source: "config" };
    return { installed: false, source: null };
  },
};

export default opencode;
