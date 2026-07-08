//go:build windows

package commands

import (
	"os"
	"os/exec"
)

// reexecSelfImpl runs the on-disk binary as a child (no execve on Windows),
// waits, then exits with the child's code so the parent never continues install.
func reexecSelfImpl() error {
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	c := exec.Command(exe, os.Args[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	c.Env = withEnvKV(os.Environ(), reexecEnvKey, "1")
	err = c.Run()
	if err == nil {
		os.Exit(0)
	}
	if ee, ok := err.(*exec.ExitError); ok {
		os.Exit(ee.ExitCode())
	}
	return err
}
