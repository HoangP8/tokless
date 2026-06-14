//go:build !windows

package commands

import "syscall"

// handoffMcp replaces this process with the MCP server so it inherits the raw
// JSON-RPC stdio untouched.
func handoffMcp(path string, argv, env []string) int {
	if err := syscall.Exec(path, argv, env); err != nil {
		return 1
	}
	return 0
}
