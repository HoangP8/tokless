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

var queryScheduledTaskState = func() ([]byte, error) {
	return exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/fo", "CSV", "/nh").CombinedOutput()
}

func scheduledTaskRunning() (bool, error) {
	out, err := queryScheduledTaskState()
	if err != nil {
		if scheduledTaskNotFound(out) {
			return false, nil
		}
		return false, err
	}
	records, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	if err != nil || len(records) == 0 || len(records[0]) == 0 {
		if err == nil {
			err = errors.New("empty task state")
		}
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(records[0][len(records[0])-1])) {
	case "running":
		return true, nil
	case "ready", "queued":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected task state %q", records[0][len(records[0])-1])
	}
}

func readScheduledTask() ([]byte, error) {
	return exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/xml").CombinedOutput()
}

func scheduledTaskNotFound(output []byte) bool {
	s := strings.ToLower(string(output))
	return strings.Contains(s, "cannot find") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "not found")
}

var endProxyAutostartTask = func() error {
	out, err := exec.Command("schtasks", "/end", "/tn", proxyAutostartTask).CombinedOutput()
	if err != nil && scheduledTaskNotFound(out) {
		return nil
	}
	return err
}

var deleteProxyAutostartTask = func() error {
	out, err := exec.Command("schtasks", "/delete", "/tn", proxyAutostartTask, "/f").CombinedOutput()
	if err != nil && scheduledTaskNotFound(out) {
		return nil
	}
	return err
}

func endExistingProxyAutostartTask(exists bool) error {
	if !exists {
		return nil
	}
	return endProxyAutostartTask()
}

