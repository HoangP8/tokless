//go:build windows

package commands

import (
	"os/exec"
)

// backgroundSpawn starts cmd detached. Windows has no session concept; the
// process is spawned independently and inherits no console of its own.
func backgroundSpawn(cmd *exec.Cmd) {
	_ = cmd.Start()
}
