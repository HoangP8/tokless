import "../bootstrap.js";
// `tokless update` — version-aware refresh.

import { runInit } from "./init.js";
import { log } from "../util/logger.js";
import { c, sym } from "../util/colors.js";
import {
  gatherVersions,
  bustVersionCache,
  semverCompare,
  type VersionInfo,
} from "../util/versions.js";
import { listTools } from "../core/registry.js";
import type { InitOptions } from "./init.js";

export async function runUpdate(opts: InitOptions = {}): Promise<number> {
  console.log("");
  console.log("  " + c.bold(c.cyan("tokless update")) + c.gray("  refresh tools to latest"));
  console.log("");

  if (opts.dryRun) {
    log.info("Dry run — would probe registries and reinstall changed tools only.");
  }

  // 1) Probe upstream.
  bustVersionCache();
  const probing = "  " + c.gray("probing upstream…");
  if (process.stdout.isTTY) process.stdout.write(probing);
  else console.log(probing);
  let versions: Record<string, VersionInfo> = {};
  try {
    versions = await gatherVersions();
  } catch {
  }
  if (process.stdout.isTTY) process.stdout.write("\r\x1b[2K");
  else console.log("");

  // 2) Print version diff table.
  const toolsToShow = listTools().map((t) => t.id);
  const changed: string[] = [];
  for (const t of toolsToShow) {
    const info = versions[t];
    const installed = info?.installed ?? c.gray("not on PATH");
    const latest = info?.latest ?? c.gray("?");
    let mark = c.gray(sym.bullet);
    let suffix = c.gray(" (pinned)");
    if (info?.installed && info.latest && semverCompare(info.installed, info.latest) < 0) {
      mark = c.yellow("↑");
      suffix = c.yellow(" → upgrade");
      changed.push(t);
    } else if (!info?.installed && info?.latest) {
      mark = c.yellow("+");
      suffix = c.yellow(" → install");
      changed.push(t);
    } else if (info?.installed && info?.latest) {
      mark = c.green(sym.check);
      suffix = c.gray(" (up to date)");
    }
    console.log(
      `  ${mark} ${t.padEnd(14)} ${String(installed).padEnd(10)} → ${String(latest).padEnd(10)}${suffix}`,
    );
  }
  console.log("");

  if (opts.dryRun) {
    log.info(
      changed.length > 0
        ? `Would upgrade: ${changed.join(", ")}`
        : "Everything up to date.",
    );
    console.log("");
    return 0;
  }

  // 3) If nothing changed, stop — no reason to re-run the install pass.
  if (changed.length === 0) {
    log.ok("Everything up to date.");
    console.log("");
    return 0;
  }

  // 4) Re-install only the tools whose version moved.
  console.log("  " + c.bold(`Upgrading: ${changed.join(", ")}`));
  return await runInit({
    ...opts,
    yes: true,
    upgrade: true,
    tools: opts.tools ?? (changed as InitOptions["tools"]),
  });
}
