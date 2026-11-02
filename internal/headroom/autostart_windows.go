//go:build windows

package headroom

import (
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/HoangP8/tokless/internal/util"
)

// Windows autostart: a scheduled task that runs `tokless __proxy-watch` at
// logon with the user's own environment.
const proxyAutostartTask = "tokless-headroom-proxy"

const proxyAutostartMechanism = `Task Scheduler task "` + proxyAutostartTask + `"`

type scheduledTask struct {
	Settings struct {
		Enabled string `xml:"Enabled"`
	} `xml:"Settings"`
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

// schtasks declares UTF-16 but pipes 8-bit text; accept both.
func scheduledTaskText(raw []byte) string {
	bom := len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE
	if bom || len(raw) > 3 && raw[1] == 0 && raw[3] == 0 {
		if bom {
			raw = raw[2:]
		}
		u := make([]uint16, len(raw)/2)
		for i := range u {
			u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
		}
		return string(utf16.Decode(u))
	}
	return strings.ToValidUTF8(strings.TrimPrefix(string(raw), "\xef\xbb\xbf"), "?")
}

func parseScheduledTask(raw []byte) (scheduledTask, bool) {
	var task scheduledTask
	dec := xml.NewDecoder(strings.NewReader(scheduledTaskText(raw)))
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	if err := dec.Decode(&task); err != nil || task.Triggers.Logon == nil {
		return scheduledTask{}, false
	}
	return task, true
}

func scheduledTaskManaged(raw []byte) bool {
	task, ok := parseScheduledTask(raw)
	command := filepath.Base(strings.Trim(strings.TrimSpace(task.Actions.Exec.Command), `"`))
	return ok && (strings.EqualFold(command, "tokless") || strings.EqualFold(command, "tokless.exe")) &&
		strings.TrimSpace(task.Actions.Exec.Arguments) == "__proxy-watch" &&
		isLimitedRunLevel(task.Principals.Principal.RunLevel)
}

func scheduledTaskEnabled(raw []byte) bool {
	task, ok := parseScheduledTask(raw)
	return ok && !strings.EqualFold(strings.TrimSpace(task.Settings.Enabled), "false")
}

func isLimitedRunLevel(level string) bool { return level == "" || level == "LeastPrivilege" }

func scheduledTaskMatches(raw []byte, bin string) bool {
	task, ok := parseScheduledTask(raw)
	if !ok || !scheduledTaskManaged(raw) {
		return false
	}
	command := filepath.Clean(strings.Trim(strings.TrimSpace(task.Actions.Exec.Command), `"`))
	want := filepath.Clean(strings.TrimSpace(bin))
	return strings.EqualFold(command, want)
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
	case "ready", "queued", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected task state %q", records[0][len(records[0])-1])
	}
}

// runSchtasks runs schtasks and, on failure, includes its output in the error
// so users see why (e.g. "Access is denied") instead of a bare exit status.
func runSchtasks(args ...string) error {
	out, err := exec.Command("schtasks", args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("schtasks %s: %s: %w", args[0], msg, err)
		}
		return fmt.Errorf("schtasks %s: %w", args[0], err)
	}
	return nil
}

var readScheduledTask = func() ([]byte, error) {
	return exec.Command("schtasks", "/query", "/tn", proxyAutostartTask, "/xml").CombinedOutput()
}

var listScheduledTasks = func() ([]byte, error) {
	return exec.Command("schtasks", "/query", "/fo", "CSV", "/nh").Output()
}

