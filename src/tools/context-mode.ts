import * as path from "node:path";
import * as fs from "node:fs";
import * as os from "node:os";
import { run, which } from "../util/exec.js";
import { log } from "../util/logger.js";
import { agentPaths } from "../util/paths.js";
import { stringifyJson } from "../util/jsonc.js";
import { upsertBlock, removeBlock } from "../util/toml.js";
import { c } from "../util/colors.js";
import { pickMcpSpawn } from "../util/mcp-spawn.js";
import { npmGlobalInstall } from "../util/npm-install.js";
import type { ToolManifest, RunOpts } from "../core/tool-manifest.js";

// Source of truth: https://github.com/mksglu/context-mode/blob/main/README.md

interface Deps {
  run: typeof run;
  fs: typeof fs;
}
const defaultDeps: Deps = { run, fs };

async function ensureInstalled(opts: RunOpts, deps: Deps = defaultDeps): Promise<boolean> {
  if (process.env.TOKLESS_TEST === "1") return true;
  if (opts.dryRun) {
    log.sub("[dry-run] would: npm install -g context-mode@latest (cache-skew-resistant)");
    return true;
  }
  void deps;
  const v = await npmGlobalInstall("context-mode", "latest");
  if (!v) {
    log.err("context-mode install failed across all strategies (npm + tarball fallback).");
    return false;
  }
  log.sub(c.dim(`context-mode @${v} installed`));
  return true;
}

// --- Claude Code -----------------------------------------------------------
async function wireClaude(opts: RunOpts, deps: Deps = defaultDeps): Promise<boolean> {
  if (opts.dryRun) {
    log.sub("[dry-run] would: claude mcp add context-mode -- context-mode");
    return true;
  }
  const spawn = pickMcpSpawn("context-mode");
  if (process.env.TOKLESS_TEST === "1") {
    const cp = agentPaths.claudeCode();
    deps.fs.mkdirSync(cp.dir, { recursive: true });
    const cfg = deps.fs.existsSync(cp.globalJson)
      ? safeParseJson(deps.fs.readFileSync(cp.globalJson, "utf8"))
      : ({} as Record<string, unknown>);
    const servers = (cfg.mcpServers && typeof cfg.mcpServers === "object" ? cfg.mcpServers : {}) as Record<string, unknown>;
    servers["context-mode"] = { type: "stdio", command: spawn.command, args: spawn.args };
    cfg.mcpServers = servers;
    deps.fs.writeFileSync(cp.globalJson, stringifyJson(cfg), "utf8");
    return true;
  }
  if (!which("claude")) {
    log.err("claude CLI not on PATH — install Claude Code first (https://claude.com/claude-code).");
    return false;
  }
  const r = await deps.run("claude", ["mcp", "add", "context-mode", "--", spawn.command, ...spawn.args], {
    capture: true,
  });
  if (r.code !== 0) {
    log.err(`claude mcp add failed: ${r.stderr.slice(0, 200)}`);
    return false;
  }
  log.sub(
    c.dim(
      "tip: to enable slash commands, type inside Claude Code: /plugin marketplace add mksglu/context-mode && /plugin install context-mode@context-mode",
    ),
  );
  return true;
}

// --- OpenCode --------------------------------------------------------------
async function wireOpenCode(opts: RunOpts, deps: Deps = defaultDeps): Promise<boolean> {
  if (opts.dryRun) {
    log.sub("[dry-run] would add 'context-mode' to opencode.json plugin[]");
    return true;
  }
  const op = agentPaths.opencode();
  deps.fs.mkdirSync(op.dir, { recursive: true });
  const cfg = deps.fs.existsSync(op.config) ? JSON.parse(deps.fs.readFileSync(op.config, "utf8")) : {};
  cfg.plugin = Array.isArray(cfg.plugin) ? cfg.plugin : [];
  if (!cfg.plugin.some((p: unknown) => typeof p === "string" && pluginIsContextMode(p))) {
    cfg.plugin.push("context-mode");
  }
  if (cfg.mcp && typeof cfg.mcp === "object" && "context-mode" in cfg.mcp) {
    delete cfg.mcp["context-mode"];
    if (Object.keys(cfg.mcp).length === 0) delete cfg.mcp;
  }
  deps.fs.writeFileSync(op.config, stringifyJson(cfg), "utf8");
  if (process.env.TOKLESS_TEST === "1") return true;
  await copyOpenCodeAgentsMd(deps, op.dir);
  await runPostinstallInOpenCodeCache(deps);
  return true;
}

