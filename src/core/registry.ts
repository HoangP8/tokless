import type { AgentManifest } from "./agent-manifest.js";
import type { ToolManifest } from "./tool-manifest.js";
import type { AgentId, ToolId } from "./ids.js";

const _agents = new Map<AgentId, AgentManifest>();
const _tools = new Map<ToolId, ToolManifest>();

export function registerAgent(a: AgentManifest): void {
  _agents.set(a.id, a);
}
export function registerTool(t: ToolManifest): void {
  _tools.set(t.id, t);
}
export function listAgents(): AgentManifest[] {
  return [..._agents.values()];
}
export function listTools(): ToolManifest[] {
  return [..._tools.values()];
}
export function getAgent(id: AgentId): AgentManifest | undefined {
  return _agents.get(id);
}
export function getTool(id: ToolId): ToolManifest | undefined {
  return _tools.get(id);
}
export function agentIds(): AgentId[] {
  return [..._agents.keys()];
}
export function toolIds(): ToolId[] {
  return [..._tools.keys()];
}
