import fs from "node:fs";
import path from "node:path";
import { run } from "../util/exec.js";
import { log } from "../util/logger.js";
import { agentPaths } from "../util/paths.js";
import type { ToolManifest, RunOpts } from "../core/tool-manifest.js";

// caveman delegates entirely to its own per-agent installers.
// Source of truth: https://github.com/JuliusBrussee/caveman/blob/main/INSTALL.md

async function exec(bin: string, args: string[], opts: RunOpts, dryHint: string): Promise<boolean> {
  if (opts.dryRun) {
    log.sub(`[dry-run] would run: ${dryHint}`);
    return true;
  }
  if (process.env.TOKLESS_TEST === "1") return true;
  const r = await run(bin, args, { capture: true });
  if (r.code !== 0) {
    log.err(`caveman command failed: ${r.stderr.slice(0, 200)}`);
    return false;
  }
  return true;
}

// caveman's OpenCode installer copies command files into ~/.config/opencode/commands
// but does not create that directory first, so it ENOENTs on a fresh config.
function ensureOpencodeCommandsDir(): void {
  try {
    fs.mkdirSync(path.join(agentPaths.opencode().dir, "commands"), { recursive: true });
  } catch { /* best effort */ }
}

// The functional artifact: caveman's OpenCode plugin entrypoint.
function opencodePluginInstalled(): boolean {
  return fs.existsSync(path.join(agentPaths.opencode().dir, "plugins", "caveman", "plugin.js"));
}

const caveman: ToolManifest = {
  id: "caveman",
  label: "Caveman",
  description: "Skill that compresses agent prompts using primitive English.",
  homepage: "https://github.com/JuliusBrussee/caveman",
  installHint: "Installed per-agent by Caveman's own CLI.",
  channel: "github",

  install: async () => true,

  wireFor: {
    claude: (opts) =>
      exec(
        "sh",
        ["-c", "claude plugin marketplace add JuliusBrussee/caveman && claude plugin install caveman@caveman"],
        opts,
        "claude plugin marketplace add JuliusBrussee/caveman && claude plugin install caveman@caveman",
      ),
    opencode: async (opts) => {
      if (!opts.dryRun && process.env.TOKLESS_TEST !== "1") ensureOpencodeCommandsDir();
      const ran = await exec(
        "npx",
        ["-y", "github:JuliusBrussee/caveman", "--", "--only", "opencode"],
        opts,
        "npx -y github:JuliusBrussee/caveman -- --only opencode",
      );
      // caveman's installer exits non-zero on its own missing optional command
      // file, but the functional plugin still lands. Trust the artifact.
      if (opts.dryRun || process.env.TOKLESS_TEST === "1") return ran;
      return ran || opencodePluginInstalled();
    },
    codex: (opts) =>
      exec(
        "npx",
        ["-y", "skills", "add", "JuliusBrussee/caveman", "-a", "codex", "-y"],
        opts,
        "npx -y skills add JuliusBrussee/caveman -a codex -y",
      ),
  },

  verifyFor: {
    opencode: () => opencodePluginInstalled(),
  },

  unwireFor: {
    claude: (opts) =>
      exec(
        "npx",
        ["-y", "github:JuliusBrussee/caveman", "--", "--uninstall", "--only", "claude"],
        opts,
        "npx -y github:JuliusBrussee/caveman -- --uninstall --only claude",
      ),
    opencode: (opts) =>
      exec(
        "npx",
        ["-y", "github:JuliusBrussee/caveman", "--", "--uninstall", "--only", "opencode"],
        opts,
        "npx -y github:JuliusBrussee/caveman -- --uninstall --only opencode",
      ),
    codex: (opts) => exec("npx", ["skills", "remove", "caveman"], opts, "npx skills remove caveman"),
  },
};

export default caveman;