async function runPostinstallInOpenCodeCache(deps: Deps): Promise<void> {
  const bun = which("bun");
  if (!bun) return;
  const cacheRoot = path.join(os.homedir(), ".cache", "opencode", "packages");
  if (!deps.fs.existsSync(cacheRoot)) return;
  // Heal every context-mode@<version> the loader has staged.
  const dirs = deps.fs
    .readdirSync(cacheRoot)
    .filter((d) => d === "context-mode" || d.startsWith("context-mode@"));
  for (const d of dirs) {
    const pkgHost = path.join(cacheRoot, d);
    const installed = path.join(pkgHost, "node_modules", "context-mode");
    if (!deps.fs.existsSync(installed)) continue;
    const r = await deps.run("bun", ["pm", "trust", "context-mode"], { cwd: pkgHost, capture: true });
    if (r.code === 0) log.sub(c.dim(`healed OpenCode plugin cache (${d})`));
  }
}

function pluginIsContextMode(entry: string): boolean {
  return entry === "context-mode" || entry.startsWith("context-mode@");
}

async function copyOpenCodeAgentsMd(deps: Deps, opencodeDir: string): Promise<void> {
  const destAgents = path.join(opencodeDir, "AGENTS.md");
  if (deps.fs.existsSync(destAgents)) return;
  if (!which("npm")) return;
  const rootRes = await deps.run("npm", ["root", "-g"], { capture: true });
  if (rootRes.code !== 0) return;
  const srcAgents = path.join(rootRes.stdout.trim(), "context-mode", "configs", "opencode", "AGENTS.md");
  if (!deps.fs.existsSync(srcAgents)) return;
  deps.fs.copyFileSync(srcAgents, destAgents);
}

// --- Codex CLI -------------------------------------------------------------
async function wireCodex(opts: RunOpts, deps: Deps = defaultDeps): Promise<boolean> {
  if (opts.dryRun) {
    log.sub("[dry-run] would wire context-mode for codex");
    return true;
  }
  if (process.env.TOKLESS_TEST === "1") return wireCodexManual(deps);
  if (!which("codex")) {
    log.err("codex CLI not on PATH — install codex first.");
    return false;
  }
  const probe = await deps.run("codex", ["plugin", "--help"], { capture: true });
  if (probe.code === 0) {
    log.sub("codex supports plugins — using `codex plugin marketplace add`");
    const r = await deps.run("codex", ["plugin", "marketplace", "add", "mksglu/context-mode"], { capture: true });
    if (r.code === 0) {
      enableCodexFeatureFlags(deps, true);
      return true;
    }
    log.debug(`codex plugin install failed (${r.code}); falling back to manual hooks`);
  }
  return wireCodexManual(deps);
}

function wireCodexManual(deps: Deps): boolean {
  const cx = agentPaths.codex();
  deps.fs.mkdirSync(cx.dir, { recursive: true });
  enableCodexFeatureFlags(deps, false);
  const tomlRaw = deps.fs.existsSync(cx.config) ? deps.fs.readFileSync(cx.config, "utf8") : "";
  const tomlOut = upsertBlock(tomlRaw, {
    header: "mcp_servers.context-mode",
    fields: { command: "context-mode" },
  });
  deps.fs.writeFileSync(cx.config, tomlOut, "utf8");

  const hooksPath = path.join(cx.dir, "hooks.json");
  const existing = deps.fs.existsSync(hooksPath) ? safeParseJson(deps.fs.readFileSync(hooksPath, "utf8")) : {};
  deps.fs.writeFileSync(hooksPath, stringifyJson(mergeCodexHooks(existing)), "utf8");
  return true;
}

