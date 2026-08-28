//go:build windows

package headroom

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

// Windows autostart: a scheduled task that runs `tokless __proxy-watch` at
// logon with the user's own environment.
const proxyAutostartTask = "tokless-headroom-proxy"

func EnableProxyAutostart() (err error) {
	bin := util.ToklessAbsStrict()
	if bin == "" {
		return nil
	}
	if _, err := exec.LookPath("schtasks"); err != nil {
		return fmt.Errorf("schtasks not found; keeping proxy running for this session")
	}
	var oldTask []byte
	oldTaskRunning := false
	if out, queryErr := exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/xml").Output(); queryErr == nil {
		if !strings.Contains(string(out), "__proxy-watch") {
			return fmt.Errorf("refusing to replace non-tokless scheduled task %s", proxyAutostartTask)
		}
		oldTask = out
		if state, stateErr := exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/fo", "LIST", "/v").Output(); stateErr == nil {
			oldTaskRunning = strings.Contains(string(state), "Running")
		}
	}
	if err := requestProxyStop(); err != nil {
		return fmt.Errorf("proxy stop request: %w", err)
	}
	started := false
	taskCreated := false
	defer func() {
		if !started {
			if taskCreated {
				rollbackErr := rollbackScheduledTask(oldTask, oldTaskRunning)
				if rollbackErr != nil {
					if err == nil {
						err = fmt.Errorf("rollback %s: %w", proxyAutostartTask, rollbackErr)
					} else {
						err = errors.Join(err, fmt.Errorf("rollback %s: %w", proxyAutostartTask, rollbackErr))
					}
				}
			}
			if cleanupErr := clearProxyStopRequest(); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("clear proxy stop request: %w", cleanupErr))
			}
		}
	}()
	_ = exec.Command("schtasks", "/end", "/tn", proxyAutostartTask).Run()
	create := exec.Command("schtasks", "/create",
		"/tn", proxyAutostartTask,
		"/tr", `"`+bin+`" __proxy-watch`,
		"/sc", "ONLOGON",
		"/rl", "LIMITED",
		"/f")
	if err := create.Run(); err != nil {
		return err
	}
	taskCreated = true
	if err := stopHeadroomDaemon(); err != nil {
		return err
	}
	if err := clearProxyStopRequest(); err != nil {
		return fmt.Errorf("clear proxy stop request: %w", err)
	}
	if err := exec.Command("schtasks", "/run", "/tn", proxyAutostartTask).Run(); err != nil {
		return fmt.Errorf("initial start of %s: %w", proxyAutostartTask, err)
	}
	deadline := time.Now().Add(proxyReadyTimeout)
	for time.Now().Before(deadline) {
		if ProxyRunning() && proxySupervisedArgsMatch(ProxyPort()) {
			started = true
			return nil
		}
		time.Sleep(proxyPollInterval)
	}
	return fmt.Errorf("%s started but proxy is not ready", proxyAutostartTask)
}

func rollbackScheduledTask(oldTask []byte, wasRunning bool) error {
	if len(oldTask) == 0 {
		return exec.Command("schtasks", "/delete", "/tn", proxyAutostartTask, "/f").Run()
	}
	f, err := os.CreateTemp("", "tokless-task-*.xml")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.Write(oldTask); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := exec.Command("schtasks", "/create", "/tn", proxyAutostartTask, "/xml", path, "/f").Run(); err != nil {
		return err
	}
	if wasRunning {
		return exec.Command("schtasks", "/run", "/tn", proxyAutostartTask).Run()
	}
	return nil
}

func DisableProxyAutostart() error {
	if _, err := exec.LookPath("schtasks"); err != nil {
		return nil
	}
	out, err := exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/xml").Output()
	if err != nil || !strings.Contains(string(out), "__proxy-watch") {
		return nil
	}
	if err := exec.Command("schtasks", "/end", "/tn", proxyAutostartTask).Run(); err != nil {
		return fmt.Errorf("end %s: %w", proxyAutostartTask, err)
	}
	if err := exec.Command("schtasks", "/delete", "/tn", proxyAutostartTask, "/f").Run(); err != nil {
		return fmt.Errorf("delete %s: %w", proxyAutostartTask, err)
	}
	return nil
}

func ProxyAutostartEnabled() bool {
	out, err := exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/xml").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "__proxy-watch")
}
