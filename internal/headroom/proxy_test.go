package headroom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

func isolateProxyOps(t *testing.T) {
	t.Helper()
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "")
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "")
	t.Setenv("TOKLESS_HEADROOM_OPENAI_URL", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(util.Home(), ".config"))
	oldProbe, oldSpawn := proxyLiveZProbe, proxySpawn
	oldIdentity, oldKill := proxyIdentity, proxyKill
	oldWrite, oldGone := proxyWrite, proxyGone
	oldWait := proxyWait
	oldSleep := proxySleep
	oldNow := proxyNow
	oldCopilotProbe := copilotProxyLiveProbe
	oldGrokProbe := grokProxyLiveZProbe
	t.Cleanup(func() {
		proxyLiveZProbe, proxySpawn = oldProbe, oldSpawn
		grokProxyLiveZProbe = oldGrokProbe
		proxyIdentity, proxyKill = oldIdentity, oldKill
		proxyWrite, proxyGone = oldWrite, oldGone
		proxyWait = oldWait
		proxySleep = oldSleep
		proxyNow = oldNow
		copilotProxyLiveProbe = oldCopilotProbe
		util.SetHomeOverride("")
		activeBYOKRoutes.Store(nil)
	})
}

func proxyTestBin(t *testing.T) string {
	t.Helper()
	util.SetHomeOverride(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(util.Home(), ".config"))
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("test headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestProxyPortDefaults(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "")
	if got := ProxyPort(); got != 8787 {
		t.Fatalf("ProxyPort = %d, want 8787", got)
	}
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "9123")
	if got := ProxyPort(); got != 9123 {
		t.Fatalf("ProxyPort = %d, want 9123", got)
	}
	if got := ProxyURL(); got != "http://127.0.0.1:9123" {
		t.Fatalf("ProxyURL = %q", got)
	}
}

