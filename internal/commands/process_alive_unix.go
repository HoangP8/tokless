//go:build unix

package commands

import (
	"os"
	"syscall"
)

// processAlive reports whether a process with the given PID is still running.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