func endRunningProxyAutostartTask(running bool) error {
	if !running {
		return nil
	}
	return endProxyAutostartTask()
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
		running, stateErr := scheduledTaskRunning()
		if stateErr != nil {
			return fmt.Errorf("query state of %s: %w", proxyAutostartTask, stateErr)
		}
		if running && ProxyRunning() && proxySupervisedArgsMatch(proxyPortFromRuntime()) {
			return nil
		}
	} else if queryErr != nil && !scheduledTaskNotFound(task) {
		return fmt.Errorf("query %s: %w", proxyAutostartTask, queryErr)
	}
	var oldTask []byte
	oldTaskRunning := false
	if out, queryErr := readScheduledTask(); queryErr == nil {
		if !scheduledTaskManaged(out) {
			return fmt.Errorf("refusing to replace non-tokless scheduled task %s", proxyAutostartTask)
		}
		oldTask = out
		var stateErr error
		oldTaskRunning, stateErr = scheduledTaskRunning()
		if stateErr != nil {
			return fmt.Errorf("query state of %s: %w", proxyAutostartTask, stateErr)
		}
	} else if !scheduledTaskNotFound(out) {
		return fmt.Errorf("query %s: %w", proxyAutostartTask, queryErr)
	}
	proxyWasRunning := ProxyRunning() && (proxyArgsMatchRecorded(proxyPortFromRuntime()) || proxySupervisedArgsMatch(proxyPortFromRuntime()))
	if err := requestProxyStop(); err != nil {
		return fmt.Errorf("proxy stop request: %w", err)
	}
	started := false
	taskCreated := false
	taskCreateAttempted := false
	oldTaskEnded := false
	stopRequestCleared := false
	defer func() {
		if !started {
			markerReady := true
			if stopRequestCleared {
				if requestErr := requestProxyStop(); requestErr != nil {
					markerReady = false
					err = errors.Join(err, fmt.Errorf("restore proxy stop request: %w", requestErr))
				}
			}
			var stopErr error
			if taskCreated {
				stopErr = stopHeadroomDaemonForHandoff()
				if stopErr != nil {
					markerReady = false
					err = errors.Join(err, fmt.Errorf("stop replacement proxy: %w", stopErr))
				}
			}
			rollbackNeeded := taskCreateAttempted || oldTaskEnded
			rollbackOK := !rollbackNeeded
			if rollbackNeeded {
				rollbackErr := rollbackScheduledTask(oldTask, false)
				if rollbackErr != nil {
					markerReady = false
					if err == nil {
						err = fmt.Errorf("rollback %s: %w", proxyAutostartTask, rollbackErr)
					} else {
						err = errors.Join(err, fmt.Errorf("rollback %s: %w", proxyAutostartTask, rollbackErr))
					}
				} else {
					rollbackOK = true
				}
			}
			if markerReady && rollbackOK {
				if clearErr := clearProxyStopRequest(); clearErr != nil {
					markerReady = false
					err = errors.Join(err, fmt.Errorf("clear proxy stop request for rollback: %w", clearErr))
				}
			}
			if markerReady && rollbackOK && oldTaskEnded && oldTaskRunning {
				if runErr := exec.Command("schtasks", "/run", "/tn", proxyAutostartTask).Run(); runErr != nil {
					err = errors.Join(err, fmt.Errorf("restart %s: %w", proxyAutostartTask, runErr))
					if markerErr := requestProxyStop(); markerErr != nil {
						err = errors.Join(err, fmt.Errorf("restore proxy stop request after restart failure: %w", markerErr))
					}
				}
			}
			if markerReady && rollbackOK && (!oldTaskEnded || !oldTaskRunning) {
				if restoreErr := restoreProxyAfterAutostartRollback(proxyWasRunning); restoreErr != nil {
					err = errors.Join(err, restoreErr)
				}
			}
		}
	}()
	if err := endRunningProxyAutostartTask(oldTaskRunning); err != nil {
		return fmt.Errorf("end %s: %w", proxyAutostartTask, err)
	}
	oldTaskEnded = len(oldTask) > 0
	taskCreateAttempted = true
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
	if err := stopHeadroomDaemonForHandoff(); err != nil {
		return err
	}
	if err := clearProxyStopRequest(); err != nil {
		return fmt.Errorf("clear proxy stop request: %w", err)
	}
	stopRequestCleared = true
	if err := exec.Command("schtasks", "/run", "/tn", proxyAutostartTask).Run(); err != nil {
		return fmt.Errorf("initial start of %s: %w", proxyAutostartTask, err)
	}
	deadline := time.Now().Add(proxyReadyTimeout)
	for time.Now().Before(deadline) {
		running, stateErr := scheduledTaskRunning()
		if stateErr != nil {
			return fmt.Errorf("query state of %s: %w", proxyAutostartTask, stateErr)
		}
		if running && ProxyRunning() && proxySupervisedArgsMatch(proxyPortFromRuntime()) {
			started = true
			return nil
		}
		time.Sleep(proxyPollInterval)
	}
	return fmt.Errorf("%s started but proxy is not ready", proxyAutostartTask)
}

func rollbackScheduledTask(oldTask []byte, wasRunning bool) error {
	if len(oldTask) == 0 {
		return deleteProxyAutostartTask()
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
	if err != nil {
		if scheduledTaskNotFound(out) {
			return nil
		}
		return fmt.Errorf("query %s: %w", proxyAutostartTask, err)
	}
	if !scheduledTaskMatches(out, util.ToklessAbsStrict()) {
		return nil
	}
	running, stateErr := scheduledTaskRunning()
	if stateErr != nil {
		return fmt.Errorf("query state of %s: %w", proxyAutostartTask, stateErr)
	}
	if err := endRunningProxyAutostartTask(running); err != nil {
		return fmt.Errorf("end %s: %w", proxyAutostartTask, err)
	}
	if err := deleteProxyAutostartTask(); err != nil {
		rollbackErr := rollbackScheduledTask(out, running)
		return errors.Join(fmt.Errorf("delete %s: %w", proxyAutostartTask, err), rollbackErr)
	}
	return nil
}

func ProxyAutostartEnabled() bool {
	out, err := readScheduledTask()
	running, stateErr := scheduledTaskRunning()
	return err == nil && stateErr == nil && scheduledTaskMatches(out, util.ToklessAbsStrict()) && running && ProxyRunning() && proxySupervisedArgsMatch(proxyPortFromRuntime())
}