function enableCodexFeatureFlags(deps: Deps, pluginHooks: boolean): void {
  const cx = agentPaths.codex();
  const raw = deps.fs.existsSync(cx.config) ? deps.fs.readFileSync(cx.config, "utf8") : "";
  const fields: Record<string, boolean> = { hooks: true };
  if (pluginHooks) fields.plugin_hooks = true;
  deps.fs.writeFileSync(cx.config, upsertBlock(raw, { header: "features", fields }), "utf8");
}

// Idempotent merge into the user's hooks.json. Keeps unrelated entries.
function mergeCodexHooks(existing: unknown): Record<string, unknown> {
  const out: Record<string, unknown> = existing && typeof existing === "object" ? { ...(existing as object) } : {};
  const hooks: Record<string, unknown> =
    out.hooks && typeof out.hooks === "object" ? { ...(out.hooks as object) } : {};
  for (const [eventName, ourEntry] of Object.entries(CODEX_HOOK_ENTRIES)) {
    const arr = Array.isArray(hooks[eventName]) ? [...(hooks[eventName] as unknown[])] : [];
    const filtered = arr.filter((entry) => !isOursForEvent(entry, eventName));
    filtered.push(ourEntry);
    hooks[eventName] = filtered;
  }
  out.hooks = hooks;
  return out;
}

function isOursForEvent(entry: unknown, eventName: string): boolean {
  if (!entry || typeof entry !== "object") return false;
  const hooks = (entry as { hooks?: unknown }).hooks;
  if (!Array.isArray(hooks)) return false;
  return hooks.some(
    (h) =>
      h &&
      typeof (h as { command?: unknown }).command === "string" &&
      ((h as { command: string }).command).startsWith(`context-mode hook codex ${eventName.toLowerCase()}`),
  );
}

// Literal copy of README "Manual fallback" hooks.json.
const CODEX_HOOK_ENTRIES = {
  PreToolUse: {
    matcher:
      "local_shell|shell|shell_command|exec_command|Bash|Shell|apply_patch|Edit|Write|grep_files|ctx_execute|ctx_execute_file|ctx_batch_execute|ctx_fetch_and_index|ctx_search|ctx_index|mcp__",
    hooks: [{ type: "command", command: "context-mode hook codex pretooluse" }],
  },
  PostToolUse: { hooks: [{ type: "command", command: "context-mode hook codex posttooluse" }] },
  SessionStart: { hooks: [{ type: "command", command: "context-mode hook codex sessionstart" }] },
  PreCompact: { hooks: [{ type: "command", command: "context-mode hook codex precompact" }] },
  UserPromptSubmit: { hooks: [{ type: "command", command: "context-mode hook codex userpromptsubmit" }] },
  Stop: { hooks: [{ type: "command", command: "context-mode hook codex stop" }] },
} as const;

function safeParseJson(s: string): Record<string, unknown> {
  try {
    return JSON.parse(s);
  } catch {
    return {};
  }
}

async function unwireClaude(opts: RunOpts, deps: Deps = defaultDeps): Promise<boolean> {
  if (opts.dryRun) return true;
  const cp = agentPaths.claudeCode();
  if (!deps.fs.existsSync(cp.globalJson)) return true;
  try {
    const cfg = JSON.parse(deps.fs.readFileSync(cp.globalJson, "utf8"));
    if (cfg?.mcpServers?.["context-mode"]) {
      delete cfg.mcpServers["context-mode"];
      deps.fs.writeFileSync(cp.globalJson, stringifyJson(cfg), "utf8");
    }
  } catch { /* noop */ }
  return true;
}

async function unwireOpenCode(opts: RunOpts, deps: Deps = defaultDeps): Promise<boolean> {
  if (opts.dryRun) return true;
  const op = agentPaths.opencode();
  if (!deps.fs.existsSync(op.config)) return true;
  const cfg = safeParseJson(deps.fs.readFileSync(op.config, "utf8"));
  if (Array.isArray(cfg.plugin)) {
    cfg.plugin = (cfg.plugin as string[]).filter((p) => !(typeof p === "string" && pluginIsContextMode(p)));
    if ((cfg.plugin as string[]).length === 0) delete cfg.plugin;
  }
  deps.fs.writeFileSync(op.config, stringifyJson(cfg), "utf8");
  return true;
}

