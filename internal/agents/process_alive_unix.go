//go:build !windows

package agents

import "os"

func proxyProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	return err == nil && proc.Signal(nil) == nil
}
