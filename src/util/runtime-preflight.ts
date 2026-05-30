import { run, which } from "./exec.js";
import { log } from "./logger.js";
import { confirm } from "./prompt.js";

export type RuntimeId = "node" | "npx" | "npm" | "cargo";

export type RuntimeReport = {
  node: boolean;
  npx: boolean;
  npm: boolean;
  cargo: boolean;
};

export function detectRuntimes(): RuntimeReport {
  return {
    node: !!which("node"),
    npx: !!which("npx"),
    npm: !!which("npm"),
    cargo: !!which("cargo"),
  };
}

export async function ensureNodeForTools(): Promise<boolean> {
  const have = detectRuntimes();
  if (have.npm && have.npx) return true;

  log.warn("CodeGraph and Context-Mode need Node.js (npm/npx), which isn't installed.");
  const yes = await confirm("Install the latest Node.js LTS now?", true);
  if (!yes) {
    log.info("Skipping Node install. Install it later: https://nodejs.org/en/download");
    log.info("Then re-run: tokless");
    return false;
  }
  const okNode = await installNode();
  if (okNode && which("npm") && which("npx")) {
    log.ok("Node.js installed.");
    return true;
  }
  log.err("Node install didn't complete. Install manually: https://nodejs.org/en/download");
  return false;
}

// Per-OS adaptive Node installer (Node bundles npm + npx).
async function installNode(): Promise<boolean> {
  if (process.env.TOKLESS_TEST === "1") return true;
  if (process.platform === "win32") return installNodeWindows();
  return installNodeUnix();
}

async function installNodeUnix(): Promise<boolean> {
  if (!which("curl")) {
    log.err("Need curl to install Node.");
    return false;
  }
  const fnmHome = `${process.env.HOME}/.local/share/fnm`;
  if ((await run("sh", ["-c", "curl -fsSL https://fnm.vercel.app/install | bash -s -- --skip-shell"])).code !== 0) {
    return false;
  }
  const r = await run("sh", [
    "-c",
    `eval "$(${fnmHome}/fnm env --shell bash)" && ${fnmHome}/fnm install --lts && ${fnmHome}/fnm use lts-latest && echo "$PATH"`,
  ]);
  if (r.code !== 0) return false;
  if (r.stdout.trim()) process.env.PATH = r.stdout.trim();
  return !!which("node") && !!which("npm");
}

async function installNodeWindows(): Promise<boolean> {
  if (!which("winget")) {
    log.err("winget not found — install Node.js LTS from https://nodejs.org");
    return false;
  }
  const r = await run("winget", [
    "install", "-e", "--id", "OpenJS.NodeJS.LTS",
    "--accept-source-agreements", "--accept-package-agreements", "--silent",
  ]);
  return r.code === 0;
}

export async function installCargo(): Promise<boolean> {
  if (process.env.TOKLESS_TEST === "1") return true;
  const yes = await confirm("RTK needs Rust (cargo) to build for your platform. Install it now?", true);
  if (!yes) return false;
  if (process.platform === "win32") {
    const ps = [
      "$ErrorActionPreference='Stop'",
      "$u='https://win.rustup.rs/x86_64'",
      "$o=\"$env:TEMP\\rustup-init.exe\"",
      "Invoke-WebRequest -UseBasicParsing -Uri $u -OutFile $o",
      "& $o -y --default-toolchain stable --profile minimal",
    ].join("; ");
    return (await run("powershell", ["-NoProfile", "-Command", ps])).code === 0;
  }
  if (!which("curl") && !which("wget")) return false;
  const fetcher = which("curl") ? "curl --proto '=https' --tlsv1.2 -sSf" : "wget -qO-";
  const r = await run("sh", [
    "-c",
    `${fetcher} https://sh.rustup.rs | sh -s -- -y --default-toolchain stable --profile minimal --no-modify-path`,
  ]);
  if (r.code !== 0) return false;
  process.env.PATH = `${process.env.HOME}/.cargo/bin:${process.env.PATH}`;
  return !!which("cargo");
}
