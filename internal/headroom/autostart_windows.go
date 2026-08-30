//go:build windows

package headroom

import (
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

// Windows autostart: a scheduled task that runs `tokless __proxy-watch` at
// logon with the user's own environment.
const proxyAutostartTask = "tokless-headroom-proxy"

type scheduledTask struct {
	Triggers struct {
		Logon *struct{} `xml:"LogonTrigger"`
	} `xml:"Triggers"`
	Principals struct {
		Principal struct {
			RunLevel string `xml:"RunLevel"`
		} `xml:"Principal"`
	} `xml:"Principals"`
	Actions struct {
		Exec struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

func parseScheduledTask(raw []byte) (scheduledTask, bool) {
	var task scheduledTask
	if err := xml.Unmarshal(raw, &task); err != nil || task.Triggers.Logon == nil {
		return scheduledTask{}, false
	}
	return task, true
}

func scheduledTaskManaged(raw []byte) bool {
	task, ok := parseScheduledTask(raw)
	command := filepath.Base(strings.Trim(strings.TrimSpace(task.Actions.Exec.Command), `"`))
	return ok && (strings.EqualFold(command, "tokless") || strings.EqualFold(command, "tokless.exe")) &&
		strings.TrimSpace(task.Actions.Exec.Arguments) == "__proxy-watch" &&
		task.Principals.Principal.RunLevel == "LeastPrivilege"
}

func scheduledTaskMatches(raw []byte, bin string) bool {
	task, ok := parseScheduledTask(raw)
	if !ok || !scheduledTaskManaged(raw) {
		return false
	}
	command := filepath.Clean(strings.TrimSpace(task.Actions.Exec.Command))
	want := filepath.Clean(strings.TrimSpace(bin))
	return command == want && strings.TrimSpace(task.Actions.Exec.Arguments) == "__proxy-watch" &&
		task.Principals.Principal.RunLevel == "LeastPrivilege"
}

func scheduledTaskRunning() bool {
	out, err := exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/fo", "CSV", "/nh").Output()
	if err != nil {
		return false
	}
	records, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	if err != nil || len(records) == 0 || len(records[0]) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(records[0][len(records[0])-1]), "Running")
}

func readScheduledTask() ([]byte, error) {
	return exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/xml").Output()
}

func EnableProxyAutostart() (err error) {
	bin := util.ToklessAbsStrict()
	if bin == "" {
		return nil
	}
	if _, err := exec.LookPath("schtasks"); err != nil {
		return fmt.Errorf("schtasks not found; keeping proxy running for this session")
	}
	if task, queryErr := readScheduledTask(); queryErr == nil && scheduledTaskMatches(task, bin) {
		if scheduledTaskRunning() && ProxyRunning() && proxySupervisedArgsMatch(ProxyPort()) {
			return nil
		}
	}
	var oldTask []byte
	oldTaskRunning := false
	if out, queryErr := readScheduledTask(); queryErr == nil {
		if !scheduledTaskManaged(out) {
			return fmt.Errorf("refusing to replace non-tokless scheduled task %s", proxyAutostartTask)
		}
		oldTask = out
		oldTaskRunning = scheduledTaskRunning()
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
		if scheduledTaskRunning() && ProxyRunning() && proxySupervisedArgsMatch(ProxyPort()) {
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
	out, err := readScheduledTask()
	if err != nil || !scheduledTaskManaged(out) {
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
	out, err := readScheduledTask()
	return err == nil && scheduledTaskMatches(out, util.ToklessAbsStrict()) && scheduledTaskRunning() && ProxyRunning() && proxySupervisedArgsMatch(ProxyPort())
}
