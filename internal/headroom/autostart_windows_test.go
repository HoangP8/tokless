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

func TestScheduledTaskAbsentParsesListing(t *testing.T) {
	oldList := listScheduledTasks
	t.Cleanup(func() { listScheduledTasks = oldList })
	for _, tc := range []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{name: "other tasks only", out: "\"\\Other\",\"N/A\",\"Ready\"\r\n\"\\tokless-headroom-proxy-x\",\"N/A\",\"Ready\"\r\n", want: true},
		{name: "empty listing", out: "", want: true},
		{name: "present", out: "\"\\tokless-headroom-proxy\",\"N/A\",\"Ready\"\r\n", want: false},
		{name: "present no backslash mixed case", out: "\"Tokless-Headroom-Proxy\",\"N/A\",\"Ready\"\r\n", want: false},
		{name: "schtasks failure", out: "ERROR: Access is denied.", err: os.ErrPermission, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listScheduledTasks = func() ([]byte, error) { return []byte(tc.out), tc.err }
			if got := scheduledTaskAbsent(); got != tc.want {
				t.Fatalf("scheduledTaskAbsent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDisableProxyAutostartLocalizedNotFound(t *testing.T) {
	oldRead, oldList := readScheduledTask, listScheduledTasks
	t.Cleanup(func() { readScheduledTask, listScheduledTasks = oldRead, oldList })
	readScheduledTask = func() ([]byte, error) {
		return []byte("FEHLER: Das System kann die angegebene Datei nicht finden."), errors.New("exit status 1")
	}
	listScheduledTasks = func() ([]byte, error) { return []byte("\"\\Other\",\"N/A\",\"Ready\"\r\n"), nil }
	if err := DisableProxyAutostart(); err != nil {
		t.Fatalf("absent task: DisableProxyAutostart() = %v, want nil", err)
	}
	listScheduledTasks = func() ([]byte, error) { return nil, os.ErrPermission }
	if err := DisableProxyAutostart(); err == nil {
		t.Fatal("listing failure: DisableProxyAutostart() = nil, want error")
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

func TestProxyAutostartMechanismNamesHostMechanism(t *testing.T) {
	if got := ProxyAutostartMechanism(); !strings.Contains(got, proxyAutostartTask) {
		t.Fatalf("ProxyAutostartMechanism() = %q, missing %q", got, proxyAutostartTask)
	}
}

func TestScheduledTaskManagedAcceptsUTF16Declaration(t *testing.T) {
	body := `<Task><Triggers><LogonTrigger/></Triggers>` +
		`<Principals><Principal><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>` +
		`<Actions><Exec><Command>"C:\Users\x\tokless.exe"</Command><Arguments>__proxy-watch</Arguments></Exec></Actions></Task>`
	header := `<?xml version="1.0" encoding="UTF-16"?>`

	// Piped schtasks output: 8-bit text with a UTF-16 declaration.
	if !scheduledTaskManaged([]byte(header + body)) {
		t.Fatal("8-bit text with UTF-16 declaration not recognized")
	}
	// Real UTF-16LE with BOM.
	raw := []byte{0xFF, 0xFE}
	for _, c := range header + body {
		raw = append(raw, byte(c), byte(c>>8))
	}
	if !scheduledTaskManaged(raw) {
		t.Fatal("UTF-16LE task not recognized")
	}
	if !scheduledTaskMatches([]byte(header+body), `C:\Users\x\tokless.exe`) {
		t.Fatal("task should match its own binary")
	}
}
