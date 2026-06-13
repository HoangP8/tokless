//go:build !windows

package commands

import (
	"os/exec"
	"syscall"
)

func detachMcpChild(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// handoffMcp replaces this process with the MCP server so it inherits the raw
// JSON-RPC stdio untouched.
func handoffMcp(path string, argv, env []string) int {
	if err := syscall.Exec(path, argv, env); err != nil {
		return 1
	}
	return 0
}