func TestEnsureProxyUpSkipsWhenRoutingDisabled(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	if err := util.SetProxyRoutingEnabled(false); err != nil {
		t.Fatal(err)
	}
	proxySpawn = func(*exec.Cmd) error { t.Fatal("proxy started while routing disabled"); return nil }
	if err := EnsureProxyUp(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureProxyUpReturnsStartFailure(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	proxyLiveZProbe = func(time.Duration) bool { return false }
	if err := util.SetProxyRoutingEnabled(true); err != nil {
		t.Fatal(err)
	}
	proxySpawn = func(*exec.Cmd) error { return errors.New("start failed") }
	if err := EnsureProxyUp(); err == nil || !strings.Contains(err.Error(), "start failed") {
		t.Fatalf("EnsureProxyUp error = %v, want start failure", err)
	}
}

func TestAcquireProxyStartLockCreatesRuntimeDirectory(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	release, err := acquireProxyStartLock(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(util.HeadroomPathsResolved().Root); err != nil {
		t.Fatalf("runtime directory missing: %v", err)
	}
}

func TestAcquireProxyLifecycleLockLivesOutsidePurgeRoot(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	release, err := AcquireProxyLifecycleLock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if filepath.Dir(proxyLifecycleLock()) == util.HeadroomPathsResolved().Root {
		t.Fatal("lifecycle lock must not live under purge root")
	}
	if _, err := os.Stat(proxyLifecycleLock()); err != nil {
		t.Fatalf("lifecycle lock missing: %v", err)
	}
}

func TestProxyLiveZRejectsNonHeadroomService(t *testing.T) {
	for _, service := range []string{"other-service", "", "headroom"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"service":%q}`, service)
		}))
		port := server.Listener.Addr().(*net.TCPAddr).Port
		t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", fmt.Sprint(port))
		want := service == "headroom-proxy"
		if got := proxyLiveZ(1e9); got != want {
			t.Fatalf("service %q: proxyLiveZ = %v, want %v", service, got, want)
		}
		server.Close()
	}
}

func TestProxyOwnershipRecordIsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.pid")
	if err := writeProxyOwnership(path, proxyOwnership{PID: 41, Executable: "/bin/old-headroom", Args: []string{"proxy"}, Start: "start-41"}); err != nil {
		t.Fatal(err)
	}
	want := proxyOwnership{PID: 42, Executable: "/bin/headroom", Args: []string{"proxy", "--port", "8787"}, Start: "start-42"}
	if err := writeProxyOwnership(path, want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got proxyOwnership
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.PID != want.PID || got.Executable != want.Executable || !equalStrings(got.Args, want.Args) || got.Start != want.Start {
		t.Fatalf("record = %+v, want %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("ownership mode = %o, want 600", mode)
	}
}

func TestProcessIdentityMatchesDirectInvocation(t *testing.T) {
	bin := proxyTestBin(t)
	t.Cleanup(func() { util.SetHomeOverride("") })
	args := []string{"proxy", "--port", "8787"}
	identity := processIdentityInfo{Executable: bin, Args: args, Start: "start"}
	if !identity.matches(bin, args) {
		t.Fatal("direct executable invocation did not match")
	}
}

func TestProcessIdentityMatchesPythonConsoleScript(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "headroom")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"proxy", "--port", "8787"}
	identity := processIdentityInfo{Executable: "/usr/bin/python3", Args: append([]string{launcher}, args...), Start: "start"}
	if !identity.matches(launcher, args) {
		t.Fatal("python console-script invocation did not match")
	}
}

func TestProcessIdentityRejectsWrongConsoleScript(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "headroom")
	wrong := filepath.Join(t.TempDir(), "other")
	for _, path := range []string{launcher, wrong} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"proxy", "--port", "8787"}
	identity := processIdentityInfo{Executable: "/usr/bin/python3", Args: append([]string{wrong}, args...), Start: "start"}
	if identity.matches(launcher, args) {
		t.Fatal("wrong console script was accepted")
	}
}

func TestProcessIdentityRecordRecheckRequiresExactIdentity(t *testing.T) {
	record := proxyOwnership{PID: 42, Executable: "/usr/bin/python3", Args: []string{"headroom", "proxy"}, Start: "start"}
	identity := processIdentityInfo{Executable: record.Executable, Args: append([]string(nil), record.Args...), Start: record.Start}
	if !identity.matchesRecord(record) {
		t.Fatal("exact identity did not match record")
	}
	for name, mutate := range map[string]func(*processIdentityInfo){
		"changed interpreter": func(p *processIdentityInfo) { p.Executable = "/usr/bin/python3.12" },
		"missing argument":    func(p *processIdentityInfo) { p.Args = p.Args[:1] },
		"extra argument":      func(p *processIdentityInfo) { p.Args = append(p.Args, "extra") },
		"reordered arguments": func(p *processIdentityInfo) {
			p.Args[0], p.Args[1] = p.Args[1], p.Args[0]
		},
		"changed argument": func(p *processIdentityInfo) { p.Args[1] = "other" },
		"changed start":    func(p *processIdentityInfo) { p.Start = "recycled-start" },
	} {
		changed := identity
		changed.Args = append([]string(nil), identity.Args...)
		mutate(&changed)
		if changed.matchesRecord(record) {
			t.Errorf("%s matched ownership record", name)
		}
	}
}

func TestRollbackProxyWaitFailureRetainsOwnershipRecord(t *testing.T) {
	isolateProxyOps(t)
	path := filepath.Join(t.TempDir(), "proxy.pid")
	if err := writeProxyOwnership(path, proxyOwnership{PID: 4245, Executable: "/bin/headroom", Args: []string{"proxy"}, Start: "start"}); err != nil {
		t.Fatal(err)
	}
	captured := processIdentityInfo{Executable: "/bin/headroom", Args: []string{"proxy"}, Start: "start"}
	proxyIdentity = func(int) (processIdentityInfo, error) { return captured, nil }
	proxyKill = func(*os.Process) error { return nil }
	proxyWait = func(*os.Process) error { return errors.New("wait failed") }
	now := time.Unix(100, 0)
	proxyNow = func() time.Time { return now }
	proxySleep = func(time.Duration) { now = now.Add(proxyStopTimeout) }

	err := rollbackProxy(&os.Process{Pid: 4245}, path, errors.New("startup failed"))
	if err == nil || !strings.Contains(err.Error(), "wait failed") {
		t.Fatalf("rollbackProxy error = %v", err)
	}
	if _, ok := util.ReadFileSafe(path); !ok {
		t.Fatal("ownership record removed after rollback timeout")
	}
}

func TestRollbackProxyKillFailureSkipsWaitAndRetainsOwnershipRecord(t *testing.T) {
	isolateProxyOps(t)
	path := filepath.Join(t.TempDir(), "proxy.pid")
	if err := writeProxyOwnership(path, proxyOwnership{PID: 4246, Executable: "/bin/headroom", Args: []string{"proxy"}, Start: "start"}); err != nil {
		t.Fatal(err)
	}
	waited := false
	proxyKill = func(*os.Process) error { return errors.New("kill failed") }
	proxyWait = func(*os.Process) error { waited = true; return nil }

	err := rollbackProxy(&os.Process{Pid: 4246}, path, errors.New("startup failed"))
	if err == nil || !strings.Contains(err.Error(), "kill failed") {
		t.Fatalf("rollbackProxy error = %v", err)
	}
	if waited {
		t.Fatal("rollbackProxy waited after kill failure")
	}
	if _, ok := util.ReadFileSafe(path); !ok {
		t.Fatal("ownership record removed after kill failure")
	}
}

func TestRollbackProxyRefusesVerifiedReplacementIdentity(t *testing.T) {
	isolateProxyOps(t)
	proc := &os.Process{Pid: 4247}
	expected := processIdentityInfo{Executable: "/bin/headroom", Args: []string{"proxy"}, Start: "original"}
	identity := processIdentityInfo{Executable: "/bin/replacement", Args: []string{"proxy"}, Start: "replacement"}
	killed := false
	proxyIdentity = func(int) (processIdentityInfo, error) { return identity, nil }
	proxyKill = func(*os.Process) error { killed = true; return nil }

	if err := rollbackProxyWithIdentity(proc, "", &expected, errors.New("identity mismatch")); err == nil {
		t.Fatal("rollbackProxyWithIdentity returned nil")
	}
	if killed {
		t.Fatal("verified replacement process was killed")
	}
}

func TestStopProxyRefusesMismatchedIdentity(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	pidFile, _ := proxyFiles()
	proxyIdentity = func(pid int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: proxyArgs(8787), Start: "start"}, nil
	}
	record := proxyOwnership{PID: 4242, Executable: bin, Args: []string{"proxy"}, Start: "old-start"}
	if err := writeProxyOwnership(pidFile, record); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: []string{"proxy"}, Start: "recycled-start"}, nil
	}
	killed := false
	proxyKill = func(*os.Process) error { killed = true; return nil }
	if err := StopProxy(); err == nil {
		t.Fatal("StopProxy unexpectedly accepted recycled identity")
	}
	if killed {
		t.Fatal("StopProxy killed mismatched process")
	}
	if _, ok := util.ReadFileSafe(pidFile); !ok {
		t.Fatal("ownership record removed after identity mismatch")
	}
}

func TestStopProxyRefusesUnavailableIdentity(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	pidFile, _ := proxyFiles()
	record := proxyOwnership{PID: 4243, Executable: bin, Args: []string{"proxy"}, Start: "start"}
	if err := writeProxyOwnership(pidFile, record); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) { return processIdentityInfo{}, errors.New("unavailable") }
	proxyGone = func(*os.Process) bool { return false } // process present, identity unverifiable
	killed := false
	proxyKill = func(*os.Process) error { killed = true; return nil }
	if err := StopProxy(); err == nil {
		t.Fatal("StopProxy unexpectedly accepted unavailable identity")
	}
	if killed {
		t.Fatal("StopProxy killed unavailable process")
	}
	if _, ok := util.ReadFileSafe(pidFile); !ok {
		t.Fatal("ownership record removed after unavailable identity")
	}
}

func TestStopProxySelfHealsStaleRecord(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	pidFile, _ := proxyFiles()
	record := proxyOwnership{PID: 4245, Executable: bin, Args: []string{"proxy"}, Start: "start"}
	if err := writeProxyOwnership(pidFile, record); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) { return processIdentityInfo{}, errors.New("unavailable") }
	proxyGone = func(*os.Process) bool { return true } // pid dead — stale record
	killed := false
	proxyKill = func(*os.Process) error { killed = true; return nil }
	if err := StopProxy(); err != nil {
		t.Fatalf("StopProxy should self-heal stale record: %v", err)
	}
	if killed {
		t.Fatal("StopProxy killed a process that is already gone")
	}
	if _, ok := util.ReadFileSafe(pidFile); ok {
		t.Fatal("stale ownership record was not removed")
	}
}

func TestStopProxyTimeoutRetainsOwnershipRecord(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	pidFile, _ := proxyFiles()
	record := proxyOwnership{PID: 4244, Executable: bin, Args: []string{"proxy"}, Start: "start"}
	if err := writeProxyOwnership(pidFile, record); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: []string{"proxy"}, Start: "start"}, nil
	}
	proxyKill = func(*os.Process) error { return nil }
	proxyGone = func(*os.Process) bool { return false }
	proxyLiveZProbe = func(time.Duration) bool { return false }
	now := time.Unix(100, 0)
	proxyNow = func() time.Time { return now }
	proxySleep = func(time.Duration) { now = now.Add(proxyStopTimeout) }
	if err := StopProxy(); err == nil || !strings.Contains(err.Error(), "did not stop") {
		t.Fatalf("StopProxy error = %v", err)
	}
	if _, ok := util.ReadFileSafe(pidFile); !ok {
		t.Fatal("ownership record removed after stop timeout")
	}
}

func TestStartProxyRecordWriteFailureRollsBack(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	proxyLiveZProbe = func(time.Duration) bool { return false }
	proxySpawn = func(cmd *exec.Cmd) error { cmd.Process = &os.Process{Pid: 5252}; return nil }
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: proxyArgs(8787), Start: "start"}, nil
	}
	proxyWrite = func(string, proxyOwnership) error { return errors.New("disk full") }
	proxyGone = func(*os.Process) bool { return true }
	kills, waits := 0, 0
	proxyKill = func(*os.Process) error { kills++; return nil }
	proxyWait = func(*os.Process) error { waits++; return nil }
	proxySleep = func(time.Duration) {}
	if err := StartProxy(); err == nil || !strings.Contains(err.Error(), "ownership record") {
		t.Fatalf("StartProxy error = %v", err)
	}
	if kills != 1 {
		t.Fatalf("rollback kills = %d, want 1", kills)
	}
	if waits != 1 {
		t.Fatalf("rollback waits = %d, want 1", waits)
	}
	pidFile, _ := proxyFiles()
	if _, ok := util.ReadFileSafe(pidFile); ok {
		t.Fatal("ownership record remains after rollback")
	}
}

func TestPersistProxyRuntimeProviderClearing(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "9123")
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "https://api.example.test")
	t.Setenv("TOKLESS_PROXY_PROVIDER", "")

	if err := persistProxyRuntime(9123); err != nil {
		t.Fatal(err)
	}
	st, ok := util.ReadProxyRuntime()
	if !ok || st.Port != 9123 || st.Provider != "" {
		t.Fatalf("initial persisted runtime = %+v (ok=%v), want port 9123 provider empty", st, ok)
	}

	t.Setenv("TOKLESS_PROXY_PROVIDER", "apibox")
	if err := persistProxyRuntime(9123); err != nil {
		t.Fatal(err)
	}
	st, _ = util.ReadProxyRuntime()
	if st.Provider != "apibox" {
		t.Fatalf("provider not persisted = %+v", st)
	}

	t.Setenv("TOKLESS_PROXY_PROVIDER", "")
	if err := persistProxyRuntime(9123); err != nil {
		t.Fatal(err)
	}
	st, _ = util.ReadProxyRuntime()
	if st.Provider != "" {
		t.Fatalf("explicitly cleared provider must not persist, got %+v", st)
	}
}

func TestPersistProxyRuntimeGeminiTargetClearing(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	t.Setenv("TOKLESS_HEADROOM_GEMINI_URL", "https://gemini.example.test")
	t.Setenv("TOKLESS_HEADROOM_CLOUDCODE_URL", "https://cloudcode.example.test")
	if err := persistProxyRuntime(8787); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOKLESS_HEADROOM_GEMINI_URL", "")
	t.Setenv("TOKLESS_HEADROOM_CLOUDCODE_URL", "")
	gemini, cloudcode := ProxyUpstreamGeminiURLs()
	if gemini != "" || cloudcode != "" {
		t.Fatalf("explicitly cleared Gemini targets = %q, %q", gemini, cloudcode)
	}
	args := strings.Join(proxyArgs(8787), " ")
	if strings.Contains(args, "--gemini-api-url") || strings.Contains(args, "--cloudcode-api-url") {
		t.Fatalf("cleared targets remained in args: %s", args)
	}
}

func TestProxyUpstreamURLsUsePersistedValuesOnlyWhenEnvUnset(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	os.Unsetenv("TOKLESS_HEADROOM_ANTHROPIC_URL")
	os.Unsetenv("TOKLESS_HEADROOM_OPENAI_URL")
	os.Unsetenv("TOKLESS_HEADROOM_GEMINI_URL")
	os.Unsetenv("TOKLESS_HEADROOM_CLOUDCODE_URL")
	if err := util.SaveHeadroomProxyRuntime(util.ProxyRuntime{
		Port:         8787,
		AnthropicURL: "https://old.anthropic.example",
		OpenAIURL:    "https://old.openai.example",
		GeminiURL:    "https://old.gemini.example",
		CloudCodeURL: "https://old.cloudcode.example",
	}); err != nil {
		t.Fatal(err)
	}
	a, o := ProxyUpstreamURLs()
	if a != "https://old.anthropic.example" || o != "https://old.openai.example" {
		t.Fatalf("unset env upstreams = %q, %q; want old values", a, o)
	}
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "")
	t.Setenv("TOKLESS_HEADROOM_OPENAI_URL", "https://new.openai.example")
	a, o = ProxyUpstreamURLs()
	if a != "https://api.anthropic.com" || o != "https://new.openai.example" {
		t.Fatalf("explicit env upstreams = %q, %q; want defaults/new value", a, o)
	}
}

func TestAcquireProxyStartLockSerializes(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	release, err := acquireProxyStartLock(time.Now)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	got := make(chan error, 1)
	go func() {
		r2, err := acquireProxyStartLock(time.Now)
		if err == nil {
			r2()
		}
		got <- err
	}()
	select {
	case err := <-got:
		t.Fatalf("second acquire completed while lock held: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	release()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("acquire after release failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second acquire never completed after release")
	}
}

func TestAcquireProxyStartLockStealsStale(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	now := time.Unix(300, 0)
	lock := proxyStartLock()
	if err := os.WriteFile(lock, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(lock, time.Unix(200, 0), time.Unix(200, 0)); err != nil {
		t.Fatal(err)
	}
	release, err := acquireProxyStartLock(func() time.Time { return now })
	if err != nil {
		t.Fatalf("stale lock not stolen: %v", err)
	}
	release()
	if util.Exists(lock) {
		t.Fatal("lock file left behind after release")
	}
}

func TestStartProxyReadinessTimeoutRollsBack(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	proxyLiveZProbe = func(time.Duration) bool { return false }
	proxySpawn = func(cmd *exec.Cmd) error {
		cmd.Process = &os.Process{Pid: 5253}
		return nil
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{
			Executable: bin,
			Args:       proxyArgs(8787),
			Start:      "start",
		}, nil
	}
	killed, waited := false, false
	proxyKill = func(*os.Process) error {
		killed = true
		return nil
	}
	proxyWait = func(*os.Process) error { waited = true; return nil }
	proxyGone = func(*os.Process) bool { return killed }
	now := time.Unix(100, 0)
	proxyNow = func() time.Time { return now }
	proxySleep = func(time.Duration) { now = now.Add(proxyReadyTimeout) }

	err := StartProxy()
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("StartProxy error = %v", err)
	}
	if !killed {
		t.Fatal("readiness timeout did not kill child")
	}
	if !waited {
		t.Fatal("readiness timeout did not wait for child")
	}
	pidFile, _ := proxyFiles()
	if _, ok := util.ReadFileSafe(pidFile); ok {
		t.Fatal("ownership record remains after readiness timeout rollback")
	}
}

func TestStartProxyIdentityFailureRollsBackDirectChild(t *testing.T) {
	tests := []struct {
		name     string
		identity func(string) processIdentityInfo
		wantErr  string
	}{
		{name: "lookup error", identity: func(string) processIdentityInfo { return processIdentityInfo{} }, wantErr: "identity lookup failed"},
		{name: "mismatch", identity: func(bin string) processIdentityInfo {
			return processIdentityInfo{Executable: bin, Args: []string{"proxy", "wrong"}, Start: "start"}
		}, wantErr: "identity could not be verified"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateProxyOps(t)
			bin := proxyTestBin(t)
			proxyLiveZProbe = func(time.Duration) bool { return false }
			proxySpawn = func(cmd *exec.Cmd) error { cmd.Process = &os.Process{Pid: 5255}; return nil }
			identityCalls := 0
			proxyIdentity = func(int) (processIdentityInfo, error) {
				identityCalls++
				if tt.name == "lookup error" {
					return processIdentityInfo{}, errors.New(tt.wantErr)
				}
				return tt.identity(bin), nil
			}
			killed, waited := false, false
			proxyKill = func(*os.Process) error { killed = true; return nil }
			proxyWait = func(*os.Process) error { waited = true; return nil }
			now := time.Unix(100, 0)
			proxyNow = func() time.Time { return now }
			proxySleep = func(time.Duration) { now = now.Add(proxyIdentityTimeout) }

			err := StartProxy()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("StartProxy error = %v", err)
			}
			if tt.name == "lookup error" && (!killed || !waited) {
				t.Fatalf("unverified direct child cleanup kill=%v wait=%v", killed, waited)
			}
			if tt.name == "mismatch" && (killed || waited) {
				t.Fatalf("rollback kill=%v wait=%v, must not touch mismatched process", killed, waited)
			}
			if identityCalls != 1 {
				t.Fatalf("proxyIdentity calls = %d, want 1", identityCalls)
			}
			pidFile, _ := proxyFiles()
			if _, ok := util.ReadFileSafe(pidFile); ok {
				t.Fatal("ownership record remains after identity rollback")
			}
		})
	}
}

func TestStartProxyReadinessRollbackIgnoresChangedIdentity(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	proxyLiveZProbe = func(time.Duration) bool { return false }
	proxySpawn = func(cmd *exec.Cmd) error {
		cmd.Process = &os.Process{Pid: 5254}
		return nil
	}
	args := proxyArgs(8787)
	launchIdentity := processIdentityInfo{Executable: bin, Args: args, Start: "start"}
	identityCalls := 0
	proxyIdentity = func(int) (processIdentityInfo, error) {
		identityCalls++
		return launchIdentity, nil
	}
	proxyWrite = func(path string, record proxyOwnership) error {
		return writeProxyOwnership(path, record)
	}
	killed, waited := false, false
	proxyKill = func(*os.Process) error {
		killed = true
		return nil
	}
	proxyWait = func(*os.Process) error { waited = true; return nil }
	now := time.Unix(100, 0)
	proxyNow = func() time.Time { return now }
	proxySleep = func(time.Duration) { now = now.Add(proxyReadyTimeout) }

	err := StartProxy()
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("StartProxy error = %v", err)
	}
	if !killed {
		t.Fatal("readiness timeout did not kill child")
	}
	if !waited {
		t.Fatal("changed-identity readiness timeout did not wait for child")
	}
	pidFile, _ := proxyFiles()
	if _, ok := util.ReadFileSafe(pidFile); ok {
		t.Fatal("ownership record remains after rollback")
	}
	if identityCalls != 3 {
		t.Fatalf("proxyIdentity calls = %d, want 3 (launch, readiness, rollback identity checks)", identityCalls)
	}
}

func TestStartProxyReusesHeadroomProbe(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	bin := proxyTestBin(t)
	proxyLiveZProbe = func(time.Duration) bool { return true }
	proxyIdentity = func(pid int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: proxyArgs(8787), Start: "start"}, nil
	}
	if err := writeProxySupervisedState(os.Getpid(), bin, proxyArgs(8787), nil, "start"); err != nil {
		t.Fatal(err)
	}
	spawned, wrote := false, false
	proxySpawn = func(*exec.Cmd) error { spawned = true; return nil }
	proxyWrite = func(string, proxyOwnership) error { wrote = true; return nil }
	if err := StartProxy(); err != nil {
		t.Fatal(err)
	}
	if spawned || wrote {
		t.Fatalf("reused proxy spawned=%v wrote=%v", spawned, wrote)
	}
}

func TestStartProxyRefusesStaleArgsDaemon(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	bin := proxyTestBin(t)
	pidFile, _ := proxyFiles()
	oldArgs := []string{"proxy", "--port", "8787"}
	if err := writeProxyOwnership(pidFile, proxyOwnership{PID: 5355, Executable: bin, Args: oldArgs, Start: "start"}); err != nil {
		t.Fatal(err)
	}
	live := true
	killed, spawned, wrote := false, false, false
	proxyLiveZProbe = func(time.Duration) bool { return live }
	proxyIdentity = func(pid int) (processIdentityInfo, error) {
		switch pid {
		case 5355:
			return processIdentityInfo{Executable: bin, Args: oldArgs, Start: "start"}, nil
		case 5356:
			return processIdentityInfo{Executable: bin, Args: proxyArgs(8787), Start: "start"}, nil
		}
		return processIdentityInfo{}, errors.New("unexpected pid")
	}
	proxyKill = func(*os.Process) error { live = false; killed = true; return nil }
	proxyGone = func(*os.Process) bool { return true }
	proxySpawn = func(cmd *exec.Cmd) error {
		live = true
		spawned = true
		cmd.Process = &os.Process{Pid: 5356}
		return nil
	}
	proxyWrite = func(string, proxyOwnership) error { wrote = true; return nil }
	if err := StartProxy(); err == nil || !strings.Contains(err.Error(), "stale arguments") {
		t.Fatalf("StartProxy error = %v, want stale-arguments refusal", err)
	}
	if killed || spawned || wrote {
		t.Fatalf("stale-args daemon was modified: killed=%v spawned=%v wrote=%v", killed, spawned, wrote)
	}
}

func TestStartProxyRefusesOwnedDaemonWhenPortChanges(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "9123")
	pidFile, _ := proxyFiles()
	oldArgs := proxyArgs(8787)
	if err := writeProxyOwnership(pidFile, proxyOwnership{PID: 5360, Executable: bin, Args: oldArgs, Start: "start"}); err != nil {
		t.Fatal(err)
	}
	spawned := false
	proxyLiveZProbe = func(time.Duration) bool { return false }
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: oldArgs, Start: "start"}, nil
	}
	proxySpawn = func(*exec.Cmd) error {
		spawned = true
		return nil
	}
	if err := StartProxy(); err == nil || !strings.Contains(err.Error(), "stale arguments") {
		t.Fatalf("StartProxy error = %v, want stale-arguments refusal", err)
	}
	if spawned {
		t.Fatal("port change spawned a second proxy")
	}
	raw, ok := util.ReadFileSafe(pidFile)
	if !ok || !strings.Contains(raw, `"pid":5360`) {
		t.Fatalf("old ownership record was overwritten: %s", raw)
	}
}

func TestClearProxyStateCanPreserveRuntimeForHandoff(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	state := util.ProxyRuntime{
		Port:         9123,
		AnthropicURL: "https://anthropic.example",
		OpenAIURL:    "https://openai.example",
		GeminiURL:    "https://gemini.example",
		CloudCodeURL: "https://cloudcode.example",
		Provider:     "apibox",
	}
	if err := util.SaveHeadroomProxyRuntime(state); err != nil {
		t.Fatal(err)
	}
	if err := clearProxyStateWithRuntime("", false); err != nil {
		t.Fatal(err)
	}
	got, ok := util.ReadProxyRuntime()
	if !ok || got != state {
		t.Fatalf("handoff runtime = %+v (ok=%v), want %+v", got, ok, state)
	}
	if err := clearProxyStateWithRuntime("", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := util.ReadProxyRuntime(); ok {
		t.Fatal("normal cleanup retained runtime")
	}
}

func TestStopHeadroomProxyRefusesReplacementRecordCleanup(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	pidFile, _ := proxyFiles()
	original := proxyOwnership{PID: 5361, Executable: bin, Args: proxyArgs(8787), Start: "start"}
	if err := writeProxyOwnership(pidFile, original); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: original.Args, Start: original.Start}, nil
	}
	proxyKill = func(*os.Process) error {
		return writeProxyOwnership(pidFile, proxyOwnership{PID: 5362, Executable: bin, Args: original.Args, Start: "replacement"})
	}
	proxyWait = func(*os.Process) error { return nil }
	proxyGone = func(*os.Process) bool { return true }
	proxyLiveZProbe = func(time.Duration) bool { return false }
	proxyNow = func() time.Time { return time.Unix(100, 0) }

	if err := stopHeadroomDaemonWithRuntime(true); err == nil || !strings.Contains(err.Error(), "replacement record") {
		t.Fatalf("stopHeadroomDaemonWithRuntime error = %v", err)
	}
	if raw, ok := util.ReadFileSafe(pidFile); !ok || !strings.Contains(raw, `"pid":5362`) {
		t.Fatal("replacement ownership record was removed")
	}
}

func TestProxyArgsMatchRecorded(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	bin := proxyTestBin(t)
	pidFile, _ := proxyFiles()
	proxyIdentity = func(int) (processIdentityInfo, error) {
		raw, _ := util.ReadFileSafe(pidFile)
		var record proxyOwnership
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return processIdentityInfo{}, err
		}
		return processIdentityInfo{Executable: record.Executable, Args: record.Args, Start: record.Start}, nil
	}

	if proxyArgsMatchRecorded(8787) {
		t.Fatal("absent record must not claim ownership of hand-started headroom")
	}
	want := proxyArgs(8787)
	if err := writeProxyOwnership(pidFile, proxyOwnership{PID: 5357, Executable: bin, Args: want, Start: "start"}); err != nil {
		t.Fatal(err)
	}
	if !proxyArgsMatchRecorded(8787) {
		t.Fatal("matching record not recognized")
	}
	full := append([]string{bin}, want...)
	if err := writeProxyOwnership(pidFile, proxyOwnership{PID: 5357, Executable: bin, Args: full, Start: "start"}); err != nil {
		t.Fatal(err)
	}
	if !proxyArgsMatchRecorded(8787) {
		t.Fatal("launcher-prefixed ownership args must match bare proxyArgs")
	}
	stale := append(append([]string{}, want[:len(want)-1]...), "bogus")
	if err := writeProxyOwnership(pidFile, proxyOwnership{PID: 5357, Executable: bin, Args: stale, Start: "start"}); err != nil {
		t.Fatal(err)
	}
	if proxyArgsMatchRecorded(8787) {
		t.Fatal("stale args must not match current proxyArgs")
	}
	if err := os.WriteFile(pidFile, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if proxyArgsMatchRecorded(8787) {
		t.Fatal("malformed ownership record must not match current proxyArgs")
	}
	if err := os.WriteFile(pidFile, []byte(`{"pid":5357,"executable":"`+bin+`","args":[],"start_fingerprint":"start"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if proxyArgsMatchRecorded(8787) {
		t.Fatal("empty ownership args must not match current proxyArgs")
	}
}

func TestProxySupervisedStateRequiresMatchingProcess(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	bin := proxyTestBin(t)
	if err := writeProxySupervisedState(5357, bin, proxyArgs(8787), nil, "start"); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{}, errors.New("process gone")
	}
	if proxySupervisedArgsMatch(8787) {
		t.Fatal("stale supervised state must not claim ownership")
	}
}

func TestStopSupervisedDaemonRefusesMismatchedProcess(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	bin := proxyTestBin(t)
	if err := writeProxySupervisedState(5358, bin, proxyArgs(8787), nil, "start"); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: []string{"other"}}, nil
	}
	proxyGone = func(*os.Process) bool { return false }
	killed := false
	proxyKill = func(*os.Process) error { killed = true; return nil }
	if err := stopSupervisedDaemon(); err == nil {
		t.Fatal("stopSupervisedDaemon accepted mismatched process")
	}
	if killed {
		t.Fatal("mismatched supervised process was killed")
	}
}

func TestStopSupervisedDaemonClearsStateAfterStop(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	bin := proxyTestBin(t)
	if err := writeProxySupervisedState(5359, bin, proxyArgs(8787), nil, "start"); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: proxyArgs(8787), Start: "start"}, nil
	}
	proxyKill = func(*os.Process) error { return nil }
	proxyGone = func(*os.Process) bool { return true }
	proxyLiveZProbe = func(time.Duration) bool { return false }
	if err := stopSupervisedDaemon(); err != nil {
		t.Fatalf("stopSupervisedDaemon = %v", err)
	}
	if _, ok := util.ReadFileSafe(proxySupervisedFile()); ok {
		t.Fatal("supervised state survived successful stop")
	}
}

