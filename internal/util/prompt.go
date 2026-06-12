package util

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MultiSelectOption is one selectable row.
type MultiSelectOption struct {
	Value          string
	Label          string
	Hint           string
	Selected       bool
	Disabled       bool
	DisabledReason string
}

func stdinIsTTY() bool { return isTerminal(os.Stdin.Fd()) }

// IsInteractive reports whether we can run an interactive prompt (stdin is a TTY).
func IsInteractive() bool { return stdinIsTTY() }

// MultiSelect renders an interactive checklist; non-TTY returns enabled defaults.
func MultiSelect(question string, options []MultiSelectOption) []string {
	if !stdinIsTTY() {
		var out []string
		for _, o := range options {
			if !o.Disabled && o.Selected {
				out = append(out, o.Value)
			}
		}
		return out
	}

	items := make([]MultiSelectOption, len(options))
	copy(items, options)
	cursor := 0
	for cursor < len(items) && items[cursor].Disabled {
		cursor++
	}
	if cursor == len(items) {
		return multiSelectLine(question, items)
	}

	// The full-screen picker needs both raw key input and ANSI redraws.
	restore, ok := rawMode()
	if !ok || !vtReady {
		if ok {
			restore()
		}
		return multiSelectLine(question, items)
	}
	defer restore()

	reader := bufio.NewReader(os.Stdin)
	firstRender := true

	const headerLines = 2

	labelW := 0
	for _, it := range items {
		if n := utf8.RuneCountInString(it.Label); n > labelW {
			labelW = n
		}
	}

	render := func() {
		if !firstRender {
			fmt.Fprintf(os.Stdout, "\x1b[%dA", len(items)+headerLines)
		}
		firstRender = false
		fmt.Fprint(os.Stdout, "\x1b[0J")
		var b strings.Builder

		b.WriteString(C.Bold(C.Cyan("?")) + " " + C.Bold(question) + "\r\n")
		b.WriteString("  " + C.Dim("↑/↓ move · <space> select · <a> all · <enter> confirm") + "\r\n")

		for i, it := range items {
			pointer := " "
			if i == cursor {
				pointer = C.Cyan(Sym.Pointer)
			}
			pad := strings.Repeat(" ", labelW-utf8.RuneCountInString(it.Label))

			var box, label, tag, extra string
			if it.Disabled {
				box = C.Dim(Sym.Disabled)
				label = C.Dim(it.Label)
				tag = C.Yellow("[MISSING]")
				if it.Hint != "" {
					extra = "  " + C.Dim(it.Hint)
				}
			} else {
				if it.Selected {
					box = C.Green(Sym.Selected)
					label = C.Bold(it.Label)
				} else {
					box = C.Gray(Sym.Unselected)
					label = it.Label
				}
				tag = C.Green("[READY]") + "  "
				if it.Hint != "" {
					extra = "  " + C.Dim(it.Hint)
				}
			}

			b.WriteString(" " + pointer + " " + box + "  " + label + pad + "  " + tag + extra + "\r\n")
		}
		fmt.Fprint(os.Stdout, b.String())
	}

	render()

	for {
		ch, err := reader.ReadByte()
		if err != nil {
			break
		}
		switch ch {
		case 3: // ctrl-c
			restore()
			fmt.Fprint(os.Stdout, "\r\n")
			RestoreConsoleCP()
			os.Exit(130)
		case 27: // escape sequence (arrow keys)
			b1, _ := reader.ReadByte()
			if b1 == '[' {
				b2, _ := reader.ReadByte()
				if b2 == 'A' {
					moveCursor(&cursor, items, -1)
					render()
				} else if b2 == 'B' {
					moveCursor(&cursor, items, 1)
					render()
				}
			}
		case 'k':
			moveCursor(&cursor, items, -1)
			render()
		case 'j':
			moveCursor(&cursor, items, 1)
			render()
		case ' ':
			if !items[cursor].Disabled {
				items[cursor].Selected = !items[cursor].Selected
			}
			render()
		case 'a':
			allOn := true
			for _, it := range items {
				if !it.Disabled && !it.Selected {
					allOn = false
					break
				}
			}
			for i := range items {
				if !items[i].Disabled {
					items[i].Selected = !allOn
				}
			}
			render()
		case '\r', '\n':
			restore()
			fmt.Fprint(os.Stdout, "\r\n")
			var out []string
			for _, it := range items {
				if it.Selected && !it.Disabled {
					out = append(out, it.Value)
				}
			}
			return out
		}
	}
	return nil
}

func moveCursor(cursor *int, items []MultiSelectOption, delta int) {
	n := len(items)
	for {
		*cursor = (*cursor + delta + n) % n
		if !items[*cursor].Disabled {
			return
		}
	}
}

// multiSelectLine is the cooked-mode fallback picker: numbered list, read a
// line. Enter keeps the preselected defaults; "a" selects everything enabled.
func multiSelectLine(question string, items []MultiSelectOption) []string {
	fmt.Fprintln(os.Stdout, C.Bold(C.Cyan("?"))+" "+C.Bold(question))
	labelW := 0
	for _, it := range items {
		if n := utf8.RuneCountInString(it.Label); n > labelW {
			labelW = n
		}
	}
	var defaults []string
	num := 0
	numByIdx := make([]int, len(items))
	for i, it := range items {
		pad := strings.Repeat(" ", labelW-utf8.RuneCountInString(it.Label))
		if it.Disabled {
			numByIdx[i] = 0
			line := "       " + C.Dim(it.Label) + pad + "  " + C.Yellow("[MISSING]")
			if it.Hint != "" {
				line += "  " + C.Dim(it.Hint)
			}
			fmt.Fprintln(os.Stdout, line)
			continue
		}
		num++
		numByIdx[i] = num
		mark := " "
		if it.Selected {
			mark = "*"
			defaults = append(defaults, it.Value)
		}
		line := fmt.Sprintf("  %2d) %s%s%s  %s", num, mark, it.Label, pad, C.Green("[READY]")+"  ")
		if it.Hint != "" {
			line += "  " + C.Dim(it.Hint)
		}
		fmt.Fprintln(os.Stdout, line)
	}
	if num == 0 {
		return nil
	}
	hint := "none"
	if len(defaults) > 0 {
		hint = "keep *"
	}
	fmt.Fprint(os.Stdout, "  "+C.Gray("Numbers comma-separated, 'a' = all, Enter = "+hint+": "))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return parseLineSelection(line, items, numByIdx, defaults)
}

// parseLineSelection resolves a typed selection ("", "a", "1,3") to values.
func parseLineSelection(line string, items []MultiSelectOption, numByIdx []int, defaults []string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return defaults
	}
	var out []string
	if strings.EqualFold(line, "a") {
		for _, it := range items {
			if !it.Disabled {
				out = append(out, it.Value)
			}
		}
		return out
	}
	want := map[int]bool{}
	for _, tok := range strings.Split(line, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(tok)); err == nil {
			want[n] = true
		}
	}
	for i, it := range items {
		if numByIdx[i] > 0 && want[numByIdx[i]] {
			out = append(out, it.Value)
		}
	}
	return out
}

// Confirm prompts yes/no; non-TTY returns defaultYes.
func Confirm(question string, defaultYes bool) bool {
	if !stdinIsTTY() {
		return defaultYes
	}
	hint := "[Y/n]"
	if !defaultYes {
		hint = "[y/N]"
	}
	fmt.Fprint(os.Stdout, C.Cyan("?")+" "+C.Bold(question)+" "+C.Gray(hint)+" ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	if a == "" {
		return defaultYes
	}
	return a == "y" || a == "yes"
}
