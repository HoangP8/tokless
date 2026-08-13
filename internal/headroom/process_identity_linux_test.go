//go:build linux

package headroom

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIdentifyProcessRealDirectExecutable(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	identity := waitForProcessIdentity(t, cmd.Process.Pid)
	wantExecutable, err := normalizedExecutable("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Executable != wantExecutable {
		t.Fatalf("executable = %q, want %q", identity.Executable, wantExecutable)
	}
	wantArgs := []string{"600"}
	if !equalStrings(identity.Args, wantArgs) {
		t.Fatalf("argv = %#v, want %#v", identity.Args, wantArgs)
	}
	if identity.Start == "" {
		t.Fatal("start fingerprint is empty")
	}
	if !identity.matches("/bin/sleep", wantArgs) {
		t.Fatal("real direct executable did not match")
	}
}

func TestIdentifyProcessRealConsoleScript(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "headroom")
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	python, err = filepath.EvalSymlinks(python)
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + python + "\nimport time\ntime.sleep(600)\n"
	if err := os.WriteFile(launcher, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(launcher, "proxy", "--port", "8787")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	identity := waitForProcessIdentity(t, cmd.Process.Pid)
	wantInterpreter, err := normalizedExecutable(python)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Executable != wantInterpreter {
		t.Fatalf("interpreter = %q, want %q", identity.Executable, wantInterpreter)
	}
	wantArgs := []string{launcher, "proxy", "--port", "8787"}
	if !equalStrings(identity.Args, wantArgs) {
		t.Fatalf("argv = %#v, want %#v", identity.Args, wantArgs)
	}
	if identity.Start == "" {
		t.Fatal("start fingerprint is empty")
	}
	if !identity.matches(launcher, wantArgs[1:]) {
		t.Fatal("real console script did not match launcher form")
	}
	for name, changed := range map[string][]string{
		"missing argument":    wantArgs[:len(wantArgs)-1],
		"extra argument":      append(append([]string(nil), wantArgs...), "extra"),
		"reordered arguments": {launcher, "--port", "proxy", "8787", "value with spaces"},
	} {
		if identity.matches(launcher, changed[1:]) {
			t.Errorf("%s unexpectedly matched", name)
		}
	}
	wrongLauncher := filepath.Join(filepath.Dir(launcher), "other-headroom")
	if identity.matches(wrongLauncher, wantArgs[1:]) {
		t.Fatal("wrong launcher unexpectedly matched")
	}
	direct := processIdentityInfo{Executable: identity.Executable, Args: []string{"proxy"}, Start: identity.Start}
	if !direct.matches(identity.Executable, direct.Args) {
		t.Fatal("direct identity fixture did not match")
	}
	direct.Executable = "/usr/bin/python3"
	if direct.matches(identity.Executable, []string{"proxy"}) {
		t.Fatal("changed interpreter unexpectedly matched direct form")
	}
}

func waitForProcessIdentity(t *testing.T, pid int) processIdentityInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		identity, err := identifyProcess(pid)
		if err == nil {
			return identity
		}
		time.Sleep(10 * time.Millisecond)
	}
	identity, err := identifyProcess(pid)
	if err != nil {
		t.Fatalf("identifyProcess(%d): %v", pid, err)
	}
	return identity
}
