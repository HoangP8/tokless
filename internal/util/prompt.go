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

type SelectOption struct {
	Value    string
	Label    string
	Hint     string
	Selected bool
}

func stdinIsTTY() bool { return isTerminal(os.Stdin.Fd()) }

// IsInteractive reports whether we can run an interactive prompt (stdin is a TTY).
func IsInteractive() bool { return stdinIsTTY() }

func promptExit(restore func()) {
	restore()
	fmt.Fprint(os.Stdout, "\r\n")
	RestoreConsoleCP()
	os.Exit(130)
}

func promptSuspend(restore *func(), render func()) {
	next, ok := suspendTTY((*restore))
	if !ok {
		return
	}
	*restore = next
	render()
}

func SelectOne(question string, options []SelectOption) string {
	if len(options) == 0 {
		return ""
	}
	selected := 0
	for i, o := range options {
		if o.Selected {
			selected = i
			break
		}
	}
	if !interactiveTTY() {
		return options[selected].Value
	}
	restore, ok := rawMode()
	if !ok || !vtReady {
		if ok {
			restore()
		}
		return selectOneLine(question, options, selected)
	}
	defer func() { restore() }()
	reader := bufio.NewReader(os.Stdin)
	cursor := selected
	first := true
	dot := pick("●", "*")
	divider := pick("─", "-")
	dividerW := 60
	ptr := pick("❯", ">")
	keyArrow := C.Orange("↑/↓") + C.Dim(" move · ") + C.Orange("<enter>") + C.Dim(" confirm")
	render := func() {
		if !first {
			fmt.Fprintf(os.Stdout, "\x1b[%dA", len(options)+3)
		}
		first = false
		fmt.Fprint(os.Stdout, "\x1b[0J")
		fmt.Fprintln(os.Stdout, C.Magenta(C.Bold(dot))+" "+C.Magenta(C.Bold(question)))
		for i, o := range options {
			mark := " "
			if i == cursor {
				mark = C.Cyan(C.Bold(ptr))
			}
			box := C.Gray(Sym.Unselected)
			label := o.Label
			if i == cursor {
				box = C.Green(Sym.Selected)
				label = C.Cyan(C.Bold(o.Label))
			}
			extra := ""
			if o.Hint != "" {
				extra = "  " + C.Dim(o.Hint)
			}
			fmt.Fprintln(os.Stdout, C.Dim("│   ")+mark+" "+box+"  "+label+extra)
		}
		fmt.Fprintln(os.Stdout, C.Dim("  "+strings.Repeat(divider, dividerW)))
		fmt.Fprintln(os.Stdout, "  "+keyArrow)
	}
	render()
	for {
		ch, err := reader.ReadByte()
		if err != nil {
			return options[cursor].Value
		}
		switch ch {
		case 3:
			promptExit(restore)
		case 26:
			promptSuspend(&restore, render)
		case 27:
			b1, _ := reader.ReadByte()
			b2, _ := reader.ReadByte()
			if b1 == '[' && b2 == 'A' {
				cursor = (cursor + len(options) - 1) % len(options)
				render()
			} else if b1 == '[' && b2 == 'B' {
				cursor = (cursor + 1) % len(options)
				render()
			}
		case ' ', '\r', '\n':
			restore()
			settleSelectOne(question, options, cursor)
			return options[cursor].Value
		}
	}
}

// settleSelectOne rewrites the active SelectOne block as a static tree node.
func settleSelectOne(question string, options []SelectOption, chosen int) {
	height := len(options) + 3
	fmt.Fprintf(os.Stdout, "\x1b[%dA\x1b[0J", height)
	corner := pick("├─ ", "+- ")
	stem := C.Dim(pick("│   ", "|   "))
	bul := pick("│", "|")
	fmt.Fprintf(os.Stdout, "%s%s\n", C.Dim(corner), C.Bold(question))
	for i, o := range options {
		mark := C.Gray(Sym.Unselected)
		label := o.Label
		if i == chosen {
			mark = C.Green(Sym.Selected)
			label = C.Green(C.Bold(o.Label))
		}
		extra := ""
		if o.Hint != "" {
			extra = "  " + C.Dim(o.Hint)
		}
		fmt.Fprintf(os.Stdout, "%s%s  %s%s\n", stem, mark, label, extra)
	}
	fmt.Fprintln(os.Stdout, C.Dim(bul))
}

