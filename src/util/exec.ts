import { spawn, spawnSync, type SpawnOptions } from "node:child_process";
import { log } from "./logger.js";

// Promise-based exec wrappers that surface stdout/stderr cleanly.

export interface ExecResult {
  code: number;
  stdout: string;
  stderr: string;
}

export async function run(
  cmd: string,
  args: string[],
  opts: SpawnOptions & { capture?: boolean; quiet?: boolean } = {},
): Promise<ExecResult> {
  const { capture = false, quiet = false, ...spawnOpts } = opts;
  return new Promise<ExecResult>((resolve) => {
    const child = spawn(cmd, args, {
      stdio: capture ? ["ignore", "pipe", "pipe"] : quiet ? ["ignore", "ignore", "ignore"] : "inherit",
      ...spawnOpts,
    });
    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (d) => (stdout += d.toString()));
    child.stderr?.on("data", (d) => (stderr += d.toString()));
    child.on("error", (err) => {
      log.debug(`spawn error for ${cmd}: ${err.message}`);
      resolve({ code: 127, stdout, stderr: stderr + err.message });
    });
    child.on("close", (code) => resolve({ code: code ?? 0, stdout, stderr }));
  });
}

export function which(bin: string): string | null {
  const PATH = process.env.PATH || "";
  const isWin = process.platform === "win32";
  const exts = isWin ? (process.env.PATHEXT || ".EXE;.CMD;.BAT").split(";") : [""];
  const sep = isWin ? ";" : ":";
  const fs = require("node:fs") as typeof import("node:fs");
  const path = require("node:path") as typeof import("node:path");
  for (const dir of PATH.split(sep)) {
    if (!dir) continue;
    for (const ext of exts) {
      const p = path.join(dir, bin + ext);
      try {
        if (fs.statSync(p).isFile()) return p;
      } catch {
      }
    }
  }
  return null;
}

export function whichAny(bins: string[]): { bin: string; path: string } | null {
  for (const b of bins) {
    const p = which(b);
    if (p) return { bin: b, path: p };
  }
  return null;
}

export function trySync(cmd: string, args: string[]): ExecResult {
  const r = spawnSync(cmd, args, { encoding: "utf8" });
  return {
    code: r.status ?? 127,
    stdout: r.stdout ?? "",
    stderr: r.stderr ?? r.error?.message ?? "",
  };
}
