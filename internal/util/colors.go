package util

import (
	"fmt"
	"os"
)

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// colorsEnabled gates all ANSI output.
var colorsEnabled = stdoutIsTTY() && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"

func wrap(open, close int) func(string) string {
	return func(s string) string {
		if colorsEnabled {
			return fmt.Sprintf("\x1b[%dm%s\x1b[%dm", open, s, close)
		}
		return s
	}
}

// Colors holds ANSI styling functions.
type Colors struct {
	Bold, Dim, Italic, Underline, Inverse         func(string) string
	Red, Green, Yellow, Blue, Magenta, Cyan, Gray func(string) string
	BgCyan, BgGreen                               func(string) string
}

var C = Colors{
	Bold:      wrap(1, 22),
	Dim:       wrap(2, 22),
	Italic:    wrap(3, 23),
	Underline: wrap(4, 24),
	Inverse:   wrap(7, 27),
	Red:       wrap(31, 39),
	Green:     wrap(32, 39),
	Yellow:    wrap(33, 39),
	Blue:      wrap(34, 39),
	Magenta:   wrap(35, 39),
	Cyan:      wrap(36, 39),
	Gray:      wrap(90, 39),
	BgCyan:    wrap(46, 49),
	BgGreen:   wrap(42, 49),
}

// Symbols carries unicode glyphs with ascii fallbacks.
type Symbols struct {
	Bullet, Arrow, Check, Cross, Warn, Info, Selected, Unselected, Pointer string
}

func pick(uni, ascii string) string {
	if colorsEnabled {
		return uni
	}
	return ascii
}

var Sym = Symbols{
	Bullet:     pick("•", "*"),
	Arrow:      pick("›", ">"),
	Check:      pick("✔", "v"),
	Cross:      pick("✖", "x"),
	Warn:       pick("⚠", "!"),
	Info:       pick("ℹ", "i"),
	Selected:   pick("◉", "(x)"),
	Unselected: pick("◯", "( )"),
	Pointer:    pick("❯", ">"),
}
