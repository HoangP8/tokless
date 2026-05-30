import * as path from "node:path";
import * as fs from "node:fs";
import { run, which } from "../util/exec.js";
import { log } from "../util/logger.js";
import { agentPaths, home } from "../util/paths.js";
import { npmGlobalInstall } from "../util/npm-install.js";
import { stringifyJson, parseJsonc } from "../util/jsonc.js";
import { upsertBlock, removeBlock } from "../util/toml.js";
import type { ToolManifest, RunOpts } from "../core/tool-manifest.js";

async function ensureInstalled(opts: RunOpts): Promise<boolean> {
  if (process.env.TOKLESS_TEST === "1") {
    const dest = path.join(home(), ".local", "bin");
    fs.mkdirSync(dest, { recursive: true });
    const cgPath = path.join(dest, "codegraph");
    if (fs.existsSync(cgPath)) {
      try { fs.unlinkSync(cgPath); } catch { /* noop */ }
    }
    fs.writeFileSync(cgPath, "#!/bin/sh\necho ok", { mode: 0o755 });
    const sep = process.platform === "win32" ? ";" : ":";
    const curPath = process.env.PATH || "";
    if (!curPath.split(sep).includes(dest)) process.env.PATH = dest + sep + curPath;
    return true;
  }
  if (which("codegraph") && !opts.upgrade) return true;
  if (opts.dryRun) {
    log.sub("[dry-run] would install @colbymchenry/codegraph globally");
    return true;
  }
  const v = await npmGlobalInstall("@colbymchenry/codegraph", "latest");
  return v !== null;
}

// `codegraph install` configures every agent CodeGraph supports in one call.
// We invoke it once per tokless run and reuse the result for each agent.
let realInstallDone: Promise<boolean> | null = null;
function callRealInstall(opts: RunOpts): Promise<boolean> {
  if (opts.dryRun) {
    log.sub("[dry-run] would run: codegraph install --yes");
    return Promise.resolve(true);
  }
  if (!realInstallDone) {
    realInstallDone = (async () => {
      const helpRes = await run("codegraph", ["install", "--help"], { capture: true });
      const hasYes = helpRes.stdout.includes("--yes") || helpRes.stderr.includes("--yes");
      const hasTarget = helpRes.stdout.includes("--target") || helpRes.stderr.includes("--target");
      const args = [
        "install",
        ...(hasYes ? ["--yes"] : []),
        ...(hasTarget ? ["--target", "all"] : []),
      ];
      const r = await run("codegraph", args, { capture: true });
      return r.code === 0;
    })();
  }
  return realInstallDone;
}

const verifyByAgent = {
  claude: verifyClaude,
  opencode: verifyOpenCode,
  codex: verifyCodex,
};

// Test-mode shim writes the same per-agent files real `codegraph install` would.
function testShim(agent: "claude" | "opencode" | "codex"): boolean {
  if (agent === "claude") {
    const cp = agentPaths.claudeCode();
    fs.mkdirSync(cp.dir, { recursive: true });
    const current = fs.existsSync(cp.globalJson) ? JSON.parse(fs.readFileSync(cp.globalJson, "utf8")) : {};
    current.mcpServers = current.mcpServers || {};
    current.mcpServers.codegraph = { type: "stdio", command: "codegraph", args: ["serve", "--mcp"] };
    fs.writeFileSync(cp.globalJson, stringifyJson(current), "utf8");
  } else if (agent === "opencode") {
    const op = agentPaths.opencode();
    fs.mkdirSync(op.dir, { recursive: true });
    const current = fs.existsSync(op.config) ? parseJsonc<any>(fs.readFileSync(op.config, "utf8")) : {};
    current.mcp = current.mcp || {};
    current.mcp.codegraph = { type: "local", command: ["codegraph", "serve", "--mcp"], enabled: true };
    fs.writeFileSync(op.config, stringifyJson(current), "utf8");
  } else {
    const cx = agentPaths.codex();
    fs.mkdirSync(cx.dir, { recursive: true });
    const current = fs.existsSync(cx.config) ? fs.readFileSync(cx.config, "utf8") : "";
    const updated = upsertBlock(current, {
      header: "mcp_servers.codegraph",
      fields: { command: "codegraph", args: ["serve", "--mcp"] },
    });
    fs.writeFileSync(cx.config, updated, "utf8");
  }
  return true;
}