func TestStartProxyDoesNotReuseNonHeadroomProbe(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	proxyLiveZProbe = func(time.Duration) bool { return false }
	spawned := false
	proxySpawn = func(cmd *exec.Cmd) error { spawned = true; cmd.Process = &os.Process{Pid: 5353}; return nil }
	proxyIdentity = func(int) (processIdentityInfo, error) { return processIdentityInfo{}, errors.New("not headroom") }
	proxyGone = func(*os.Process) bool { return true }
	proxyKill = func(*os.Process) error { return nil }
	if err := StartProxy(); err == nil {
		t.Fatal("StartProxy reused non-headroom probe")
	}
	if !spawned {
		t.Fatal("StartProxy did not attempt spawn after non-headroom probe")
	}
}

func TestStartProxyRefusesLiveUnownedProxy(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	proxyTestBin(t)
	proxyLiveZProbe = func(time.Duration) bool { return true }
	proxySpawn = func(*exec.Cmd) error {
		t.Fatal("live unowned proxy must not be replaced")
		return nil
	}
	if err := StartProxy(); err == nil || !strings.Contains(err.Error(), "without tokless ownership") {
		t.Fatalf("StartProxy error = %v, want unowned-proxy refusal", err)
	}
}

func TestProxyDaemonEnvSetsResolveTimeout(t *testing.T) {
	t.Setenv("HEADROOM_UPSTREAM_RESOLVE_TIMEOUT_S", "")
	got := proxyDaemonEnv()
	found := ""
	for _, value := range got {
		if strings.HasPrefix(value, "HEADROOM_UPSTREAM_RESOLVE_TIMEOUT_S=") {
			if found != "" {
				t.Fatalf("duplicate resolve timeout: %q and %q", found, value)
			}
			found = value
		}
	}
	if found != "HEADROOM_UPSTREAM_RESOLVE_TIMEOUT_S="+headroomUpstreamResolveTimeout {
		t.Fatalf("resolve timeout = %q, want %q", found, "HEADROOM_UPSTREAM_RESOLVE_TIMEOUT_S="+headroomUpstreamResolveTimeout)
	}

	t.Setenv("HEADROOM_UPSTREAM_RESOLVE_TIMEOUT_S", "12")
	got = proxyDaemonEnv()
	found = ""
	for _, value := range got {
		if strings.HasPrefix(value, "HEADROOM_UPSTREAM_RESOLVE_TIMEOUT_S=") {
			if found != "" {
				t.Fatalf("duplicate resolve timeout: %q and %q", found, value)
			}
			found = value
		}
	}
	if found != "HEADROOM_UPSTREAM_RESOLVE_TIMEOUT_S=12" {
		t.Fatalf("resolve timeout override = %q, want HEADROOM_UPSTREAM_RESOLVE_TIMEOUT_S=12", found)
	}
}

