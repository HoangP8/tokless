import * as os from "node:os";
import * as path from "node:path";
import * as fs from "node:fs";
import { run } from "./exec.js";

const REGISTRY = "https://registry.npmjs.org/";

async function resolveFromRegistry(
  pkg: string,
  spec: string,
): Promise<{ version: string; tarball: string } | null> {
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 5000);
    const res = await fetch(`${REGISTRY}${encodeURIComponent(pkg)}`, { signal: ctrl.signal });
    clearTimeout(timer);
    if (!res.ok) return null;
    const doc = (await res.json()) as {
      "dist-tags"?: Record<string, string>;
      versions?: Record<string, { version: string; dist?: { tarball?: string } }>;
    };
    const version = doc["dist-tags"]?.[spec] ?? (doc.versions?.[spec] ? spec : null);
    if (!version) return null;
    const tarball = doc.versions?.[version]?.dist?.tarball;
    if (!tarball) return null;
    return { version, tarball };
  } catch {
    return null;
  }
}

// Universal, cache-skew-resistant global install for an npm package.
export async function npmGlobalInstall(pkg: string, spec = "latest"): Promise<string | null> {
  const resolved = await resolveFromRegistry(pkg, spec);
  const target = resolved?.version ?? null;
  const atSpec = `${pkg}@${spec}`;

  const attempts: string[][] = [
    ["install", "-g", atSpec],
    ["install", "-g", atSpec, "--prefer-online"],
    ["install", "-g", atSpec, "--registry", REGISTRY, "--cache", freshCacheDir(), "--prefer-online"],
  ];
  if (resolved) attempts.push(["install", "-g", resolved.tarball]);

  for (const args of attempts) {
    const r = await run("npm", args, { capture: true });
    const tmpIdx = args.indexOf("--cache");
    if (tmpIdx >= 0) cleanupDir(args[tmpIdx + 1]);
    if (r.code === 0) return target ?? spec;
  }
  return null;
}

function freshCacheDir(): string {
  const dir = path.join(os.tmpdir(), `tokless-npm-${Date.now()}-${Math.random().toString(36).slice(2)}`);
  try {
    fs.mkdirSync(dir, { recursive: true });
  } catch {
  }
  return dir;
}

function cleanupDir(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
  }
}
