package util

import (
	"fmt"
	"os"
	"strings"
)

type logger struct {
	verbose bool
	quiet   bool
}

// L is the shared logger instance.
var L = &logger{}

func SetVerbose(v bool) {
	L.verbose = v
	if v {
		L.quiet = false
	}
}

func SetQuiet(q bool) { L.quiet = q }

func (l *logger) Info(msg string) { fmt.Println(C.Cyan(Sym.Info) + " " + msg) }
func (l *logger) Ok(msg string)   { fmt.Println(C.Green(Sym.Check) + " " + msg) }
func (l *logger) Warn(msg string) { fmt.Println(C.Yellow(Sym.Warn) + " " + msg) }
func (l *logger) Err(msg string)  { fmt.Fprintln(os.Stderr, C.Red(Sym.Cross)+" "+msg) }

func (l *logger) Step(msg string) {
	if !l.quiet {
		fmt.Println("\n" + C.Bold(C.Magenta(Sym.Arrow+" "+msg)))
	}
}

func (l *logger) Sub(msg string) {
	if !l.quiet {
		fmt.Println("  " + C.Gray(Sym.Bullet) + " " + msg)
	}
}

func (l *logger) Debug(msg string) {
	if l.verbose {
		fmt.Println("  " + C.Gray("[debug] "+msg))
	}
}

func (l *logger) Raw(msg string) { fmt.Println(msg) }

func (l *logger) Banner(title, subtitle string) {
	if l.quiet {
		return
	}
	width := len(title)
	if len(subtitle) > width {
		width = len(subtitle)
	}
	line := strings.Repeat("─", width+4)
	fmt.Println("\n" + C.Cyan(line))
	fmt.Println("  " + C.Bold(C.Cyan(title)))
	if subtitle != "" {
		fmt.Println("  " + C.Gray(subtitle))
	}
	fmt.Println(C.Cyan(line) + "\n")
}
