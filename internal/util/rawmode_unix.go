//go:build !windows

package util

import (
	"os"
	"os/exec"
	"strings"
)

// enableVT is a no-op on unix terminals; ANSI is always available.
func enableVT() bool { return true }

// RestoreConsoleCP is a no-op outside Windows.
func RestoreConsoleCP() {}

// rawMode toggles terminal raw mode via stty; returns a restore func.
func rawMode() (func(), bool) {
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
