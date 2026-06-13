//go:build windows

package commands

import (
	"os"
	"os/exec"
)

func detachMcpChild(c *exec.Cmd) {}

// handoffMcp runs the MCP server in the foreground; Windows has no exec replace.
func handoffMcp(path string, argv, env []string) int {
	c := exec.Command(path, argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	c.Env = env
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}
