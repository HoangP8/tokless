//go:build windows

package headroom

import "os/exec"

// spawnDetached starts cmd independently.
func spawnDetached(cmd *exec.Cmd) error {
	return cmd.Start()
}
