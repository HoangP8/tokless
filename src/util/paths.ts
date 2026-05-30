import * as os from "node:os";
import * as path from "node:path";
import * as fs from "node:fs";

export const IS_WIN = process.platform === "win32";
export const IS_MAC = process.platform === "darwin";

function resolveHome(): string {
  return process.env.HOME || os.homedir();
}

export function appDataDir(): string {
  const h = resolveHome();
  if (IS_WIN) return process.env.APPDATA || path.join(h, "AppData", "Roaming");
  if (IS_MAC) return path.join(h, "Library", "Application Support");
  return process.env.XDG_CONFIG_HOME || path.join(h, ".config");
}

let HOME_OVERRIDE: string | null = null;
export function setHomeOverride(p: string | null) {
  HOME_OVERRIDE = p;
}
export function home(): string {
  return HOME_OVERRIDE ?? resolveHome();
}
export function configRoot(): string {
  if (HOME_OVERRIDE) {
    if (IS_WIN) return path.join(HOME_OVERRIDE, "AppData", "Roaming");
    if (IS_MAC) return path.join(HOME_OVERRIDE, "Library", "Application Support");
    return path.join(HOME_OVERRIDE, ".config");
  }
  return appDataDir();
}

export function ensureDir(p: string): void {
  fs.mkdirSync(p, { recursive: true });
}

export function readFileSafe(p: string): string | null {
  try {
    return fs.readFileSync(p, "utf8");
  } catch {
    return null;
  }
}

export function writeFile(p: string, content: string): void {
  ensureDir(path.dirname(p));
  fs.writeFileSync(p, content, "utf8");
}

export function exists(p: string): boolean {
  try {
    fs.accessSync(p);
    return true;
  } catch {
    return false;
  }
}

// Agent-specific config locations resolved against current home/configRoot.
export const agentPaths = {
  claudeCode: () => ({
    dir: path.join(home(), ".claude"),
    settings: path.join(home(), ".claude", "settings.json"),
    globalJson: path.join(home(), ".claude.json"), // mcpServers live here for user scope
    instructions: path.join(home(), ".claude", "CLAUDE.md"),
    skillsDir: path.join(home(), ".claude", "skills"),
  }),
  opencode: () => {
    const dir = path.join(configRoot(), "opencode");
    const candidates = [
      path.join(dir, "opencode.jsonc"),
      path.join(dir, "opencode.json"),
      path.join(dir, "config.json"),
    ];
    const existing = candidates.find((c) => {
      try { fs.accessSync(c); return true; } catch { return false; }
    });
    return {
      dir,
      config: existing ?? path.join(dir, "opencode.jsonc"),
      instructions: path.join(dir, "AGENTS.md"),
      pluginsDir: path.join(dir, "plugins"),
      rulesDir: path.join(dir, "rules"),
    };
  },
  codex: () => {
    const envHome = process.env.CODEX_HOME;
    const dir = envHome && envHome.length > 0 ? envHome : path.join(home(), ".codex");
    return {
      dir,
      config: path.join(dir, "config.toml"),
      instructions: path.join(dir, "AGENTS.md"),
    };
  },
};
