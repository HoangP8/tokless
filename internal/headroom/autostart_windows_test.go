//go:build windows

package headroom

import (
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