function wire(agent: "claude" | "opencode" | "codex") {
  return async (opts: RunOpts): Promise<boolean> => {
    if (process.env.TOKLESS_TEST === "1") return testShim(agent);
    if (opts.dryRun) return callRealInstall(opts);
    const ran = await callRealInstall(opts);
    return ran && verifyByAgent[agent]();
  };
}

async function unwireClaude(_opts: RunOpts): Promise<boolean> {
  const cp = agentPaths.claudeCode();
  if (!fs.existsSync(cp.globalJson)) return true;
  try {
    const cfg = JSON.parse(fs.readFileSync(cp.globalJson, "utf8"));
    if (cfg?.mcpServers?.codegraph) {
      delete cfg.mcpServers.codegraph;
      fs.writeFileSync(cp.globalJson, stringifyJson(cfg), "utf8");
    }
  } catch { /* file corrupt, leave it */ }
  return true;
}
async function unwireOpenCode(_opts: RunOpts): Promise<boolean> {
  const op = agentPaths.opencode();
  if (!fs.existsSync(op.config)) return true;
  try {
    const cfg = parseJsonc<any>(fs.readFileSync(op.config, "utf8"));
    if (cfg?.mcp?.codegraph) {
      delete cfg.mcp.codegraph;
      fs.writeFileSync(op.config, stringifyJson(cfg), "utf8");
    }
  } catch { /* noop */ }
  return true;
}
async function unwireCodex(_opts: RunOpts): Promise<boolean> {
  const cx = agentPaths.codex();
  if (!fs.existsSync(cx.config)) return true;
  const raw = fs.readFileSync(cx.config, "utf8");
  const next = removeBlock(raw, "mcp_servers.codegraph");
  if (next !== raw) fs.writeFileSync(cx.config, next, "utf8");
  return true;
}

function verifyClaude(): boolean {
  const cp = agentPaths.claudeCode();
  if (!fs.existsSync(cp.globalJson)) return false;
  try {
    const cfg = JSON.parse(fs.readFileSync(cp.globalJson, "utf8"));
    return !!cfg?.mcpServers?.codegraph;
  } catch { return false; }
}
function verifyOpenCode(): boolean {
  const op = agentPaths.opencode();
  if (!fs.existsSync(op.config)) return false;
  try {
    const cfg = parseJsonc<any>(fs.readFileSync(op.config, "utf8"));
    return !!cfg?.mcp?.codegraph;
  } catch { return false; }
}
function verifyCodex(): boolean {
  const cx = agentPaths.codex();
  if (!fs.existsSync(cx.config)) return false;
  const raw = fs.readFileSync(cx.config, "utf8");
  return raw.includes("[mcp_servers.codegraph]");
}

const codegraph: ToolManifest = {
  id: "codegraph",
  label: "CodeGraph",
  description: "MCP server that lets agents query a code knowledge graph instead of reading raw files.",
  homepage: "https://github.com/colbymchenry/codegraph",
  installHint: "npm i -g @colbymchenry/codegraph",
  channel: "npm",

  install: ensureInstalled,
  wireFor: {
    claude: wire("claude"),
    opencode: wire("opencode"),
    codex: wire("codex"),
  },
  unwireFor: {
    claude: unwireClaude,
    opencode: unwireOpenCode,
    codex: unwireCodex,
  },
  verifyFor: {
    claude: verifyClaude,
    opencode: verifyOpenCode,
    codex: verifyCodex,
  },
};

export default codegraph;
