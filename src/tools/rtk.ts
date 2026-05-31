import * as path from "node:path";
import * as fs from "node:fs";
import * as os from "node:os";
import { run, which } from "../util/exec.js";
import { log } from "../util/logger.js";
import { home, agentPaths } from "../util/paths.js";
import type { ToolManifest, RunOpts } from "../core/tool-manifest.js";
import type { AgentId } from "../core/ids.js";

function rtkAssetForThisPlatform(): string | null {
  const arch = process.arch === "arm64" ? "aarch64" : "x86_64";
  if (process.platform === "darwin") return `rtk-${arch}-apple-darwin.tar.gz`;
  if (process.platform === "linux") {
    return process.arch === "arm64"
      ? "rtk-aarch64-unknown-linux-gnu.tar.gz"
      : "rtk-x86_64-unknown-linux-musl.tar.gz";
  }
  if (process.platform === "win32") return `rtk-${arch}-pc-windows-msvc.zip`;
  return null;
}

async function ensureInstalled(opts: RunOpts): Promise<boolean> {
  if (process.env.TOKLESS_TEST === "1") {
    const dest = path.join(home(), ".local", "bin");
    fs.mkdirSync(dest, { recursive: true });
    const rtkPath = path.join(dest, "rtk");
    if (fs.existsSync(rtkPath)) {
      try { fs.unlinkSync(rtkPath); } catch { /* noop */ }
    }
    fs.writeFileSync(rtkPath, "#!/bin/sh\necho ok", { mode: 0o755 });
    const sep = process.platform === "win32" ? ";" : ":";
    const curPath = process.env.PATH || "";
    if (!curPath.split(sep).includes(dest)) process.env.PATH = dest + sep + curPath;
    return true;
  }
  if (which("rtk") && !opts.upgrade) return true;
  if (opts.dryRun) {
    log.sub(opts.upgrade ? "[dry-run] would re-download latest rtk binary" : "[dry-run] would download prebuilt rtk binary");
    return true;
  }
  const asset = rtkAssetForThisPlatform();
  if (asset && (await installPrebuilt(asset))) return true;
  if (process.platform !== "win32" && which("curl") && which("sh")) {
    const r = await run("sh", ["-c", "curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/master/install.sh | sh"]);
    if (r.code === 0) return true;
  }
  if (!which("cargo")) {
    const { installCargo } = await import("../util/runtime-preflight.js");
    await installCargo();
  }
  if (which("cargo")) {
    const r = await run("cargo", ["install", "--git", "https://github.com/rtk-ai/rtk"]);
    if (r.code === 0) return true;
  }
  log.err("Cannot install rtk on this platform. See https://github.com/rtk-ai/rtk for manual install.");
  return false;
}

async function installPrebuilt(asset: string): Promise<boolean> {
  const url = `https://github.com/rtk-ai/rtk/releases/latest/download/${asset}`;
  const dest = path.join(home(), ".local", "bin");
  fs.mkdirSync(dest, { recursive: true });
  log.sub(`downloading ${asset}…`);
  if (process.platform === "win32") {
    const ps = [
      "$ErrorActionPreference='Stop'",
      `Invoke-WebRequest -UseBasicParsing -Uri '${url}' -OutFile $env:TEMP\\\\rtk.zip`,
      `Expand-Archive -Force -Path $env:TEMP\\\\rtk.zip -DestinationPath '${dest}'`,
      `Remove-Item $env:TEMP\\\\rtk.zip`,
    ].join("; ");
    const r = await run("powershell", ["-NoProfile", "-Command", ps]);
    return r.code === 0;
  }
  const tmp = path.join(os.tmpdir(), asset);
  const dl = await run("sh", ["-c", `curl -fsSL '${url}' -o '${tmp}'`]);
  if (dl.code !== 0) return false;
  const ex = await run("sh", ["-c", `tar -xzf '${tmp}' -C '${dest}' && rm -f '${tmp}' && chmod +x '${dest}/rtk'`]);
  return ex.code === 0;
}

