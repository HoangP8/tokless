import * as fs from "node:fs";
import * as path from "node:path";
import { agentPaths, ensureDir, readFileSafe, writeFile } from "../util/paths.js";
import { tryParseJsonc, stringifyJson } from "../util/jsonc.js";
import { pickMcpSpawn } from "../util/mcp-spawn.js";
import { which } from "../util/exec.js";
import type { AgentManifest } from "../core/agent-manifest.js";

interface ClaudeMcpStdio {
  type?: "stdio";
  command: string;
  args?: string[];
  env?: Record<string, string>;
}

interface ClaudeJson {
  mcpServers?: Record<string, ClaudeMcpStdio>;
  [k: string]: unknown;
}

// Writes (or updates) an MCP server entry under ~/.claude.json.
export function configureClaudeMcp(toolId: "codegraph" | "context-mode"): { changed: boolean; file: string } {
  const p = agentPaths.claudeCode();
  ensureDir(p.dir);
  const raw = readFileSafe(p.globalJson);
  const cfg: ClaudeJson = raw ? tryParseJsonc<ClaudeJson>(raw) ?? {} : {};
  cfg.mcpServers ??= {};
  const spawn =
    toolId === "codegraph"
      ? pickMcpSpawn("codegraph", ["serve", "--mcp"])
      : pickMcpSpawn("context-mode");
  const desired: ClaudeMcpStdio = { type: "stdio", command: spawn.command, args: spawn.args };
  const existing = cfg.mcpServers[toolId];
  if (existing && deepEqualMcp(existing, desired)) return { changed: false, file: p.globalJson };
  cfg.mcpServers[toolId] = desired;
  writeFile(p.globalJson, stringifyJson(cfg));
  return { changed: true, file: p.globalJson };
}

export function removeClaudeMcp(toolId: string): boolean {
  const p = agentPaths.claudeCode();
  const raw = readFileSafe(p.globalJson);
  if (!raw) return false;
  const cfg = tryParseJsonc<ClaudeJson>(raw);
  if (!cfg?.mcpServers?.[toolId]) return false;
  delete cfg.mcpServers[toolId];
  writeFile(p.globalJson, stringifyJson(cfg));
  return true;
}

function deepEqualMcp(a: ClaudeMcpStdio, b: ClaudeMcpStdio): boolean {
  return (
    a.command === b.command &&
    JSON.stringify(a.args ?? []) === JSON.stringify(b.args ?? []) &&
    JSON.stringify(a.env ?? {}) === JSON.stringify(b.env ?? {})
  );
}

export function ensureClaudeSkillDir(): string {
  const p = agentPaths.claudeCode();
  ensureDir(p.skillsDir);
  return p.skillsDir;
}

export function locateClaudeCaveman(): string {
  return path.join(ensureClaudeSkillDir(), "caveman");
}

const claude: AgentManifest = {
  id: "claude",
  label: "Claude Code",
  homepage: "https://claude.com/claude-code",
  cliBin: "claude",
  configDir: () => agentPaths.claudeCode().dir,
  detect: () => {
    if (which("claude")) return { installed: true, source: "cli" };
    if (fs.existsSync(agentPaths.claudeCode().dir)) return { installed: true, source: "config" };
    return { installed: false, source: null };
  },
};

export default claude;