func TestProxyPortIgnoresInvalidEnv(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	for _, raw := range []string{"abc", "-1", "0", "70000", " 8a "} {
		t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", raw)
		if got := ProxyPort(); got != 8787 {
			t.Fatalf("ProxyPort(%q) = %d, want 8787", raw, got)
		}
	}
}

func TestProxyOpenAIURL(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "9123")
	if got := ProxyOpenAIURL(); got != "http://127.0.0.1:9123/v1" {
		t.Fatalf("ProxyOpenAIURL = %q, want http://127.0.0.1:9123/v1", got)
	}
}

func TestProxyUpstreamURLsDefaultsAndEnv(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "")
	t.Setenv("TOKLESS_HEADROOM_OPENAI_URL", "")
	a, o := ProxyUpstreamURLs()
	if a != defaultAnthropicTargetURL || o != defaultOpenAITargetURL {
		t.Fatalf("defaults = %q, %q", a, o)
	}
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "https://custom.anthropic")
	t.Setenv("TOKLESS_HEADROOM_OPENAI_URL", "https://custom.openai/v1")
	a, o = ProxyUpstreamURLs()
	if a != "https://custom.anthropic" || o != "https://custom.openai/v1" {
		t.Fatalf("overrides = %q, %q", a, o)
	}
}

