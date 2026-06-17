//go:build windows

package commands

import (
	"syscall"
)

// processAlive reports whether the process identified by pid is still running.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil || h == 0 {
		return false
	}
	var exitCode uint32
	err = syscall.GetExitCodeProcess(h, &exitCode)
	_ = syscall.CloseHandle(h)
	if err != nil {
		return false
	}
	return exitCode == 259
}
