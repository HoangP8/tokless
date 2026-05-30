// Remote version lookups for the 4 upstream tools + tokless itself.
import * as path from "node:path";
import * as fs from "node:fs";
import * as os from "node:os";
import { run, which } from "./exec.js";

export interface VersionInfo {
  installed: string | null;
  latest: string | null;
  channel: "npm" | "github" | "binary";
}

interface CacheShape {
  ts: number;
  map: Record<string, VersionInfo>;
}

const CACHE_PATH = path.join(os.homedir(), ".cache", "tokless", "versions.json");
const CACHE_TTL_MS = 6 * 60 * 60 * 1000;

function loadCache(): CacheShape | null {
  try {
    if (!fs.existsSync(CACHE_PATH)) return null;
    const obj = JSON.parse(fs.readFileSync(CACHE_PATH, "utf8")) as CacheShape;
    if (Date.now() - obj.ts > CACHE_TTL_MS) return null;
    return obj;
  } catch {
    return null;
  }
}

function saveCache(map: Record<string, VersionInfo>): void {
  try {
    fs.mkdirSync(path.dirname(CACHE_PATH), { recursive: true });
    fs.writeFileSync(CACHE_PATH, JSON.stringify({ ts: Date.now(), map }, null, 2));
  } catch {
  }
}

/** Fetch JSON from a URL with a hard timeout. */
async function fetchJsonWithTimeout<T>(url: string, ms = 3000): Promise<T | null> {
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), ms);
    const res = await fetch(url, { signal: ctrl.signal });
    clearTimeout(timer);
    if (!res.ok) return null;
    return (await res.json()) as T;
  } catch {
    return null;
  }
}

async function npmLatest(pkg: string): Promise<string | null> {
  const data = await fetchJsonWithTimeout<{ "dist-tags"?: { latest?: string } }>(
    `https://registry.npmjs.org/${encodeURIComponent(pkg)}`,
  );
  return data?.["dist-tags"]?.latest ?? null;
}

async function githubLatestRelease(repo: string): Promise<string | null> {
  const data = await fetchJsonWithTimeout<{ tag_name?: string; name?: string }>(
    `https://api.github.com/repos/${repo}/releases/latest`,
  );
  const tag = data?.tag_name ?? data?.name ?? null;
  return tag ? tag.replace(/^v/, "") : null;
}

async function rtkInstalledVersion(): Promise<string | null> {
  if (!which("rtk")) return null;
  const r = await run("rtk", ["--version"], { capture: true });
  const m = /(\d+\.\d+\.\d+)/.exec(r.stdout || r.stderr);
  return m ? m[1] : null;
}

async function npmInstalledVersion(pkg: string): Promise<string | null> {
  if (which("npm")) {
    const r = await run("npm", ["ls", "-g", "--depth=0", "--json", pkg], { capture: true });
    try {
      const j = JSON.parse(r.stdout || "{}") as {
        dependencies?: Record<string, { version?: string }>;
      };
      const v = j.dependencies?.[pkg]?.version;
      if (v) return v;
    } catch {
    }
  }
  return null;
}

/** Fetch all version info, using cache when fresh. */
export async function gatherVersions(): Promise<Record<string, VersionInfo>> {
  if (process.env.TOKLESS_TEST === "1") {
    return {
      rtk: { installed: "0.40.0", latest: "0.40.0", channel: "github" },
      caveman: { installed: null, latest: "1.0.0", channel: "github" },
      codegraph: { installed: null, latest: "0.9.0", channel: "npm" },
      "context-mode": { installed: null, latest: "1.0.0", channel: "npm" },
      tokless: { installed: "0.1.0", latest: "0.1.0", channel: "npm" },
    };
  }
  const cached = loadCache();
  if (cached) return cached.map;

  const out: Record<string, VersionInfo> = {};

  // RTK — installed via rtk --version; latest via GitHub releases.
  const [rtkI, rtkL] = await Promise.all([rtkInstalledVersion(), githubLatestRelease("rtk-ai/rtk")]);
  out["rtk"] = { installed: rtkI, latest: rtkL, channel: "github" };

  // Caveman — no canonical "installed" probe (it's installer-driven). Latest from GitHub release or repo commit
  const cvmL = await githubLatestRelease("JuliusBrussee/caveman");
  out["caveman"] = { installed: null, latest: cvmL, channel: "github" };

  // CodeGraph + Context-Mode — both npm, both run via `npx -y …@latest`
  const [cgL, ctxL] = await Promise.all([
    npmLatest("@colbymchenry/codegraph"),
    npmLatest("context-mode"),
  ]);
  out["codegraph"] = { installed: await npmInstalledVersion("@colbymchenry/codegraph"), latest: cgL, channel: "npm" };
  out["context-mode"] = {
    installed: await npmInstalledVersion("context-mode"),
    latest: ctxL,
    channel: "npm",
  };

  // tokless.
  const [tkI, tkL] = await Promise.all([npmInstalledVersion("tokless"), npmLatest("tokless")]);
  out["tokless"] = { installed: tkI, latest: tkL, channel: "npm" };

  saveCache(out);
  return out;
}

/** Compare two semver-ish strings. -1 / 0 / 1. Falls back to string compare on garbage. */
export function semverCompare(a: string | null, b: string | null): number {
  if (a == null && b == null) return 0;
  if (a == null) return -1;
  if (b == null) return 1;
  const pa = a.replace(/^v/, "").split(".").map((x) => parseInt(x, 10) || 0);
  const pb = b.replace(/^v/, "").split(".").map((x) => parseInt(x, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const da = pa[i] ?? 0;
    const db = pb[i] ?? 0;
    if (da !== db) return da > db ? 1 : -1;
  }
  return 0;
}

/** Count how many tools have a newer remote version than the local one. */
export function countOutdated(map: Record<string, VersionInfo>): number {
  let n = 0;
  for (const v of Object.values(map)) {
    if (v.installed && v.latest && semverCompare(v.installed, v.latest) < 0) n++;
  }
  return n;
}

/** Invalidate the cache so the next call re-probes. */
export function bustVersionCache(): void {
  try {
    if (fs.existsSync(CACHE_PATH)) fs.unlinkSync(CACHE_PATH);
  } catch {
  }
}
