//go:build !windows

package headroom

import (
	"os"
	"syscall"
)

var proxyExec = syscall.Exec

func runHeadroomForeground(bin string, args []string) error {
	identity, err := identifyProcess(os.Getpid())
	if err != nil {
		return err
	}
	if err := writeProxySupervisedState(os.Getpid(), bin, args, nil, identity.Start); err != nil {
		return err
	}
	if err := proxyExec(bin, append([]string{bin}, args...), os.Environ()); err != nil {
		_ = clearProxySupervisedState()
		return err
	}
	return nil
}
