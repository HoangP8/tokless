//go:build !windows

package commands

import (
	"os"
	"syscall"
)

// reexecSelfImpl replaces the current process image.
func reexecSelfImpl() error {
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	args := append([]string{exe}, os.Args[1:]...)
	return syscall.Exec(exe, args, withEnvKV(os.Environ(), reexecEnvKey, "1"))
}
