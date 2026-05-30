import { run } from "../util/exec.js";
import { log } from "../util/logger.js";
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
    opencode: (opts) =>
      exec(
        "npx",
        ["-y", "github:JuliusBrussee/caveman", "--", "--only", "opencode"],
        opts,
        "npx -y github:JuliusBrussee/caveman -- --only opencode",
      ),
    codex: (opts) =>
      exec(
        "npx",
        ["skills", "add", "JuliusBrussee/caveman", "-a", "codex"],
        opts,
        "npx skills add JuliusBrussee/caveman -a codex",
      ),
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
