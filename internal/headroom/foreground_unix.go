//go:build !windows

package headroom

import (
	"os"
	"syscall"
)

var proxyExec = syscall.Exec

func runHeadroomForeground(bin string, args []string) error {
	if err := writeProxySupervisedState(os.Getpid(), bin, args, nil); err != nil {
		return err
	}
	if err := proxyExec(bin, append([]string{bin}, args...), os.Environ()); err != nil {
		_ = clearProxySupervisedState()
		return err
	}
	return nil
}
