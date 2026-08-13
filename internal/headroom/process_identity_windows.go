//go:build windows

package headroom

import (
	"fmt"
	"os"
)

type processIdentityInfo struct {
	Executable string
	Args       []string
	Start      string
}

func processIdentitySupported() bool { return false }

func identifyProcess(int) (processIdentityInfo, error) {
	return processIdentityInfo{}, fmt.Errorf("process identity unavailable")
}
func killProcess(proc *os.Process) error {
	return fmt.Errorf("process identity unavailable for pid %d", proc.Pid)
}
func processGone(*os.Process) bool { return false }
