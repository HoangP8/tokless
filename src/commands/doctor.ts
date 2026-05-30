// `tokless doctor` — for each detected agent, ask each tool with a verifyFor
// entry whether it's currently wired. Print one line per agent + an update hint.

import "../bootstrap.js";
import { c, sym } from "../util/colors.js";
import { log } from "../util/logger.js";
import { gatherVersions, countOutdated, semverCompare } from "../util/versions.js";
import { listAgents, listTools } from "../core/registry.js";

interface AgentReport {
  label: string;
  installed: boolean;
  wired: boolean;
  missing: string[];
}

export async function runDoctor(opts: { offline?: boolean } = {}): Promise<number> {
  console.log("");
  console.log("  " + c.bold(c.cyan("tokless doctor")) + c.gray("  quick health check"));
  console.log("");

  const tools = listTools();
  const reports: AgentReport[] = [];
  for (const agent of listAgents()) {
    const det = agent.detect();
    if (!det.installed) {
      reports.push({ label: agent.label, installed: false, wired: false, missing: [] });
      continue;
    }
    const missing: string[] = [];
    for (const tool of tools) {
      const verify = tool.verifyFor?.[agent.id];
      if (!verify) continue;
      const ok = await verify();
      if (ok === false) missing.push(tool.label);
    }
    reports.push({ label: agent.label, installed: true, wired: missing.length === 0, missing });
  }

  for (const r of reports) summary(r);

  if (!opts.offline && process.env.TOKLESS_TEST !== "1") {
    const probing = "  " + c.gray("checking for updates…");
    if (process.stdout.isTTY) process.stdout.write(probing);
    else console.log(probing);
    try {
      const v = await gatherVersions();
      const outdated = countOutdated(v);
      if (process.stdout.isTTY) process.stdout.write("\r\x1b[2K");
      else console.log("");
      if (outdated > 0) {
        log.warn(`${outdated} update${outdated > 1 ? "s" : ""} available — run ${c.cyan("tokless update")}`);
        for (const [name, info] of Object.entries(v)) {
          if (info.installed && info.latest && semverCompare(info.installed, info.latest) < 0) {
            console.log("  " + c.gray(`• ${name.padEnd(14)} ${info.installed} → ${c.green(info.latest)}`));
          }
        }
      } else {
        log.ok("All up to date.");
      }
    } catch {
      if (process.stdout.isTTY) process.stdout.write("\r\x1b[2K");
      log.warn("Update check skipped (offline?). Re-run with network for version status.");
    }
  }

  const broken = reports.filter((r) => r.installed && !r.wired);
  if (broken.length > 0) {
    console.log("");
    log.info(`Run ${c.cyan("tokless")} to fix.`);
  }
  console.log("");
  return 0;
}

function summary(r: AgentReport): void {
  const mark = !r.installed ? c.gray(sym.bullet) : r.wired ? c.green(sym.check) : c.yellow(sym.warn);
  const label = r.label.padEnd(14);
  let status: string;
  if (!r.installed) status = c.gray("not installed");
  else if (r.wired) status = c.gray("all tools wired");
  else status = c.yellow(`missing: ${r.missing.join(", ")}`);
  console.log(`  ${mark} ${label} ${status}`);
}