function testShim(agent: AgentId): void {
  if (agent === "codex") {
    const dir = agentPaths.codex().dir;
    fs.mkdirSync(dir, { recursive: true });
    const stub = "# RTK\nInstalled by tokless. See https://github.com/rtk-ai/rtk\n";
    if (!fs.existsSync(path.join(dir, "AGENTS.md"))) fs.writeFileSync(path.join(dir, "AGENTS.md"), stub);
    if (!fs.existsSync(path.join(dir, "RTK.md"))) fs.writeFileSync(path.join(dir, "RTK.md"), stub);
  } else if (agent === "claude") {
    const dir = path.join(home(), ".claude");
    fs.mkdirSync(dir, { recursive: true });
    const f = path.join(dir, "RTK.md");
    if (!fs.existsSync(f)) fs.writeFileSync(f, "# RTK\nInstalled by tokless.\n");
    const settingsPath = path.join(dir, "settings.json");
    if (!claudeSettingsHasRtkHook(settingsPath)) {
      const settings = fs.existsSync(settingsPath)
        ? (JSON.parse(fs.readFileSync(settingsPath, "utf8")) as ClaudeSettings)
        : {};
      const hooks = (settings.hooks ??= {});
      const pre = Array.isArray(hooks.PreToolUse) ? hooks.PreToolUse : (hooks.PreToolUse = []);
      pre.push({ matcher: "Bash", hooks: [{ type: "command", command: "rtk hook claude" }] });
      fs.writeFileSync(settingsPath, JSON.stringify(settings, null, 2), "utf8");
    }
  } else if (agent === "opencode") {
    const dir = path.join(home(), ".config", "opencode", "plugins");
    fs.mkdirSync(dir, { recursive: true });
    const f = path.join(dir, "rtk.ts");
    if (!fs.existsSync(f)) fs.writeFileSync(f, "// rtk plugin shim (tokless test mode)\nexport const Plugin = async () => ({});\n");
  }
}

interface ClaudeHookEntry {
  matcher?: string;
  hooks?: { type?: string; command?: string }[];
}
interface ClaudeSettings {
  hooks?: { PreToolUse?: ClaudeHookEntry[]; [k: string]: unknown };
  [k: string]: unknown;
}

function claudeSettingsHasRtkHook(settingsPath: string): boolean {
  if (!fs.existsSync(settingsPath)) return false;
  try {
    const s = JSON.parse(fs.readFileSync(settingsPath, "utf8")) as ClaudeSettings;
    const pre = s.hooks?.PreToolUse;
    if (!Array.isArray(pre)) return false;
    return pre.some((e) => e.hooks?.some((h) => typeof h.command === "string" && h.command.includes("rtk hook")));
  } catch {
    return false;
  }
}

function wire(agent: AgentId) {
  return async (opts: RunOpts): Promise<boolean> => {
    if (opts.dryRun) {
      const flag = agent === "claude" ? "" : agent === "opencode" ? " --opencode" : " --codex";
      log.sub(`[dry-run] would run: rtk init -g${flag}`);
      return true;
    }
    if (process.env.TOKLESS_TEST === "1") {
      testShim(agent);
      return true;
    }
    const args = ["init", "-g"];
    if (agent === "opencode") args.push("--opencode");
    else if (agent === "codex") args.push("--codex");
    const r = await run("rtk", args, { capture: true });
    if (r.code !== 0) {
      log.debug(`rtk init exited ${r.code}: ${r.stderr.trim().slice(0, 200)}`);
      return false;
    }
    const verify = await run("rtk", ["init", "--show"], { capture: true });
    if (verify.code !== 0) {
      log.err(`rtk init --show failed: ${verify.stderr.trim().slice(0, 200)}`);
      return false;
    }
    return true;
  };
}

const rtk: ToolManifest = {
  id: "rtk",
  label: "RTK",
  description: "Token-efficient command-runner replacing shell commands with deterministic primitives.",
  homepage: "https://github.com/rtk-ai/rtk",
  installHint: "Prebuilt binary from GitHub releases (no Rust required).",
  channel: "github",

  install: ensureInstalled,
  wireFor: {
    claude: wire("claude"),
    opencode: wire("opencode"),
    codex: wire("codex"),
  },
  verifyFor: {
    claude: () => {
      const dir = path.join(home(), ".claude");
      return claudeSettingsHasRtkHook(path.join(dir, "settings.json")) || fs.existsSync(path.join(dir, "RTK.md"));
    },
    opencode: () => fs.existsSync(path.join(home(), ".config", "opencode", "plugins", "rtk.ts")),
    codex: () => fs.existsSync(path.join(agentPaths.codex().dir, "RTK.md")),
  },
};

export default rtk;
