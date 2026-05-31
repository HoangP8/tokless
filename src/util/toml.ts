export interface TomlBlock {
  header: string;
  fields: Record<string, TomlValue>;
}

export type TomlValue = string | number | boolean | string[] | Record<string, string>;

function escapeStr(s: string): string {
  return s.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n");
}

function fmtValue(v: TomlValue): string {
  if (typeof v === "string") return `"${escapeStr(v)}"`;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (Array.isArray(v)) return "[" + v.map((x) => `"${escapeStr(x)}"`).join(", ") + "]";
  const entries = Object.entries(v).map(([k, val]) => `${k} = "${escapeStr(val)}"`);
  return "{ " + entries.join(", ") + " }";
}

export function renderBlock(block: TomlBlock): string {
  const lines: string[] = [`[${block.header}]`];
  for (const [k, v] of Object.entries(block.fields)) {
    if (v === undefined || v === null) continue;
    if (k === "env" && typeof v === "object" && !Array.isArray(v)) {
      continue;
    }
    lines.push(`${k} = ${fmtValue(v)}`);
  }
  if ("env" in block.fields && typeof block.fields.env === "object" && !Array.isArray(block.fields.env)) {
    lines.push("");
    lines.push(`[${block.header}.env]`);
    for (const [k, v] of Object.entries(block.fields.env as Record<string, string>)) {
      lines.push(`${k} = "${escapeStr(v)}"`);
    }
  }
  return lines.join("\n") + "\n";
}

// Find the slice [start,end) of an existing [header] block in source.
function findBlockRange(src: string, header: string): { start: number; end: number } | null {
  const reHeader = new RegExp(`^\\[\\s*${escapeRe(header)}\\s*\\]\\s*$`, "m");
  const m = reHeader.exec(src);
  if (!m) return null;
  const start = m.index;
  const reNext = /^\[(?!\s*$)([^\]]+)\]\s*$/gm;
  reNext.lastIndex = start + m[0].length;
  let next: RegExpExecArray | null;
  while ((next = reNext.exec(src))) {
    const candidate = next[1].trim();
    if (candidate === header) continue;
    if (candidate.startsWith(header + ".")) continue;
    return { start, end: next.index };
  }
  return { start, end: src.length };
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function upsertBlock(src: string, block: TomlBlock, opts?: { merge?: boolean }): string {
  let effective = block;
  if (opts?.merge) {
    const range = findBlockRange(src, block.header);
    if (range) {
      const existing = parseScalarFields(src.slice(range.start, range.end));
      effective = { header: block.header, fields: { ...existing, ...block.fields } };
    }
  }
  const rendered = renderBlock(effective);
  const range = findBlockRange(src, block.header);
  if (!range) {
    const sep = src.length === 0 || src.endsWith("\n\n") ? "" : src.endsWith("\n") ? "\n" : "\n\n";
    return src + sep + rendered;
  }
  const before = src.slice(0, range.start);
  const after = src.slice(range.end);
  const beforeNorm = before.endsWith("\n") ? before : before + "\n";
  return beforeNorm + rendered + (after.startsWith("\n") ? after : after === "" ? "" : "\n" + after);
}

function parseScalarFields(blockText: string): Record<string, TomlValue> {
  const out: Record<string, TomlValue> = {};
  const lines = blockText.split("\n");
  for (const line of lines) {
    const m = /^\s*([A-Za-z0-9_-]+)\s*=\s*(.+?)\s*$/.exec(line);
    if (!m) continue;
    const key = m[1];
    const raw = m[2];
    if (raw.startsWith("[") || raw.startsWith("{")) continue; // skip arrays/inline tables
    if (raw === "true" || raw === "false") out[key] = raw === "true";
    else if (/^-?\d+(\.\d+)?$/.test(raw)) out[key] = Number(raw);
    else if (raw.startsWith('"') && raw.endsWith('"')) out[key] = raw.slice(1, -1);
  }
  return out;
}

export function removeBlock(src: string, header: string): string {
  const range = findBlockRange(src, header);
  if (!range) return src;
  return src.slice(0, range.start) + src.slice(range.end);
}

export function hasBlock(src: string, header: string): boolean {
  return findBlockRange(src, header) !== null;
}