// settleMultiSelect rewrites the active MultiSelect block as a static tree node.
func settleMultiSelect(question string, items []MultiSelectOption) {
	height := len(items) + 3
	fmt.Fprintf(os.Stdout, "\x1b[%dA\x1b[0J", height)
	corner := pick("├─ ", "+- ")
	stem := C.Dim(pick("│   ", "|   "))
	bul := pick("│", "|")

	labelW := 0
	for _, it := range items {
		if n := utf8.RuneCountInString(it.Label); n > labelW {
			labelW = n
		}
	}

	fmt.Fprintf(os.Stdout, "%s%s\n", C.Dim(corner), C.Bold(question))
	for _, it := range items {
		pad := strings.Repeat(" ", labelW-utf8.RuneCountInString(it.Label))
		var box, label, tag, extra string
		if it.Disabled {
			box = C.Dim(Sym.Disabled)
			label = C.Dim(it.Label)
			tag = C.Yellow("[MISSING]")
			if it.Hint != "" {
				extra = "  " + C.Dim(it.Hint)
			}
		} else if it.Selected {
			box = C.Green(Sym.Selected)
			label = C.Green(C.Bold(it.Label))
			tag = C.Green(C.Bold("✓ ")) + C.Green("[READY]") + "  "
			if it.Hint != "" {
				extra = "  " + C.Dim(it.Hint)
			}
		} else {
			box = C.Gray(Sym.Unselected)
			label = it.Label
			tag = C.Green("[READY]") + "  "
			if it.Hint != "" {
				extra = "  " + C.Dim(it.Hint)
			}
		}
		fmt.Fprintf(os.Stdout, "%s%s  %s%s  %s%s\n", stem, box, label, pad, tag, extra)
	}
	fmt.Fprintln(os.Stdout, C.Dim(bul))
}

func selectOneLine(question string, options []SelectOption, selected int) string {
	fmt.Fprintln(os.Stdout, C.Bold(C.Cyan("?"))+" "+C.Bold(question))
	for i, o := range options {
		mark := " "
		if i == selected {
			mark = "*"
		}
		extra := ""
		if o.Hint != "" {
			extra = "  " + C.Dim(o.Hint)
		}
		fmt.Fprintf(os.Stdout, "  %d) %s%s%s\n", i+1, mark, o.Label, extra)
	}
	fmt.Fprint(os.Stdout, "  "+C.Gray("Enter = "+options[selected].Label+": "))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(options) {
		return options[n-1].Value
	}
	return options[selected].Value
}

// MultiSelect renders an interactive checklist; non-TTY returns enabled defaults.
func MultiSelect(question string, options []MultiSelectOption) []string {
	if !interactiveTTY() {
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
	defer func() { restore() }()

	reader := bufio.NewReader(os.Stdin)
	firstRender := true

	const headerLines = 3 // root + divider + keybinds

	labelW := 0
	for _, it := range items {
		if n := utf8.RuneCountInString(it.Label); n > labelW {
			labelW = n
		}
	}

	dot := pick("●", "*")
	divider := pick("─", "-")
	dividerW := 60
	ptr := pick("❯", ">")
	checkMark := "✓"
	tagPad := "  "
	keyHint := "  " +
		C.Orange("↑/↓") + C.Dim(" move · ") +
		C.Orange("<space>") + C.Dim(" select · ") +
		C.Orange("<a>") + C.Dim(" all · ") +
		C.Orange("<enter>") + C.Dim(" confirm")

	render := func() {
		if !firstRender {
			fmt.Fprintf(os.Stdout, "\x1b[%dA", len(items)+headerLines)
		}
		firstRender = false
		fmt.Fprint(os.Stdout, "\x1b[0J")
		var b strings.Builder

		b.WriteString(C.Magenta(C.Bold(dot)) + " " + C.Magenta(C.Bold(question)) + "\r\n")

		for i, it := range items {
			pad := strings.Repeat(" ", labelW-utf8.RuneCountInString(it.Label))

			active := i == cursor && !it.Disabled

			ptrCol := "  "
			if active {
				ptrCol = C.Cyan(C.Bold(ptr)) + " "
			}

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
					label = C.Green(C.Bold(it.Label))
					tag = C.Green(C.Bold(checkMark+" ")) + C.Green("[READY]") + "  "
				} else {
					box = C.Gray(Sym.Unselected)
					label = it.Label
					tag = tagPad + C.Green("[READY]") + "  "
				}
				if it.Hint != "" {
					extra = "  " + C.Dim(it.Hint)
				}
			}

			if active {
				label = C.Cyan(C.Bold(it.Label))
			}

			b.WriteString(C.Dim("│   ") + ptrCol + box + "  " + label + pad + "  " + tag + extra + "\r\n")
		}
		b.WriteString(C.Dim("  "+strings.Repeat(divider, dividerW)) + "\r\n")
		b.WriteString(keyHint + "\r\n")
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
			promptExit(restore)
		case 26: // ctrl-z
			promptSuspend(&restore, render)
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
			settleMultiSelect(question, items)
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
	if !interactiveTTY() {
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
