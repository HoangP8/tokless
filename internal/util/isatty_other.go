//go:build !linux && !darwin && !windows

package util

import "os"

// isTerminal falls back to the char-device heuristic on unknown platforms.
func isTerminal(fd uintptr) bool {
	fi, err := os.NewFile(fd, "").Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
