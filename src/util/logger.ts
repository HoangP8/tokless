import { c, sym } from "./colors.js";
let verbose = false;
let quiet = false;

export function setVerbose(v: boolean) {
  verbose = v;
  if (v) quiet = false;
}
export function setQuiet(q: boolean) {
  quiet = q;
}

export const log = {
  info: (msg: string) => console.log(`${c.cyan(sym.info)} ${msg}`),
  ok: (msg: string) => console.log(`${c.green(sym.check)} ${msg}`),
  warn: (msg: string) => console.log(`${c.yellow(sym.warn)} ${msg}`),
  err: (msg: string) => console.error(`${c.red(sym.cross)} ${msg}`),
  step: (msg: string) => {
    if (!quiet) console.log(`\n${c.bold(c.magenta(sym.arrow + " " + msg))}`);
  },
  sub: (msg: string) => {
    if (!quiet) console.log(`  ${c.gray(sym.bullet)} ${msg}`);
  },
  debug: (msg: string) => {
    if (verbose) console.log(`  ${c.gray("[debug] " + msg)}`);
  },
  raw: (msg: string) => console.log(msg),
  banner: (title: string, subtitle?: string) => {
    if (quiet) return;
    const line = "─".repeat(Math.max(title.length, subtitle?.length ?? 0) + 4);
    console.log("\n" + c.cyan(line));
    console.log("  " + c.bold(c.cyan(title)));
    if (subtitle) console.log("  " + c.gray(subtitle));
    console.log(c.cyan(line) + "\n");
  },
};