func TestProxyArgsFromRuntimeIgnoresAmbientEnvironment(t *testing.T) {
	isolateProxyOps(t)
	util.SetHomeOverride(t.TempDir())
	if err := util.SaveHeadroomProxyRuntime(util.ProxyRuntime{
		Port:         19787,
		AnthropicURL: "https://stored.anthropic",
		OpenAIURL:    "https://stored.openai",
		GeminiURL:    "https://stored.gemini",
		CloudCodeURL: "https://stored.cloudcode",
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "9123")
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "https://ambient.anthropic")
	args := proxyArgsFromRuntime(proxyPortFromRuntime())
	want := []string{"--port", "19787", "--anthropic-api-url", "https://stored.anthropic", "--openai-api-url", "https://stored.openai", "--gemini-api-url", "https://stored.gemini", "--cloudcode-api-url", "https://stored.cloudcode"}
	for _, value := range want {
		if !proxyArgsContains(args, value) {
			t.Fatalf("runtime args missing %q: %v", value, args)
		}
	}
}

func proxyArgsContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestResolveHeadroomBinPrefersManaged(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveHeadroomBin(); got != bin {
		t.Fatalf("ResolveHeadroomBin = %q, want %q", got, bin)
	}
	_ = os.Remove(bin)
	if got := ResolveHeadroomBin(); got != "" {
		t.Fatalf("ResolveHeadroomBin without managed bin = %q, want empty", got)
	}
}

