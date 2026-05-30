// Zero-dep ANSI color helpers.

const isTTY = process.stdout.isTTY ?? false;
const noColor = process.env.NO_COLOR !== undefined || process.env.TERM === "dumb";
const enabled = isTTY && !noColor;

function wrap(open: number, close: number) {
  return (s: string) => (enabled ? `\x1b[${open}m${s}\x1b[${close}m` : s);
}

export const c = {
  reset: "\x1b[0m",
  bold: wrap(1, 22),
  dim: wrap(2, 22),
  italic: wrap(3, 23),
  underline: wrap(4, 24),
  inverse: wrap(7, 27),
  red: wrap(31, 39),
  green: wrap(32, 39),
  yellow: wrap(33, 39),
  blue: wrap(34, 39),
  magenta: wrap(35, 39),
  cyan: wrap(36, 39),
  gray: wrap(90, 39),
  bgCyan: wrap(46, 49),
  bgGreen: wrap(42, 49),
  enabled,
};

export function badge(label: string, kind: "info" | "ok" | "warn" | "err" = "info") {
  const map = {
    info: c.cyan("ℹ"),
    ok: c.green("✔"),
    warn: c.yellow("⚠"),
    err: c.red("✖"),
  };
  return `${map[kind]} ${label}`;
}

export const sym = {
  bullet: enabled ? "•" : "*",
  arrow: enabled ? "›" : ">",
  check: enabled ? "✔" : "v",
  cross: enabled ? "✖" : "x",
  warn: enabled ? "⚠" : "!",
  info: enabled ? "ℹ" : "i",
  selected: enabled ? "◉" : "(x)",
  unselected: enabled ? "◯" : "( )",
  pointer: enabled ? "❯" : ">",
};
