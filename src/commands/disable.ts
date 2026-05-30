// `tokless disable`   — un-wire each agent (each tool's unwireFor entry runs).
// `tokless uninstall` — same, then nuke tokless's own cache dir.

import "../bootstrap.js";
import * as fs from "node:fs";
import * as path from "node:path";
import { listAgents, listTools, getAgent } from "../core/registry.js";
import type { AgentId } from "../core/ids.js";
import { c, sym } from "../util/colors.js";
import { Progress } from "../util/progress.js";

export interface DisableOptions {
  agents?: AgentId[];
  dryRun?: boolean;
  verbose?: boolean;
  removeTools?: boolean;
}

export async function runDisable(opts: DisableOptions = {}): Promise<number> {
  return runImpl({ ...opts, removeTools: false }, "Disabled");
}

export async function runUninstall(opts: DisableOptions = {}): Promise<number> {
  return runImpl({ ...opts, removeTools: true }, "Uninstalled");
}

async function runImpl(opts: DisableOptions, verb: string): Promise<number> {
  console.log("");
  console.log("  " + c.bold(c.cyan("tokless")) + c.gray(`  ${verb.toLowerCase()}`));

  const detected = listAgents()
    .map((a) => ({ id: a.id, ...a.detect() }))
    .filter((d) => d.installed)
    .map((d) => d.id);
  const agentIds = opts.agents ? opts.agents.filter((id) => detected.includes(id)) : detected;
  if (agentIds.length === 0) {
    console.log("  " + c.gray("nothing wired."));
    console.log("");
    return 0;
  }

  const tools = listTools();
  const bar = new Progress("");
  bar.start(agentIds.length);
  for (const id of agentIds) {
    const agent = getAgent(id)!;
    bar.begin(agent.label);
    try {
      for (const tool of tools) {
        const unwire = tool.unwireFor?.[id];
        if (unwire && !opts.dryRun) await unwire({ dryRun: opts.dryRun });
      }
      bar.complete();
    } catch (err) {
      bar.fail((err as Error).message.split("\n")[0]);
    }
  }
  bar.done();

  if (opts.removeTools && !opts.dryRun) {
    const cacheDir = path.join(process.env.HOME || "", ".cache", "tokless");
    if (fs.existsSync(cacheDir)) fs.rmSync(cacheDir, { recursive: true, force: true });
  }

  console.log("");
  console.log(
    "  " +
      c.green(sym.check) +
      ` ${verb} for ${c.bold(agentIds.map((id) => getAgent(id)!.label).join(", "))}.`,
  );
  console.log("");
  return 0;
}
