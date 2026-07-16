package util

import (
	"fmt"
	"os"
	"strings"
)

func stdoutIsTTY() bool { return isTerminal(os.Stdout.Fd()) }

// StdoutIsTTY reports whether stdout is a real terminal.
func StdoutIsTTY() bool { return stdoutIsTTY() }
func StdoutANSI() bool  { return stdoutIsTTY() && vtReady }

var vtReady = enableVT()

func runningInsideClaudeCode() bool {
	return os.Getenv("CLAUDECODE") == "1" ||
		os.Getenv("CLAUDE_CODE_CHILD_SESSION") == "1"
}

func RunningInsideClaudeCode() bool { return runningInsideClaudeCode() }

func interactiveTTY() bool { return stdinIsTTY() && stdoutIsTTY() && !runningInsideClaudeCode() }

// colorsEnabled gates all ANSI output.
var colorsEnabled = stdoutIsTTY() && vtReady && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"

func wrap(open, close int) func(string) string {
	return func(s string) string {
		if colorsEnabled {
			return fmt.Sprintf("\x1b[%dm%s\x1b[%dm", open, s, close)
		}
		return s
	}
}

// wrapFg256 styles with a 256-color foreground.
func wrapFg256(n int) func(string) string {
	return func(s string) string {
		if colorsEnabled {
			return fmt.Sprintf("\x1b[38;5;%dm%s\x1b[39m", n, s)
		}
		return s
	}
}

// Colors holds ANSI styling functions.
type Colors struct {
	Bold, Dim, Italic, Underline, Inverse         func(string) string
	Red, Green, Yellow, Blue, Magenta, Cyan, Gray func(string) string
	Orange                                        func(string) string
}

var C = Colors{
	Bold:      wrap(1, 22),
	Dim:       wrap(2, 22),
	Italic:    wrap(3, 23),
	Underline: wrap(4, 24),
	Red:       wrap(31, 39),
	Green:     wrap(32, 39),
	Yellow:    wrap(33, 39),
	Blue:      wrap(34, 39),
	Magenta:   wrap(35, 39),
	Cyan:      wrap(36, 39),
	Gray:      wrap(90, 39),
	Orange:    wrapFg256(208),
}

// Symbols carries unicode glyphs with ascii fallbacks.
type Symbols struct {
	Bullet, Arrow, Check, Cross, Warn, Info, Selected, Unselected, Disabled, Pointer string
}

// legacyWinConsole reports a plain conhost window (classic PowerShell/cmd)
func legacyWinConsole() bool {
	if !IsWin {
		return false
	}
	return os.Getenv("WT_SESSION") == "" &&
		os.Getenv("TERM_PROGRAM") == "" &&
		os.Getenv("ConEmuANSI") != "ON" &&
		os.Getenv("TERM") == ""
}

// glyphsEnabled gates unicode glyphs.
var glyphsEnabled = colorsEnabled && !legacyWinConsole()

func pick(uni, ascii string) string {
	if glyphsEnabled {
		return uni
	}
	return ascii
}

func Rule(width int) string { return strings.Repeat(pick("─", "-"), width) }

var Sym = Symbols{
	Bullet:     pick("•", "*"),
	Arrow:      pick("›", ">"),
	Check:      pick("✔", "v"),
	Cross:      pick("✖", "x"),
	Warn:       pick("⚠", "!"),
	Info:       pick("ℹ", "i"),
	Selected:   pick("◉", "(x)"),
	Unselected: pick("◯", "( )"),
	Disabled:   pick("·", " - "),
	Pointer:    pick("❯", ">"),
}
