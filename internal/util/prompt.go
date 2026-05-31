package util

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// rawMode toggles terminal raw mode via stty; returns a restore func.
func rawMode() (func(), bool) {
	if IsWin {
		return func() {}, false
	}
	saved, err := exec.Command("stty", "-F", "/dev/tty", "-g").Output()
	if err != nil {
		c := exec.Command("stty", "-g")
		c.Stdin = os.Stdin
		saved, err = c.Output()
		if err != nil {
			return func() {}, false
		}
	}
	set := exec.Command("stty", "-F", "/dev/tty", "raw", "-echo")
	if set.Run() != nil {
		c := exec.Command("stty", "raw", "-echo")
		c.Stdin = os.Stdin
		if c.Run() != nil {
			return func() {}, false
		}
	}
	restore := func() {
		s := strings.TrimSpace(string(saved))
		r := exec.Command("stty", "-F", "/dev/tty", s)
		if r.Run() != nil {
			c := exec.Command("stty", s)
			c.Stdin = os.Stdin
			_ = c.Run()
		}
	}
	return restore, true
}

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

	restore, ok := rawMode()
	if !ok {
		var out []string
		for _, o := range options {
			if !o.Disabled && o.Selected {
				out = append(out, o.Value)
			}
		}
		return out
	}
	defer restore()

	reader := bufio.NewReader(os.Stdin)
	firstRender := true

	render := func() {
		if !firstRender {
			fmt.Fprintf(os.Stdout, "\x1b[%dA", len(items)+1)
		}
		firstRender = false
		fmt.Fprint(os.Stdout, "\x1b[0J")
		var b strings.Builder
		b.WriteString(C.Bold(C.Cyan("?")) + " " + C.Bold(question) + " " +
			C.Gray("(<space> toggle, <a> all, <enter> confirm)") + "\r\n")
		for i, it := range items {
			pointer := " "
			if i == cursor {
				pointer = C.Cyan(Sym.Pointer)
			}
			box := C.Gray(Sym.Unselected)
			if it.Selected {
				box = C.Green(Sym.Selected)
			}
			label := it.Label
			if it.Disabled {
				reason := it.DisabledReason
				if reason == "" {
					reason = "unavailable"
				}
				label = C.Gray(it.Label + " (" + reason + ")")
			} else if it.Selected {
				label = C.Bold(C.Cyan(it.Label))
			}
			hint := ""
			if it.Hint != "" {
				hint = C.Gray(" — " + it.Hint)
			}
			b.WriteString("  " + pointer + " " + box + " " + label + hint + "\r\n")
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
