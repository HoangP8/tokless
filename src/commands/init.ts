// Main install command — invoked by bare `tokless`.
//   1) Install each tool's binary globally (no-op if already installed).
//   2) Pick which agents to wire.
//   3) For each (agent, tool) where the tool has a wireFor entry → wire it.

import "../bootstrap.js";
import { setQuiet } from "../util/logger.js";
import { c, sym } from "../util/colors.js";
import { multiSelect } from "../util/prompt.js";
import { Progress, withSilencedLogs } from "../util/progress.js";
import { selfHealPath } from "../util/path-setup.js";

import { ensureNodeForTools } from "../util/runtime-preflight.js";
import { listAgents, listTools, getAgent } from "../core/registry.js";
import type { AgentId, ToolId } from "../core/ids.js";

export interface InitOptions {
  agents?: AgentId[];
  tools?: ToolId[];
  dryRun?: boolean;
  yes?: boolean;
  verbose?: boolean;
  upgrade?: boolean;
}

export async function runInit(opts: InitOptions = {}): Promise<number> {
  const verbose = opts.verbose === true;
  setQuiet(!verbose);

  console.log("");
  console.log("  " + c.bold(c.cyan("tokless")) + c.gray("  global token-saver for AI agents"));

  if (!opts.dryRun) {
    await ensureNodeForTools();
  }

  // 1) Install every selected tool globally.
  const allTools = listTools();
  const tools = opts.tools ? allTools.filter((t) => opts.tools!.includes(t.id)) : allTools;
  const toolBar = new Progress("");
  toolBar.start(tools.length);
  for (const tool of tools) {
    toolBar.begin(tool.label);
    try {
      await withSilencedLogs(() => tool.install({ dryRun: opts.dryRun, upgrade: opts.upgrade }));
      toolBar.complete();
    } catch (err) {
      toolBar.fail((err as Error).message.split("\n")[0]);
    }
  }
  toolBar.done();

  if (!opts.dryRun) selfHealPath();

  // 2) Pick which agents to wire.
  const allAgents = listAgents();
  const detection = allAgents.map((a) => ({ id: a.id, ...a.detect() }));
  const installedIds = new Set(detection.filter((d) => d.installed).map((d) => d.id));

  let requested: AgentId[];
  if (opts.agents && opts.agents.length > 0) {
    requested = opts.agents;
  } else {
    console.log("");
    requested = await multiSelect<AgentId>(
      "Which AI agent(s) to wire up?",
      allAgents.map((a) => ({
        value: a.id,
        label: a.label,
        hint: installedIds.has(a.id) ? c.gray("installed") : c.gray("not installed"),
        selected: installedIds.has(a.id),
      })),
    );
  }

  const wireIds = requested.filter((id) => installedIds.has(id));
  const skipped = requested.filter((id) => !installedIds.has(id));
  for (const id of skipped) {
    const a = getAgent(id);
    if (!a) continue;
    console.log("  " + c.yellow(sym.warn) + ` ${a.label} not installed — install it first: ${c.cyan(a.homepage)}`);
  }

  if (wireIds.length === 0) {
    setQuiet(false);
    if (skipped.length === 0) {
      console.log("  " + c.gray("Nothing selected. Tools are installed; re-run to wire an agent."));
    }
    console.log("");
    return 0;
  }

  // 3) Wire each (agent, tool) pair where the tool has a wireFor entry.
  const failures: Record<string, string[]> = {};
  const wireBar = new Progress("");
  wireBar.start(wireIds.length);
  for (const agentId of wireIds) {
    const agent = getAgent(agentId)!;
    wireBar.begin(agent.label);
    const failed: string[] = [];
    try {
      await withSilencedLogs(async () => {
        for (const tool of tools) {
          const fn = tool.wireFor[agentId];
          if (!fn) continue;
          let ok = false;
          try {
            ok = await fn({ dryRun: opts.dryRun });
          } catch {
            ok = false;
          }
          // On a real run, confirm the agent can actually see the tool.
          // Skipped under TOKLESS_TEST: the suite asserts shapes directly.
          if (ok && !opts.dryRun && process.env.TOKLESS_TEST !== "1") {
            const verify = tool.verifyFor?.[agentId];
            if (verify) ok = (await verify()) === true;
          }
          if (!ok) failed.push(tool.label);
        }
      });
      if (failed.length === 0) wireBar.complete();
      else wireBar.fail(`${failed.length} tool(s) not wired`);
    } catch (err) {
      wireBar.fail((err as Error).message.split("\n")[0]);
      failed.push("(aborted)");
    }
    if (failed.length > 0) failures[agentId] = failed;
  }
  wireBar.done();
  setQuiet(false);

  console.log("");
  const fullyOk = wireIds.filter((id) => !failures[id]);
  if (fullyOk.length > 0) {
    console.log(
      "  " + c.green(sym.check) + " Equipped " +
        c.bold(fullyOk.map((id) => getAgent(id)!.label).join(", ")) + ".",
    );
  }
  for (const [id, failed] of Object.entries(failures)) {
    console.log(
      "  " + c.yellow(sym.warn) + ` ${getAgent(id as AgentId)!.label}: ` +
        `${failed.join(", ")} not wired. Run ${c.cyan("tokless doctor")} for details.`,
    );
  }
  console.log("");
  return Object.keys(failures).length > 0 ? 1 : 0;
}
