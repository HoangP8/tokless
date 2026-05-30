import * as fs from "node:fs";
import { agentPaths, ensureDir, readFileSafe, writeFile } from "../util/paths.js";
import { upsertBlock, hasBlock, type TomlBlock } from "../util/toml.js";
import { which } from "../util/exec.js";
import type { AgentManifest } from "../core/agent-manifest.js";

// codegraph wiring for codex.
export function configureCodexMcp(toolId: "codegraph"): { changed: boolean; file: string } {
  const p = agentPaths.codex();
  ensureDir(p.dir);
  const raw = readFileSafe(p.config) ?? "";
  const block: TomlBlock = {
    header: `mcp_servers.${toolId}`,
    fields: { command: "codegraph", args: ["serve", "--mcp"], enabled: true },
  };
  const next = upsertBlock(raw, block);
  if (next === raw) return { changed: false, file: p.config };
  writeFile(p.config, next);
  return { changed: true, file: p.config };
}

export function codexHasMcp(toolId: string): boolean {
  const p = agentPaths.codex();
  const raw = readFileSafe(p.config) ?? "";
  return hasBlock(raw, `mcp_servers.${toolId}`);
}

const codex: AgentManifest = {
  id: "codex",
  label: "Codex",
  homepage: "https://github.com/openai/codex",
  cliBin: "codex",
  configDir: () => agentPaths.codex().dir,
  detect: () => {
    if (which("codex")) return { installed: true, source: "cli" };
    if (fs.existsSync(agentPaths.codex().dir)) return { installed: true, source: "config" };
    return { installed: false, source: null };
  },
};

export default codex;
