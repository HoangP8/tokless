//go:build unix

package commands

import (
	"os/exec"
	"syscall"
)

// backgroundSpawn starts cmd detached in a new session so the parent can exit
// without tearing it down. Unix-only: uses Setsid.
func backgroundSpawn(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = cmd.Start()
}
