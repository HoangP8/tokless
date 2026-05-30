import * as fs from "node:fs";
import * as path from "node:path";
import * as os from "node:os";
import { which, run } from "./exec.js";
import { log } from "./logger.js";

const MARK_START = "# >>> tokless path >>>";
const MARK_END = "# <<< tokless path <<<";

/** All bin dirs that any of our installers may drop binaries into. */
export function expectedBinDirs(): string[] {
  const home = os.homedir();
  if (process.platform === "win32") {
    return [path.join(home, ".local", "bin"), path.join(home, ".bun", "bin")];
  }
  return [path.join(home, ".local", "bin"), path.join(home, ".bun", "bin"), path.join(home, ".cargo", "bin")];
}

/** Patch process.env.PATH NOW so any same-process spawn picks up new binaries. */
export function ensureProcessPath(): string[] {
  const sep = process.platform === "win32" ? ";" : ":";
  const current = (process.env.PATH ?? "").split(sep);
  const added: string[] = [];
  for (const dir of expectedBinDirs()) {
    if (!current.includes(dir) && fs.existsSync(dir)) {
      current.unshift(dir);
      added.push(dir);
    }
  }
  if (added.length > 0) process.env.PATH = current.join(sep);
  return added;
}

/** Persist the PATH dirs across new shells. Returns the files actually patched. */
export function ensurePersistentPath(): string[] {
  if (process.platform === "win32") return ensurePersistentPathWindows();
  return ensurePersistentPathUnix();
}

function ensurePersistentPathUnix(): string[] {
  const home = os.homedir();
  const dirs = expectedBinDirs();
  const block = renderUnixBlock(dirs);

  const rcs = candidateRcFiles(home).filter((f) => fs.existsSync(f));
  if (rcs.length === 0) rcs.push(path.join(home, ".profile"));

  const patched: string[] = [];
  for (const rc of rcs) {
    const before = readFileSafe(rc);
    const after = upsertBlock(before, block);
    if (after !== before) {
      fs.writeFileSync(rc, after);
      patched.push(rc);
    }
  }
  return patched;
}

function ensurePersistentPathWindows(): string[] {
  const dirs = expectedBinDirs();
  const r = which("powershell");
  if (!r) return [];
  const ps = [
    "$ErrorActionPreference='Stop'",
    "$cur = [Environment]::GetEnvironmentVariable('Path', 'User')",
    `$add = @(${dirs.map((d) => `'${d.replace(/'/g, "''")}'`).join(",")})`,
    "$parts = @($cur -split ';' | Where-Object { $_ -ne '' })",
    "foreach ($d in $add) { if ($parts -notcontains $d) { $parts += $d } }",
    "$new = ($parts -join ';')",
    "if ($new -ne $cur) { [Environment]::SetEnvironmentVariable('Path', $new, 'User') }",
    "Write-Output $new",
  ].join("; ");
  const result = run("powershell", ["-NoProfile", "-Command", ps], { capture: true });
  return result instanceof Promise ? [] : [];
}

function renderUnixBlock(dirs: string[]): string {
  const home = os.homedir();
  const relDirs = dirs.map((d) => (d.startsWith(home) ? '"$HOME' + d.slice(home.length) + '"' : `"${d}"`));
  const lines = [
    MARK_START,
    "# Adds tokless tool bin dirs to PATH (rtk, bun, cargo).",
    `for d in ${relDirs.join(" ")}; do`,
    `  [ -d "$d" ] && case ":$PATH:" in *":$d:"*) ;; *) PATH="$d:$PATH" ;; esac`,
    "done",
    "export PATH",
    MARK_END,
    "",
  ];
  return lines.join("\n");
}

function candidateRcFiles(home: string): string[] {
  const shell = process.env.SHELL ?? "";
  const fish = path.join(home, ".config", "fish", "config.fish");
  void fish;
  if (shell.endsWith("zsh")) return [path.join(home, ".zshrc")];
  if (shell.endsWith("bash")) return [path.join(home, ".bashrc"), path.join(home, ".bash_profile")];
  return [path.join(home, ".zshrc"), path.join(home, ".bashrc"), path.join(home, ".profile")];
}

function readFileSafe(p: string): string {
  try {
    return fs.readFileSync(p, "utf8");
  } catch {
    return "";
  }
}

function upsertBlock(src: string, block: string): string {
  const re = new RegExp(`${escape(MARK_START)}[\\s\\S]*?${escape(MARK_END)}\\n?`, "m");
  if (re.test(src)) return src.replace(re, block);
  const sep = src.length === 0 || src.endsWith("\n") ? "" : "\n";
  return src + sep + "\n" + block;
}

function escape(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** One-shot: patch process PATH + persist to shell rc. Logs a single friendly line. */
export function selfHealPath(): void {
  if (process.env.TOKLESS_TEST === "1") return;
  const added = ensureProcessPath();
  const patched = ensurePersistentPath();
  if (added.length === 0 && patched.length === 0) return;
  const msg: string[] = [];
  if (added.length > 0) msg.push(`PATH updated for this session (+${added.length} dir${added.length > 1 ? "s" : ""})`);
  if (patched.length > 0) msg.push(`persisted to ${patched.map((p) => p.replace(os.homedir(), "~")).join(", ")}`);
  log.debug(msg.join(" · "));
}
