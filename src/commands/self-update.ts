import { log } from "../util/logger.js";
import { run, which } from "../util/exec.js";
import { c } from "../util/colors.js";
import * as https from "node:https";
import { toklessVersion } from "../util/version.js";

// Self-update for the curl-installed binary: compare the embedded build version
// against the latest GitHub Release tag, then re-run the official installer.
const OWNER = "HoangP8";
const REPO = "tokless";
const INSTALL_SH = `https://raw.githubusercontent.com/${OWNER}/${REPO}/main/scripts/install.sh`;
const INSTALL_PS1 = `https://raw.githubusercontent.com/${OWNER}/${REPO}/main/scripts/install.ps1`;

export async function runSelfUpdate(): Promise<number> {
  log.banner("tokless self-update", "Update the tokless CLI itself");
  const local = toklessVersion();
  log.sub(`local: ${local}`);
  const latest = await fetchLatestReleaseTag();
  if (!latest) {
    log.err("Could not reach GitHub Releases. Try again later.");
    log.warn("Manual update:");
    log.raw("  " + c.cyan(`curl -fsSL ${INSTALL_SH} | bash`));
    return 1;
  }
  log.sub(`latest: ${latest}`);
  if (semverGte(local, latest)) {
    log.ok("Already up to date.");
    return 0;
  }
  log.step(`Updating ${local} → ${latest}…`);
  if (process.platform === "win32") {
    log.warn("On Windows, run:");
    log.raw("  " + c.cyan(`irm ${INSTALL_PS1} | iex`));
    return 0;
  }
  if (which("curl") && which("bash")) {
    const r = await run("bash", ["-c", `curl -fsSL ${INSTALL_SH} | bash`]);
    if (r.code === 0) {
      log.ok(`Updated to ${latest}. Restart your shell if needed.`);
      return 0;
    }
    log.err("Auto-update failed.");
  }
  log.warn("Manual update:");
  log.raw("  " + c.cyan(`curl -fsSL ${INSTALL_SH} | bash`));
  return 0;
}

// GitHub's releases/latest API redirects to the newest tag.
function fetchLatestReleaseTag(): Promise<string | null> {
  return new Promise((resolve) => {
    const req = https.get(
      `https://api.github.com/repos/${OWNER}/${REPO}/releases/latest`,
      { timeout: 5000, headers: { "User-Agent": "tokless-self-update", Accept: "application/vnd.github+json" } },
      (res) => {
        let data = "";
        res.on("data", (d) => (data += d));
        res.on("end", () => {
          try {
            const j = JSON.parse(data) as { tag_name?: string };
            resolve(j.tag_name ? j.tag_name.replace(/^v/, "") : null);
          } catch {
            resolve(null);
          }
        });
      },
    );
    req.on("error", () => resolve(null));
    req.on("timeout", () => {
      req.destroy();
      resolve(null);
    });
  });
}

function semverGte(a: string, b: string): boolean {
  const pa = a.split("-")[0].split(".").map((n) => parseInt(n, 10) || 0);
  const pb = b.split("-")[0].split(".").map((n) => parseInt(n, 10) || 0);
  for (let i = 0; i < 3; i++) {
    if ((pa[i] ?? 0) > (pb[i] ?? 0)) return true;
    if ((pa[i] ?? 0) < (pb[i] ?? 0)) return false;
  }
  return true;
}