async function unwireCodex(opts: RunOpts, deps: Deps = defaultDeps): Promise<boolean> {
  if (opts.dryRun) return true;
  const cx = agentPaths.codex();
  if (deps.fs.existsSync(cx.config)) {
    const raw = deps.fs.readFileSync(cx.config, "utf8");
    const next = removeBlock(raw, "mcp_servers.context-mode");
    if (next !== raw) deps.fs.writeFileSync(cx.config, next, "utf8");
  }

  const hooksPath = path.join(cx.dir, "hooks.json");
  if (!deps.fs.existsSync(hooksPath)) return true;
  const data = safeParseJson(deps.fs.readFileSync(hooksPath, "utf8")) as { hooks?: Record<string, unknown> };
  if (!data.hooks) return true;
  for (const eventName of Object.keys(CODEX_HOOK_ENTRIES)) {
    const arr = Array.isArray(data.hooks[eventName]) ? (data.hooks[eventName] as unknown[]) : [];
    const kept = arr.filter((entry) => !isOursForEvent(entry, eventName));
    if (kept.length === 0) delete data.hooks[eventName];
    else data.hooks[eventName] = kept;
  }
  if (Object.keys(data.hooks).length === 0) {
    deps.fs.unlinkSync(hooksPath);
  } else {
    deps.fs.writeFileSync(hooksPath, stringifyJson(data), "utf8");
  }
  return true;
}

function verifyClaude(): boolean {
  const cp = agentPaths.claudeCode();
  if (!fs.existsSync(cp.globalJson)) return false;
  try {
    const cfg = JSON.parse(fs.readFileSync(cp.globalJson, "utf8"));
    return !!cfg?.mcpServers?.["context-mode"];
  } catch { return false; }
}
function verifyOpenCode(): boolean {
  const op = agentPaths.opencode();
  if (!fs.existsSync(op.config)) return false;
  try {
    const cfg = JSON.parse(fs.readFileSync(op.config, "utf8"));
    const hasPlugin = Array.isArray(cfg?.plugin) && cfg.plugin.some((p: unknown) => typeof p === "string" && pluginIsContextMode(p));
    const hasLegacyMcp = !!(cfg?.mcp && typeof cfg.mcp === "object" && "context-mode" in cfg.mcp);
    return hasPlugin && !hasLegacyMcp;
  } catch { return false; }
}
function verifyCodex(): boolean {
  const cx = agentPaths.codex();
  if (!fs.existsSync(cx.config)) return false;
  const raw = fs.readFileSync(cx.config, "utf8");
  if (!raw.includes("[mcp_servers.context-mode]")) return false;
  const hooksPath = path.join(cx.dir, "hooks.json");
  if (!fs.existsSync(hooksPath)) return false;
  try {
    const data = JSON.parse(fs.readFileSync(hooksPath, "utf8")) as { hooks?: Record<string, unknown> };
    return !!data.hooks?.PreToolUse;
  } catch { return false; }
}

const contextMode: ToolManifest = {
  id: "context-mode",
  label: "Context-Mode",
  description: "Routes long context off-thread to a sandbox, keeping the agent's window small.",
  homepage: "https://github.com/mksglu/context-mode",
  installHint: "npm i -g context-mode",
  channel: "npm",

  install: ensureInstalled,
  wireFor: {
    claude: (opts) => wireClaude(opts),
    opencode: (opts) => wireOpenCode(opts),
    codex: (opts) => wireCodex(opts),
  },
  unwireFor: {
    claude: (opts) => unwireClaude(opts),
    opencode: (opts) => unwireOpenCode(opts),
    codex: (opts) => unwireCodex(opts),
  },
  verifyFor: {
    claude: verifyClaude,
    opencode: verifyOpenCode,
    codex: verifyCodex,
  },
};

export default contextMode;
