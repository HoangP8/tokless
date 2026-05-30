import * as readline from "node:readline";
import { c, sym } from "./colors.js";

// Minimal zero-dep interactive prompts (multi-select + confirm).

export interface MultiSelectOption<T = string> {
  value: T;
  label: string;
  hint?: string;
  selected?: boolean;
  disabled?: boolean;
  disabledReason?: string;
}

export async function multiSelect<T = string>(
  question: string,
  options: MultiSelectOption<T>[],
): Promise<T[]> {
  if (!process.stdin.isTTY) {
    return options.filter((o) => !o.disabled && o.selected !== false).map((o) => o.value);
  }

  const items = options.map((o) => ({ ...o, selected: !!o.selected }));
  let cursor = 0;

  return await new Promise<T[]>((resolve) => {
    const stdin = process.stdin;
    const stdout = process.stdout;
    readline.emitKeypressEvents(stdin);
    stdin.setRawMode?.(true);
    stdin.resume();

    let firstRender = true;

    const render = () => {
      const lines: string[] = [];
      lines.push(`${c.bold(c.cyan("?"))} ${c.bold(question)} ${c.gray("(<space> toggle, <a> all, <enter> confirm)")}`);
      items.forEach((it, i) => {
        const pointer = i === cursor ? c.cyan(sym.pointer) : " ";
        const box = it.selected ? c.green(sym.selected) : c.gray(sym.unselected);
        const label = it.disabled
          ? c.gray(`${it.label} ${it.disabledReason ? `(${it.disabledReason})` : "(unavailable)"}`)
          : it.selected
            ? c.bold(c.cyan(it.label))
            : it.label;
        const hint = it.hint ? c.gray(` — ${it.hint}`) : "";
        lines.push(`  ${pointer} ${box} ${label}${hint}`);
      });

      if (!firstRender) {
        readline.moveCursor(stdout, 0, -(items.length + 1));
      }
      firstRender = false;
      readline.clearScreenDown(stdout);
      stdout.write(lines.join("\n") + "\n");
    };

    const cleanup = () => {
      stdin.setRawMode?.(false);
      stdin.pause();
      stdin.removeListener("keypress", onKey);
    };

    const onKey = (_str: string, key: readline.Key) => {
      if (!key) return;
      if (key.ctrl && key.name === "c") {
        cleanup();
        stdout.write("\n");
        process.exit(130);
      }
      if (key.name === "up" || key.name === "k") {
        do {
          cursor = (cursor - 1 + items.length) % items.length;
        } while (items[cursor].disabled);
        render();
      } else if (key.name === "down" || key.name === "j") {
        do {
          cursor = (cursor + 1) % items.length;
        } while (items[cursor].disabled);
        render();
      } else if (key.name === "space") {
        if (!items[cursor].disabled) items[cursor].selected = !items[cursor].selected;
        render();
      } else if (key.name === "a") {
        const allOn = items.filter((i) => !i.disabled).every((i) => i.selected);
        items.forEach((i) => {
          if (!i.disabled) i.selected = !allOn;
        });
        render();
      } else if (key.name === "return") {
        cleanup();
        stdout.write("\n");
        resolve(items.filter((i) => i.selected && !i.disabled).map((i) => i.value));
      }
    };

    while (items[cursor] && items[cursor].disabled) cursor++;
    render();
    stdin.on("keypress", onKey);
  });
}

export async function confirm(question: string, defaultYes = true): Promise<boolean> {
  if (!process.stdin.isTTY) return defaultYes;
  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
  const hint = defaultYes ? "[Y/n]" : "[y/N]";
  const answer: string = await new Promise((resolve) =>
    rl.question(`${c.cyan("?")} ${c.bold(question)} ${c.gray(hint)} `, (a) => {
      rl.close();
      resolve(a);
    }),
  );
  const a = answer.trim().toLowerCase();
  if (!a) return defaultYes;
  return a === "y" || a === "yes";
}
