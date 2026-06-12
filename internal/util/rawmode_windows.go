//go:build windows

package util

import (
	"os"
	"unsafe"
)

// Windows console mode flags.
const (
	winEnableProcessedInput = 0x0001
	winEnableLineInput      = 0x0002
	winEnableEchoInput      = 0x0004
	winEnableWindowInput    = 0x0008
	winEnableMouseInput     = 0x0010
	winEnableVTInput        = 0x0200
	winEnableVTProcessing   = 0x0004
)

var (
	procSetConsoleMode     = kernel32.NewProc("SetConsoleMode")
	procGetConsoleOutputCP = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

var savedOutputCP uintptr

func RestoreConsoleCP() {
	if savedOutputCP != 0 && savedOutputCP != 65001 {
		_, _, _ = procSetConsoleOutputCP.Call(savedOutputCP)
	}
}

func getConsoleMode(fd uintptr) (uint32, bool) {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(fd, uintptr(unsafe.Pointer(&mode)))
	return mode, r != 0
}

func setConsoleMode(fd uintptr, mode uint32) bool {
	r, _, _ := procSetConsoleMode.Call(fd, uintptr(mode))
	return r != 0
}

// enableVT turns on ANSI escape processing for stdout/stderr consoles.
func enableVT() bool {
	if cp, _, _ := procGetConsoleOutputCP.Call(); cp != 0 {
		savedOutputCP = cp
	}
	_, _, _ = procSetConsoleOutputCP.Call(uintptr(65001))
	ok := true
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		fd := f.Fd()
		mode, isConsole := getConsoleMode(fd)
		if !isConsole {
			continue // redirected; isTerminal gates ANSI separately
		}
		if mode&winEnableVTProcessing != 0 {
			continue
		}
		if !setConsoleMode(fd, mode|winEnableVTProcessing) {
			ok = false
		}
	}
	return ok
}

// rawMode switches the stdin console to raw, VT-encoded input so the
// interactive picker receives keystrokes (arrows arrive as ESC [ A/B).
func rawMode() (func(), bool) {
	fd := os.Stdin.Fd()
	saved, isConsole := getConsoleMode(fd)
	if !isConsole {
		return func() {}, false
	}
	raw := saved &^ (winEnableEchoInput | winEnableLineInput | winEnableProcessedInput |
		winEnableWindowInput | winEnableMouseInput)
	raw |= winEnableVTInput
	if !setConsoleMode(fd, raw) {
		return func() {}, false
	}
	return func() { setConsoleMode(fd, saved) }, true
}