func TestProxyDaemonEnvIncludesAllowedBaseURLs(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{
		{ID: "opencode:test", Token: "testtoken", Protocol: "openai-chat", Upstream: "https://api.testprovider.com/v1"},
	}); err != nil {
		t.Fatal(err)
	}
	env := proxyDaemonEnv()
	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "HEADROOM_ALLOWED_BASE_URLS=") {
			found = true
			if !strings.Contains(kv, "https://api.testprovider.com") {
				t.Fatalf("HEADROOM_ALLOWED_BASE_URLS missing upstream exact origin: %s", kv)
			}
			if !strings.Contains(kv, "http://127.0.0.1:18787") {
				t.Fatalf("HEADROOM_ALLOWED_BASE_URLS missing byok gateway exact origin: %s", kv)
			}
			parts := strings.Split(strings.TrimPrefix(kv, "HEADROOM_ALLOWED_BASE_URLS="), ",")
			for _, part := range parts {
				if part == "127.0.0.1" || part == "localhost" {
					t.Fatalf("HEADROOM_ALLOWED_BASE_URLS contains over-permissive bare host: %s", part)
				}
				if part == "api.testprovider.com" {
					t.Fatalf("HEADROOM_ALLOWED_BASE_URLS contains bare hostname without scheme: %s", part)
				}
			}
		}
	}
	if !found {
		t.Fatalf("HEADROOM_ALLOWED_BASE_URLS not in proxyDaemonEnv: %v", env)
	}
}

