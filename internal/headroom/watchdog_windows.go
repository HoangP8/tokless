//go:build windows

package headroom

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

func proxyStopRequestFile() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "proxy.stop.request")
}

func requestProxyStop() error {
	if err := util.EnsureDir(util.HeadroomPathsResolved().Root); err != nil {
		return err
	}
	return os.WriteFile(proxyStopRequestFile(), nil, 0o600)
}
func clearProxyStopRequest() error {
	err := os.Remove(proxyStopRequestFile())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func proxyStopRequested() (bool, error) {
	_, err := os.Stat(proxyStopRequestFile())
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func RunProxyWatchdog() error {
	bin := ResolveHeadroomBin()
	if bin == "" {
		return fmt.Errorf("headroom binary not found")
	}
	requested, err := proxyStopRequested()
	if err != nil {
		return err
	}
	if requested {
		return nil
	}
	for attempts := 0; ; attempts++ {
		if err := runHeadroomSupervised(bin, proxyArgs(ProxyPort())); err != nil {
			requested, statErr := proxyStopRequested()
			if statErr != nil {
				return statErr
			}
			if requested {
				return nil
			}
			if attempts >= 4 {
				return fmt.Errorf("headroom watchdog failed after %d attempts: %w", attempts+1, err)
			}
			time.Sleep(time.Second)
			continue
		}
		requested, statErr := proxyStopRequested()
		if statErr != nil {
			return statErr
		}
		if requested {
			return nil
		}
		time.Sleep(time.Second)
	}
}

func runHeadroomSupervised(bin string, args []string) error {
	release, err := acquireProxyStartLock(proxyNow)
	if err != nil {
		return err
	}
	released := false
	defer func() {
		if !released {
			release()
		}
	}()
	requested, err := proxyStopRequested()
	if err != nil {
		return err
	}
	if requested {
		release()
		released = true
		return nil
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process == nil {
		return fmt.Errorf("headroom proxy started without a process")
	}
	identity, err := verifyIdentityWithRetry(cmd.Process.Pid, bin, args)
	if err != nil {
		return errors.Join(err, cleanupSupervisedProcess(cmd.Process, ""))
	}
	pidFile, _ := proxyFiles()
	if err := proxyWrite(pidFile, proxyOwnership{PID: cmd.Process.Pid, Executable: identity.Executable, Args: identity.Args, Start: identity.Start}); err != nil {
		return errors.Join(err, cleanupSupervisedProcess(cmd.Process, ""))
	}
	if err := writeProxySupervisedState(cmd.Process.Pid, bin, args, nil); err != nil {
		return errors.Join(err, cleanupSupervisedProcess(cmd.Process, pidFile))
	}
	requested, err = proxyStopRequested()
	if err != nil {
		return errors.Join(err, cleanupSupervisedProcess(cmd.Process, pidFile))
	}
	if requested {
		return cleanupSupervisedProcess(cmd.Process, pidFile)
	}
	release()
	released = true
	err = cmd.Wait()
	return errors.Join(err, cleanupSupervisedProcess(nil, pidFile))
}

func cleanupSupervisedProcess(proc *os.Process, pidFile string) error {
	var errs []error
	if proc != nil {
		killErr := proc.Kill()
		if killErr != nil {
			errs = append(errs, killErr)
		}
		if _, waitErr := proc.Wait(); waitErr != nil {
			errs = append(errs, waitErr)
		}
	}
	if pidFile != "" {
		if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if err := clearProxySupervisedState(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
