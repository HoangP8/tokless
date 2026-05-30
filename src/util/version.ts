// Version is injected at build time via `--define TOKLESS_VERSION="x.y.z"`.
declare const TOKLESS_VERSION: string | undefined;

export function toklessVersion(): string {
  try {
    if (typeof TOKLESS_VERSION === "string" && TOKLESS_VERSION.length > 0) {
      return TOKLESS_VERSION;
    }
  } catch {
  }
  return devVersionFromPackageJson() ?? "0.0.0-dev";
}

function devVersionFromPackageJson(): string | null {
  try {
    const { readFileSync } = require("node:fs") as typeof import("node:fs");
    const path = require("node:path") as typeof import("node:path");
    const { fileURLToPath } = require("node:url") as typeof import("node:url");
    const here = path.dirname(fileURLToPath(import.meta.url));
    for (let i = 0; i <= 4; i++) {
      const candidate = path.join(here, "../".repeat(i) || "./", "package.json");
      try {
        const pkg = JSON.parse(readFileSync(candidate, "utf8")) as { name?: string; version?: string };
        if (pkg.name === "tokless" && pkg.version) return pkg.version;
      } catch {
      }
    }
  } catch {
  }
  return null;
}
