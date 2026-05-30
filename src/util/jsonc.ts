export function parseJsonc<T = unknown>(text: string): T {
  const stripped = stripComments(text);
  return JSON.parse(stripped) as T;
}

export function tryParseJsonc<T = unknown>(text: string): T | null {
  try {
    return parseJsonc<T>(text);
  } catch {
    return null;
  }
}

function stripComments(s: string): string {
  let out = "";
  let i = 0;
  const n = s.length;
  let inStr = false;
  let strCh = "";
  let esc = false;
  while (i < n) {
    const ch = s[i];
    if (inStr) {
      out += ch;
      if (esc) {
        esc = false;
      } else if (ch === "\\") {
        esc = true;
      } else if (ch === strCh) {
        inStr = false;
      }
      i++;
      continue;
    }
    if (ch === '"' || ch === "'") {
      inStr = true;
      strCh = ch;
      out += ch;
      i++;
      continue;
    }
    if (ch === "/" && s[i + 1] === "/") {
      while (i < n && s[i] !== "\n") i++;
      continue;
    }
    if (ch === "/" && s[i + 1] === "*") {
      i += 2;
      while (i < n && !(s[i] === "*" && s[i + 1] === "/")) i++;
      i += 2;
      continue;
    }
    out += ch;
    i++;
  }
  out = out.replace(/,(\s*[}\]])/g, "$1");
  return out;
}

export function stringifyJson(obj: unknown): string {
  return JSON.stringify(obj, null, 2) + "\n";
}

export function deepMerge<T extends Record<string, unknown>>(...sources: Partial<T>[]): T {
  const out: Record<string, unknown> = {};
  for (const src of sources) {
    if (!src) continue;
    for (const k of Object.keys(src)) {
      const v = (src as Record<string, unknown>)[k];
      if (v && typeof v === "object" && !Array.isArray(v) && out[k] && typeof out[k] === "object" && !Array.isArray(out[k])) {
        out[k] = deepMerge(out[k] as Record<string, unknown>, v as Record<string, unknown>);
      } else {
        out[k] = v;
      }
    }
  }
  return out as T;
}