// scheduledTaskAbsent is a locale-independent missing-task check: the full
// task listing succeeds and has no row for our task.
var scheduledTaskAbsent = func() bool {
	out, err := listScheduledTasks()
	if err != nil {
		return false
	}
	r := csv.NewReader(strings.NewReader(string(out)))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return false
	}
	for _, rec := range records {
		if len(rec) == 0 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSpace(rec[0]), `\`)
		if strings.EqualFold(name, proxyAutostartTask) {
			return false
		}
	}
	return true
}

func scheduledTaskNotFound(output []byte) bool {
	s := strings.ToLower(string(output))
	return strings.Contains(s, "cannot find") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "not found")
}

var endProxyAutostartTask = func() error {
	out, err := exec.Command("schtasks", "/end", "/tn", proxyAutostartTask).CombinedOutput()
	if err != nil && (scheduledTaskNotFound(out) || scheduledTaskAbsent()) {
		return nil
	}
	return err
}

var deleteProxyAutostartTask = func() error {
	out, err := exec.Command("schtasks", "/delete", "/tn", proxyAutostartTask, "/f").CombinedOutput()
	if err != nil && (scheduledTaskNotFound(out) || scheduledTaskAbsent()) {
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

func enableProxyAutostartOS() (err error) {
	bin := util.ToklessAbsStrict()
	if bin == "" {
		return nil
	}
	if _, err := exec.LookPath("schtasks"); err != nil {
		return fmt.Errorf("%w: schtasks not found; keeping proxy running for this session", ErrProxyAutostartUnavailable)
	}
	if task, queryErr := readScheduledTask(); queryErr == nil && scheduledTaskMatches(task, bin) {
		running, stateErr := scheduledTaskRunning()
		if stateErr != nil {
			return fmt.Errorf("query state of %s: %w", proxyAutostartTask, stateErr)
		}
		if running && scheduledTaskEnabled(task) && ProxyRunning() && proxySupervisedArgsMatch(proxyPortFromRuntime()) {
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
				if runErr := runSchtasks("/run", "/tn", proxyAutostartTask); runErr != nil {
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
	if err := runSchtasks("/create",
		"/tn", proxyAutostartTask,
		"/tr", `"`+bin+`" __proxy-watch`,
		"/sc", "ONLOGON",
		"/rl", "LIMITED",
		"/f"); err != nil {
		return fmt.Errorf("%w (creating the logon task may need an elevated terminal: run PowerShell as administrator)", err)
	}
	taskCreated = true
	if err := stopHeadroomDaemonForHandoff(); err != nil {
		return err
	}
	if err := clearProxyStopRequest(); err != nil {
		return fmt.Errorf("clear proxy stop request: %w", err)
	}
	stopRequestCleared = true
	if err := runSchtasks("/run", "/tn", proxyAutostartTask); err != nil {
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
	if !utf8.Valid(oldTask) && !strings.HasPrefix(string(oldTask), "\xff\xfe") {
		_ = f.Close()
		return errors.New("cannot restore task: schtasks output is not UTF-8")
	}
	u := utf16.Encode([]rune(scheduledTaskText(oldTask)))
	buf := make([]byte, 0, 2+2*len(u))
	buf = append(buf, 0xFF, 0xFE)
	for _, c := range u {
		buf = append(buf, byte(c), byte(c>>8))
	}
	if _, err := f.Write(buf); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := runSchtasks("/create", "/tn", proxyAutostartTask, "/xml", path, "/f"); err != nil {
		return err
	}
	if wasRunning {
		return runSchtasks("/run", "/tn", proxyAutostartTask)
	}
	return nil
}

func DisableProxyAutostart() error {
	if _, err := exec.LookPath("schtasks"); err != nil {
		return nil
	}
	out, err := readScheduledTask()
	if err != nil {
		if scheduledTaskNotFound(out) || scheduledTaskAbsent() {
			return nil
		}
		return fmt.Errorf("query %s: %w", proxyAutostartTask, err)
	}
	if !scheduledTaskManaged(out) {
		return fmt.Errorf("scheduled task %s is not recognized as tokless's; remove it with: schtasks /delete /tn %s /f", proxyAutostartTask, proxyAutostartTask)
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
	return err == nil && stateErr == nil && scheduledTaskMatches(out, util.ToklessAbsStrict()) && scheduledTaskEnabled(out) && running && ProxyRunning() && proxySupervisedArgsMatch(proxyPortFromRuntime())
}

// ProxyAutostartConfigured reports whether Tokless owns a scheduled task,
// regardless of whether it is currently running.
func ProxyAutostartConfigured() bool {
	out, err := readScheduledTask()
	return err == nil && scheduledTaskMatches(out, util.ToklessAbsStrict())
}
