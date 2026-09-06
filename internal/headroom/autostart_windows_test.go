//go:build windows

package headroom

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestProxyAutostartWindowsCommandShape(t *testing.T) {
	task := proxyAutostartTask
	if task == "" || strings.ContainsAny(task, `\/"`) {
		t.Fatalf("invalid task name: %q", task)
	}

	createArgs := []string{"/create", "/tn", task, "/tr", `"tokless.exe" __proxy-watch`, "/sc", "ONLOGON", "/rl", "LIMITED", "/f"}
	joined := strings.Join(createArgs, " ")
	for _, want := range []string{"/sc ONLOGON", "/rl LIMITED", "/f", "__proxy-watch"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("create args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "/rl HIGHEST") {
		t.Fatalf("create args must not use /rl HIGHEST: %s", joined)
	}

	runArgs := []string{"/run", "/tn", task}
	if len(runArgs) != 3 || runArgs[0] != "/run" {
		t.Fatalf("run args wrong: %v", runArgs)
	}

	deleteArgs := []string{"/delete", "/tn", task, "/f"}
	if len(deleteArgs) != 4 || deleteArgs[0] != "/delete" || deleteArgs[3] != "/f" {
		t.Fatalf("delete args wrong: %v", deleteArgs)
	}
	endArgs := []string{"/end", "/tn", task}
	if len(endArgs) != 3 || endArgs[0] != "/end" {
		t.Fatalf("end args wrong: %v", endArgs)
	}
}

func TestEndProxyAutostartTaskPropagatesFailure(t *testing.T) {
	oldEnd := endProxyAutostartTask
	t.Cleanup(func() { endProxyAutostartTask = oldEnd })
	endProxyAutostartTask = func() error { return os.ErrPermission }
	if err := endExistingProxyAutostartTask(true); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("endExistingProxyAutostartTask = %v, want permission error", err)
	}
}

func TestEndRunningProxyAutostartTaskSkipsDormantTask(t *testing.T) {
	called := false
	oldEnd := endProxyAutostartTask
	t.Cleanup(func() { endProxyAutostartTask = oldEnd })
	endProxyAutostartTask = func() error {
		called = true
		return nil
	}
	if err := endRunningProxyAutostartTask(false); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("dormant task should not receive schtasks /end")
	}
}

func TestScheduledTaskNotFoundRequiresExplicitError(t *testing.T) {
	if !scheduledTaskNotFound([]byte("ERROR: The system cannot find the file specified.")) {
		t.Fatal("expected explicit task-not-found output")
	}
	if scheduledTaskNotFound([]byte("ERROR: Access is denied.")) {
		t.Fatal("access failure must not be treated as missing task")
	}
}

func TestScheduledTaskRunningPropagatesStateErrors(t *testing.T) {
	oldQuery := queryScheduledTaskState
	t.Cleanup(func() { queryScheduledTaskState = oldQuery })
	for _, tc := range []struct {
		name string
		out  string
		err  error
	}{
		{name: "access", out: "ERROR: Access is denied.", err: os.ErrPermission},
		{name: "empty", out: "", err: nil},
		{name: "malformed", out: `"task","Running`, err: nil},
		{name: "unknown", out: `"\\tokless","Unknown"`, err: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queryScheduledTaskState = func() ([]byte, error) { return []byte(tc.out), tc.err }
			if _, err := scheduledTaskRunning(); err == nil {
				t.Fatal("scheduledTaskRunning returned nil error")
			}
		})
	}
}

func TestDeleteProxyAutostartTaskIgnoresMissingTask(t *testing.T) {
	oldDelete := deleteProxyAutostartTask
	t.Cleanup(func() { deleteProxyAutostartTask = oldDelete })
	deleteProxyAutostartTask = func() error { return nil }
	if err := deleteProxyAutostartTask(); err != nil {
		t.Fatal(err)
	}
}

func TestEndProxyAutostartTaskPropagatesOperationalFailure(t *testing.T) {
	if scheduledTaskNotFound([]byte("ERROR: The system cannot find the task specified.")) == false {
		t.Fatal("expected explicit task-not-found output")
	}
	if scheduledTaskNotFound([]byte("ERROR: The task cannot be ended because access is denied.")) {
		t.Fatal("operational end failure must not be treated as missing task")
	}
}
