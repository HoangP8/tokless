import { run, which, whichAny } from "./exec.js";
import { log } from "./logger.js";

// Detect available package runtimes and a usable npx-equivalent.

export interface RuntimeReport {
  node: string | null;
  npm: string | null;
  npx: string | null;
  bun: string | null;
  bunx: string | null;
  cargo: string | null;
  python: string | null;
}

export function detectRuntimes(): RuntimeReport {
  return {
    node: which("node"),
    npm: which("npm"),
    npx: which("npx"),
    bun: which("bun"),
    bunx: which("bunx"),
    cargo: which("cargo"),
    python: which("python3") ?? which("python"),
  };
}

export function pickJsRunner(): { cmd: string; runArgs: string[] } | null {
  const npx = which("npx");
  if (npx) return { cmd: "npx", runArgs: ["-y"] };
  const bunx = which("bunx");
  if (bunx) return { cmd: "bunx", runArgs: [] };
  return null;
}

export async function ensureNpmInstall(pkg: string, opts: { global?: boolean } = {}): Promise<boolean> {
  const npm = which("npm");
  const bun = which("bun");
  if (npm) {
    const args = ["install", opts.global ? "-g" : "", `${pkg}@latest`].filter(Boolean);
    const r = await run("npm", args);
    return r.code === 0;
  }
  if (bun) {
    const args = ["add", opts.global ? "-g" : "", `${pkg}@latest`].filter(Boolean);
    const r = await run("bun", args);
    return r.code === 0;
  }
  log.err(`Neither npm nor bun is installed — cannot install ${pkg}.`);
  return false;
}

export function reportRuntimes(r: RuntimeReport): void {
  const fmt = (name: string, p: string | null) =>
    p ? log.sub(`${name}: ${p}`) : log.sub(`${name}: not found`);
  fmt("node", r.node);
  fmt("npm/npx", r.npm ?? r.npx);
  fmt("bun/bunx", r.bun ?? r.bunx);
  fmt("cargo", r.cargo);
  fmt("python", r.python);
}
