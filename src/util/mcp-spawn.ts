// Picks the spawn shape for an MCP server entry written into an agent config.

import { which } from "./exec.js";

export interface McpSpawn {
  command: string;
  args: string[];
}

const PKG_FOR_BIN: Record<string, string> = {
  "context-mode": "context-mode",
  codegraph: "@colbymchenry/codegraph",
};

export function pickMcpSpawn(bin: string, extraArgs: string[] = []): McpSpawn {
  if (which(bin)) {
    return { command: bin, args: extraArgs };
  }
  const pkg = PKG_FOR_BIN[bin] ?? bin;
  return { command: "npx", args: ["--no-install", pkg, ...extraArgs] };
}
