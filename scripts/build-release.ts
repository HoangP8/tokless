#!/usr/bin/env bun
import { mkdirSync, rmSync, readFileSync } from "node:fs";
import { $ } from "bun";

const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8")) as { version: string };
const VERSION = pkg.version;

const TARGETS: Record<string, string> = {
  "tokless-linux-x64": "bun-linux-x64",
  "tokless-linux-arm64": "bun-linux-arm64",
  "tokless-darwin-x64": "bun-darwin-x64",
  "tokless-darwin-arm64": "bun-darwin-arm64",
  "tokless-windows-x64.exe": "bun-windows-x64",
};

const OUT = new URL("../dist/release/", import.meta.url).pathname;
rmSync(OUT, { recursive: true, force: true });
mkdirSync(OUT, { recursive: true });

let failed = 0;
for (const [asset, target] of Object.entries(TARGETS)) {
  const outfile = OUT + asset;
  process.stdout.write(`compiling ${asset} (${target}) … `);
  try {
    await $`bun build src/cli.ts --compile --target=${target} --define TOKLESS_VERSION=${JSON.stringify(VERSION)} --minify --outfile ${outfile}`.quiet();
    console.log("ok");
  } catch (e) {
    failed++;
    console.log("FAIL");
    console.error(String((e as { stderr?: Buffer }).stderr ?? e));
  }
}

if (failed > 0) {
  console.error(`\n${failed} target(s) failed.`);
  process.exit(1);
}
console.log(`\nBuilt ${Object.keys(TARGETS).length} binaries for v${VERSION} in dist/release/`);
