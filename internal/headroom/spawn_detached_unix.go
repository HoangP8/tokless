//go:build unix

package headroom

import (
	"os/exec"
	"syscall"
)

// spawnDetached starts cmd in a new session so the parent can exit without
// tearing the daemon down.
func spawnDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