func TestHeadroomUpstreamGuardExactOriginAndSlowDNSBypass(t *testing.T) {
	python := filepath.Join(util.HeadroomPathsResolved().Tools, "headroom-ai", "bin", "python3")
	if _, err := os.Stat(python); err != nil {
		t.Skip("headroom python not found")
	}
	script := `
import os, sys
from concurrent.futures import TimeoutError

os.environ["HEADROOM_ALLOWED_BASE_URLS"] = "http://127.0.0.1:18787,http://localhost:18787,https://api.customprovider.ai"
from headroom.proxy.upstream_guard import is_safe_upstream_url, _allowlisted_destinations
import headroom.proxy.upstream_guard as guard

# Mock resolver pool so any DNS call raises TimeoutError (simulating 6s timeout)
class SlowResolver:
    def submit(self, *args, **kwargs):
        class Future:
            def result(self, timeout=None):
                raise TimeoutError("DNS resolution timed out")
        return Future()

guard._RESOLVER_POOL = SlowResolver()

# 1. Allowlisted exact origins MUST succeed without hitting resolver
assert is_safe_upstream_url("https://api.customprovider.ai") is True, "exact origin https://api.customprovider.ai must pass"
assert is_safe_upstream_url("https://api.customprovider.ai/api/v1/chat/completions") is True, "subpath must pass"
assert is_safe_upstream_url("http://127.0.0.1:18787") is True, "gateway loopback must pass"

# 2. Insecure or wrong-port variants MUST be rejected
assert is_safe_upstream_url("http://api.customprovider.ai") is False, "insecure http variant must be rejected"
assert is_safe_upstream_url("http://127.0.0.1:22") is False, "arbitrary loopback port must be rejected"
assert is_safe_upstream_url("http://127.0.0.1:80") is False, "arbitrary loopback port 80 must be rejected"

# 3. Unallowlisted host must hit resolver and fail closed when DNS times out
assert is_safe_upstream_url("https://unallowlisted.slowdns.example.com") is False, "unallowlisted slow DNS host must fail closed"

print("PASS: Upstream guard exact origin and slow DNS bypass verified")
`
	cmd := exec.Command(python, "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python upstream guard test failed: %v\noutput:\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "PASS: Upstream guard exact origin and slow DNS bypass verified") {
		t.Fatalf("unexpected output: %s", string(out))
	}
}

func TestStartProxyRestartsWhenAllowedBaseURLsChange(t *testing.T) {
	isolateProxyOps(t)
	byokGatewayState.Lock()
	oldListener := byokGatewayState.listener
	byokGatewayState.listener = &net.TCPListener{}
	byokGatewayState.Unlock()
	t.Cleanup(func() {
		byokGatewayState.Lock()
		byokGatewayState.listener = oldListener
		byokGatewayState.Unlock()
	})
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	bin := proxyTestBin(t)
	pidFile, _ := proxyFiles()
	args := proxyArgs(8787)
	oldAllowed := "http://127.0.0.1:18787,http://localhost:18787,https://old.provider.com"
	if err := writeProxyOwnership(pidFile, proxyOwnership{
		PID:             5370,
		Executable:      bin,
		Args:            args,
		Start:           "start",
		AllowedBaseURLs: oldAllowed,
	}); err != nil {
		t.Fatal(err)
	}
	// Add a new route so headroomAllowedBaseURLs() differs from oldAllowed
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{
		{ID: "opencode:new", Token: "newtoken", Protocol: "openai-chat", Upstream: "https://new.provider.com/v1"},
	}); err != nil {
		t.Fatal(err)
	}
	live := true
	killed, spawned, wrote := false, false, false
	proxyLiveZProbe = func(time.Duration) bool { return live }
	proxyIdentity = func(pid int) (processIdentityInfo, error) {
		switch pid {
		case os.Getpid(), 5370, 5371:
			return processIdentityInfo{Executable: bin, Args: args, Start: "start"}, nil
		}
		return processIdentityInfo{}, errors.New("unexpected pid")
	}
	proxyKill = func(p *os.Process) error {
		if p.Pid == 5370 {
			killed = true
			live = false
			return nil
		}
		return nil
	}
	proxyGone = func(*os.Process) bool { return true }
	proxySpawn = func(cmd *exec.Cmd) error {
		live = true
		spawned = true
		cmd.Process = &os.Process{Pid: 5371}
		return nil
	}
	proxyWrite = func(path string, record proxyOwnership) error {
		wrote = true
		if !strings.Contains(record.AllowedBaseURLs, "https://new.provider.com") {
			t.Fatalf("restarted proxy recorded stale allowed urls: %s", record.AllowedBaseURLs)
		}
		return nil
	}
	if err := StartProxy(); err != nil {
		t.Fatalf("StartProxy with updated allowlist failed: %v", err)
	}
	if !killed {
		t.Fatalf("expected old daemon to be killed on allowlist update")
	}
	if !spawned {
		t.Fatalf("expected new daemon to be spawned with updated allowlist")
	}
	if !wrote {
		t.Fatalf("expected new ownership record to be written with updated allowlist")
	}
}

