// Single-line progress renderer.
import { c, sym } from "./colors.js";

const isTTY = !!process.stdout.isTTY;
const FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

export interface Step {
  label: string;
  run: () => Promise<void> | void;
}

export class Progress {
  private idx = 0;
  private total = 0;
  private current = "";
  private timer: ReturnType<typeof setInterval> | null = null;
  private frame = 0;
  private startedAt = 0;
  private realWrite = process.stdout.write.bind(process.stdout);

  constructor(private title: string) {}

  /** Start the renderer; reserves a line. */
  start(total: number): void {
    this.total = total;
    this.idx = 0;
    this.startedAt = Date.now();
    if (this.title) console.log("\n  " + c.bold(c.cyan(this.title)));
    if (isTTY) {
      this.timer = setInterval(() => {
        this.frame = (this.frame + 1) % FRAMES.length;
        this.repaint();
      }, 80);
    }
  }

  /** Begin a sub-step; renders spinner + label until complete() or fail(). */
  begin(label: string): void {
    this.current = label;
    if (isTTY) this.repaint();
  }

  /** Mark current step as completed with a green check + optional note. */
  complete(note?: string): void {
    this.idx++;
    this.clearLine();
    const pct = this.total > 0 ? Math.round((this.idx / this.total) * 100) : 100;
    const noteStr = note ? c.gray(` ${note}`) : "";
    console.log(
      `  ${c.green(sym.check)} ${this.current.padEnd(22)} ${c.gray(`[${this.pctBar(pct)}] ${pct}%`)}${noteStr}`,
    );
  }

  /** Mark current step as failed. */
  fail(reason: string): void {
    this.idx++;
    this.clearLine();
    console.log(`  ${c.red(sym.cross)} ${this.current.padEnd(22)} ${c.red(reason)}`);
  }

  /** Mark current step as skipped (counts toward progress but neutral). */
  skip(note: string): void {
    this.idx++;
    this.clearLine();
    const pct = this.total > 0 ? Math.round((this.idx / this.total) * 100) : 100;
    console.log(
      `  ${c.gray(sym.bullet)} ${this.current.padEnd(22)} ${c.gray(`[${this.pctBar(pct)}] ${pct}%  ${note}`)}`,
    );
  }

  /** Finish the renderer. */
  done(summary?: string): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = null;
    }
    this.clearLine();
    if (summary) console.log("  " + c.gray(summary));
  }

  // --- helpers ---
  private repaint(): void {
    if (!isTTY || !this.current) return;
    const f = c.cyan(FRAMES[this.frame]);
    const pct = this.total > 0 ? Math.round((this.idx / this.total) * 100) : 0;
    const line = `  ${f} ${this.current.padEnd(22)} ${c.gray(`[${this.pctBar(pct)}] ${pct}%`)}`;
    this.realWrite("\r\x1b[2K" + line);
  }

  private clearLine(): void {
    if (isTTY) this.realWrite("\r\x1b[2K");
  }

  private pctBar(pct: number): string {
    const width = 16;
    const filled = Math.round((pct / 100) * width);
    return c.green("█".repeat(filled)) + c.gray("░".repeat(width - filled));
  }
}

/** Wrap an async fn so any chatty logs it makes are silenced for the duration. */
export async function withSilencedLogs<T>(fn: () => Promise<T> | T): Promise<T> {
  const realStdout = process.stdout.write.bind(process.stdout);
  const realStderr = process.stderr.write.bind(process.stderr);
  const buf: string[] = [];
  process.stdout.write = ((chunk: string | Buffer) => {
    buf.push(typeof chunk === "string" ? chunk : chunk.toString("utf8"));
    return true;
  }) as typeof process.stdout.write;
  process.stderr.write = process.stdout.write;
  try {
    return await fn();
  } finally {
    process.stdout.write = realStdout;
    process.stderr.write = realStderr;
  }
}