func TestCanonicalAllowlistOriginRejectsInvalidEntries(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"https://api.customprovider.ai", "https://api.customprovider.ai", false},
		{"https://api.customprovider.ai:443", "https://api.customprovider.ai", false},
		{"HTTPS://API.CustomProvider.AI:443/", "https://api.customprovider.ai", false},
		{"http://127.0.0.1:18787", "http://127.0.0.1:18787", false},
		{"http://localhost:80", "http://localhost", false},
		{"http://localhost:8080", "http://localhost:8080", false},
		{"ws://127.0.0.1:80", "ws://127.0.0.1", false},
		{"ws://127.0.0.1:8080", "ws://127.0.0.1:8080", false},
		{"wss://api.customprovider.ai:443", "wss://api.customprovider.ai", false},
		{"wss://api.customprovider.ai:9443", "wss://api.customprovider.ai:9443", false},
		{"http://[::1]:8080", "http://[::1]:8080", false},
		{"http://[::1]:80", "http://[::1]", false},
		{"https://[2001:db8::1]:443", "https://[2001:db8::1]", false},
		{"https://[2001:db8::1]:8443", "https://[2001:db8::1]:8443", false},
		{"https://user:pass@api.customprovider.ai", "", true},
		{"http://user@127.0.0.1:18787", "", true},
		{"https://api.customprovider.ai?param=1", "", true},
		{"https://api.customprovider.ai#section", "", true},
		{"https://api.customprovider.ai/other/subpath", "", true},
		{"bare-host.com", "", true},
		{"ftp://example.com", "", true},
		{"file:///local/path", "", true},
		{"https://", "", true},
		{"", "", true},
		{"://bad-url", "", true},
		{"https://api.customprovider.ai:", "", true},
		{"https://api.customprovider.ai:0", "", true},
		{"https://api.customprovider.ai:65536", "", true},
		{"https://api.customprovider.ai:invalidport", "", true},
		{"http://[::1%eth0]:8080", "", true},
		{"http://[invalid-ipv6]:8080", "", true},
		{"http://host with spaces:8080", "", true},
	}
	for _, tc := range cases {
		got, err := canonicalAllowlistOrigin(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("canonicalAllowlistOrigin(%q) err = %v, wantErr = %v", tc.input, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("canonicalAllowlistOrigin(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStartProxyFailsClosedOnCorruptRouteRegistry(t *testing.T) {
	isolateProxyOps(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	routesFile := filepath.Join(home, ".local", "share", "tokless", "headroom", "byok.routes.json")
	if err := os.MkdirAll(filepath.Dir(routesFile), 0o755); err != nil {
		t.Fatal(err)
	}
	// Write invalid corrupted JSON
	if err := os.WriteFile(routesFile, []byte("{corrupt-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := StartProxy()
	if err == nil || !strings.Contains(err.Error(), "failed to parse configured BYOK route registry") {
		t.Fatalf("StartProxy on corrupt routes want fail-closed error, got: %v", err)
	}
}

func TestStartProxyRestartsSupervisedDaemonWhenAllowlistChanges(t *testing.T) {
	isolateProxyOps(t)
	byokGatewayState.Lock()
	oldListener := byokGatewayState.listener
	byokGatewayState.listener = &net.TCPListener{}
	byokGatewayState.Unlock()
	t.Cleanup(func() {
		byokGatewayState.Lock()
		byokGatewayState.listener = oldListener
		byokGatewayState.Unlock()
	})
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	bin := proxyTestBin(t)
	args := proxyArgs(8787)
	oldAllowed := "http://127.0.0.1:18787,http://localhost:18787,https://old.provider.com"

	// Supervised state with old allowlist
	if err := writeProxySupervisedState(5380, bin, args, nil, "start"); err != nil {
		t.Fatal(err)
	}
	// Overwrite supervised state file with oldAllowed explicitly
	supFile := proxySupervisedFile()
	raw, _ := os.ReadFile(supFile)
	var st proxySupervisedState
	_ = json.Unmarshal(raw, &st)
	st.AllowedBaseURLs = oldAllowed
	b, _ := json.Marshal(st)
	_ = os.WriteFile(supFile, b, 0o600)

	// Add new route
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{
		{ID: "opencode:supervised-test", Token: "tok", Protocol: "openai-chat", Upstream: "https://supervised.provider.com/v1"},
	}); err != nil {
		t.Fatal(err)
	}

	live := true
	killed, spawned, wrote := false, false, false
	proxyLiveZProbe = func(time.Duration) bool { return live }
	proxyIdentity = func(pid int) (processIdentityInfo, error) {
		switch pid {
		case os.Getpid(), 5380:
			return processIdentityInfo{Executable: bin, Args: args, Start: "start"}, nil
		case 5381:
			return processIdentityInfo{Executable: bin, Args: args, Start: "start"}, nil
		}
		return processIdentityInfo{}, errors.New("unexpected pid")
	}
	proxyKill = func(p *os.Process) error {
		if p.Pid == 5380 {
			killed = true
			live = false
			return nil
		}
		return nil
	}
	proxyGone = func(*os.Process) bool { return true }
	proxySpawn = func(cmd *exec.Cmd) error {
		live = true
		spawned = true
		cmd.Process = &os.Process{Pid: 5381}
		return nil
	}
	proxyWrite = func(path string, record proxyOwnership) error {
		wrote = true
		return nil
	}

	if err := StartProxy(); err != nil {
		t.Fatalf("StartProxy with updated allowlist on supervised daemon failed: %v", err)
	}
	if !killed {
		t.Fatalf("expected old supervised daemon to be stopped on allowlist update")
	}
	if !spawned {
		t.Fatalf("expected new daemon to be spawned with updated allowlist")
	}
	if !wrote {
		t.Fatalf("expected new ownership record to be written with updated allowlist")
	}
}
