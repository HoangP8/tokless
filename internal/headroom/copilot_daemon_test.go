package headroom

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

func isolateCopilotProxyOps(t *testing.T) {
	t.Helper()
	util.SetHomeOverride(t.TempDir())
	t.Setenv("TOKLESS_COPILOT_PROXY_PORT", "")
	oldProbe, oldSpawn := copilotProxyLiveProbe, proxySpawn
	oldIdentity, oldKill := proxyIdentity, proxyKill
	oldWrite, oldGone := proxyWrite, proxyGone
	oldWait, oldSleep, oldNow := proxyWait, proxySleep, proxyNow
	t.Cleanup(func() {
		copilotProxyLiveProbe, proxySpawn = oldProbe, oldSpawn
		proxyIdentity, proxyKill = oldIdentity, oldKill
		proxyWrite, proxyGone = oldWrite, oldGone
		proxyWait, proxySleep, proxyNow = oldWait, oldSleep, oldNow
		util.SetHomeOverride("")
	})
}

func TestCopilotProxyArgsUseDedicatedPort(t *testing.T) {
	isolateCopilotProxyOps(t)
	t.Setenv("TOKLESS_COPILOT_PROXY_PORT", "9128")
	if got, want := copilotProxyArgs(), []string{"wrap", "vscode", "--port", "9128", "--no-configure"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("copilotProxyArgs = %#v, want %#v", got, want)
	}
}

func TestStartCopilotProxyRecordsOwnedWrapper(t *testing.T) {
	isolateCopilotProxyOps(t)
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	spawned := []string{}
	proxySpawn = func(cmd *exec.Cmd) error {
		spawned = append([]string{cmd.Path}, cmd.Args[1:]...)
		cmd.Process = &os.Process{Pid: 4128}
		return nil
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: copilotProxyArgs(), Start: "start"}, nil
	}
	proxyWrite = func(path string, record proxyOwnership) error {
		return writeProxyOwnership(path, record)
	}
	readyChecks := 0
	copilotProxyLiveProbe = func(time.Duration) bool {
		readyChecks++
		return readyChecks > 1
	}
	proxySleep = func(time.Duration) {}
	proxyNow = func() time.Time { return time.Unix(100, 0) }

	if err := StartCopilotProxy(); err != nil {
		t.Fatalf("StartCopilotProxy = %v", err)
	}
	if !reflect.DeepEqual(spawned, append([]string{bin}, copilotProxyArgs()...)) {
		t.Fatalf("spawned = %#v", spawned)
	}
	if !CopilotProxyOwned() {
		t.Fatal("started wrapper not reported as owned")
	}
}

func TestStartCopilotProxyStopsWhenIdentityChangesDuringReadiness(t *testing.T) {
	isolateCopilotProxyOps(t)
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	proxySpawn = func(cmd *exec.Cmd) error {
		cmd.Process = &os.Process{Pid: 4127}
		return nil
	}
	identityCalls := 0
	proxyIdentity = func(int) (processIdentityInfo, error) {
		identityCalls++
		if identityCalls == 1 {
			return processIdentityInfo{Executable: bin, Args: copilotProxyArgs(), Start: "start-1"}, nil
		}
		return processIdentityInfo{Executable: bin, Args: copilotProxyArgs(), Start: "start-2"}, nil
	}
	killed := false
	proxyKill = func(*os.Process) error { killed = true; return nil }
	proxyWait = func(*os.Process) error { return nil }
	proxyNow = func() time.Time { return time.Unix(100, 0) }

	if err := StartCopilotProxy(); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("StartCopilotProxy error = %v", err)
	}
	if killed {
		t.Fatal("identity change killed replacement process")
	}
}

func TestStartCopilotProxyCleansUpWhenIdentityNeverSurfaces(t *testing.T) {
	isolateCopilotProxyOps(t)
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	proxySpawn = func(cmd *exec.Cmd) error {
		cmd.Process = &os.Process{Pid: 4126}
		return nil
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{}, errors.New("identity unavailable")
	}
	killed := false
	proxyKill = func(*os.Process) error { killed = true; return nil }
	proxyWait = func(*os.Process) error { return nil }
	proxyGone = func(*os.Process) bool { return true }
	copilotProxyLiveProbe = func(time.Duration) bool { return false }
	now := time.Unix(100, 0)
	proxyNow = func() time.Time { return now }
	proxySleep = func(time.Duration) { now = now.Add(proxyPollInterval) }

	if err := StartCopilotProxy(); err == nil || !strings.Contains(err.Error(), "could not be verified") {
		t.Fatalf("StartCopilotProxy error = %v", err)
	}
	if !killed {
		t.Fatal("unknown-identity direct child was not cleaned up")
	}
}

func TestStartCopilotProxyRefusesUnownedHeadroomListener(t *testing.T) {
	isolateCopilotProxyOps(t)
	proxyTestBin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(proxyTestBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proxyTestBin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	copilotProxyLiveProbe = func(time.Duration) bool { return true }
	proxySpawn = func(*exec.Cmd) error { t.Fatal("unowned listener must not be replaced"); return nil }
	if err := StartCopilotProxy(); err == nil || !strings.Contains(err.Error(), "unverified process") {
		t.Fatalf("StartCopilotProxy error = %v", err)
	}
}

func TestCopilotRollbackDoesNotClearSharedProxyState(t *testing.T) {
	isolateCopilotProxyOps(t)
	shared := filepath.Join(util.HeadroomPathsResolved().Root, "proxy.supervised.json")
	if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte("shared"), 0o600); err != nil {
		t.Fatal(err)
	}
	proxyKill = func(*os.Process) error { return nil }
	proxyWait = func(*os.Process) error { return nil }

	if err := rollbackCopilotProxy(&os.Process{Pid: 4131}, filepath.Join(t.TempDir(), "copilot.pid"), nil, errors.New("startup failed")); err == nil {
		t.Fatal("rollbackCopilotProxy returned nil")
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatalf("shared proxy state removed: %v", err)
	}
}

func TestCopilotRollbackRetainsRecordWhenDetachedProcessSurvives(t *testing.T) {
	isolateCopilotProxyOps(t)
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile, _ := copilotProxyFiles()
	record := proxyOwnership{PID: 4132, Executable: bin, Args: copilotProxyArgs(), Start: "start"}
	if err := writeProxyOwnership(pidFile, record); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: copilotProxyArgs(), Start: "start"}, nil
	}
	proxyKill = func(*os.Process) error { return nil }
	proxyWait = func(*os.Process) error { return errors.New("not a child") }
	proxyGone = func(*os.Process) bool { return false }
	copilotProxyLiveProbe = func(time.Duration) bool { return false }
	now := time.Unix(100, 0)
	proxyNow = func() time.Time { return now }
	proxySleep = func(time.Duration) { now = now.Add(proxyStopTimeout) }

	if err := rollbackCopilotProxy(&os.Process{Pid: record.PID}, pidFile, nil, errors.New("startup failed")); err == nil || !strings.Contains(err.Error(), "did not stop") {
		t.Fatalf("rollbackCopilotProxy error = %v", err)
	}
	if _, ok := util.ReadFileSafe(pidFile); !ok {
		t.Fatal("Copilot ownership record removed while process remained alive")
	}
}

func TestStopCopilotProxyRequiresMatchingOwner(t *testing.T) {
	isolateCopilotProxyOps(t)
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile, _ := copilotProxyFiles()
	if err := writeProxyOwnership(pidFile, proxyOwnership{PID: 4129, Executable: bin, Args: copilotProxyArgs(), Start: "start"}); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: []string{"foreign"}, Start: "start"}, nil
	}
	proxyGone = func(*os.Process) bool { return false }
	killed := false
	proxyKill = func(*os.Process) error { killed = true; return nil }
	if err := StopCopilotProxy(); err == nil {
		t.Fatal("StopCopilotProxy accepted mismatched owner")
	}
	if killed {
		t.Fatal("mismatched process was killed")
	}
}

func TestStopCopilotProxyRefusesReplacementRecordCleanup(t *testing.T) {
	isolateCopilotProxyOps(t)
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile, _ := copilotProxyFiles()
	original := proxyOwnership{PID: 4133, Executable: bin, Args: copilotProxyArgs(), Start: "start"}
	if err := writeProxyOwnership(pidFile, original); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: copilotProxyArgs(), Start: "start"}, nil
	}
	proxyKill = func(*os.Process) error {
		return writeProxyOwnership(pidFile, proxyOwnership{PID: 4134, Executable: bin, Args: copilotProxyArgs(), Start: "replacement"})
	}
	proxyWait = func(*os.Process) error { return nil }
	proxyGone = func(*os.Process) bool { return true }
	copilotProxyLiveProbe = func(time.Duration) bool { return false }
	proxyNow = func() time.Time { return time.Unix(100, 0) }

	if err := StopCopilotProxy(); err == nil || !strings.Contains(err.Error(), "replacement record") {
		t.Fatalf("StopCopilotProxy error = %v", err)
	}
	if raw, ok := util.ReadFileSafe(pidFile); !ok || !strings.Contains(raw, `"pid":4134`) {
		t.Fatal("replacement ownership record was removed")
	}
}

func TestEnsureCopilotProxyUpSkipsWhenUnwired(t *testing.T) {
	isolateCopilotProxyOps(t)
	oldWired, oldLock := copilotVSCodeWired, acquireCopilotLifecycleLock
	t.Cleanup(func() {
		copilotVSCodeWired, acquireCopilotLifecycleLock = oldWired, oldLock
	})
	started := 0
	proxySpawn = func(*exec.Cmd) error {
		started++
		return errors.New("must not spawn")
	}
	copilotVSCodeWired = func() bool { return false }
	acquireCopilotLifecycleLock = func() (func(), error) { return func() {}, nil }
	EnsureCopilotProxyUp()
	if started != 0 {
		t.Fatalf("spawned when unwired: %d", started)
	}
}

func TestEnsureCopilotProxyUpSkipsWhenRoutingDisabled(t *testing.T) {
	isolateCopilotProxyOps(t)
	if err := util.SetProxyRoutingEnabled(false); err != nil {
		t.Fatal(err)
	}
	oldWired, oldLock := copilotVSCodeWired, acquireCopilotLifecycleLock
	t.Cleanup(func() {
		copilotVSCodeWired, acquireCopilotLifecycleLock = oldWired, oldLock
	})
	copilotVSCodeWired = func() bool { t.Fatal("must not check wiring when routing disabled"); return true }
	acquireCopilotLifecycleLock = func() (func(), error) { t.Fatal("must not lock when routing disabled"); return nil, nil }
	EnsureCopilotProxyUp()
}

func TestEnsureCopilotProxyUpStartsWhenWired(t *testing.T) {
	isolateCopilotProxyOps(t)
	oldWired, oldLock := copilotVSCodeWired, acquireCopilotLifecycleLock
	t.Cleanup(func() {
		copilotVSCodeWired, acquireCopilotLifecycleLock = oldWired, oldLock
	})
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	spawned := 0
	proxySpawn = func(cmd *exec.Cmd) error {
		spawned++
		cmd.Process = &os.Process{Pid: 4242}
		return nil
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: copilotProxyArgs(), Start: "start"}, nil
	}
	proxyWrite = func(path string, record proxyOwnership) error {
		return writeProxyOwnership(path, record)
	}
	ready := 0
	copilotProxyLiveProbe = func(time.Duration) bool {
		ready++
		return ready > 1
	}
	proxySleep = func(time.Duration) {}
	proxyNow = func() time.Time { return time.Unix(100, 0) }
	copilotVSCodeWired = func() bool { return true }
	acquireCopilotLifecycleLock = func() (func(), error) { return func() {}, nil }
	EnsureCopilotProxyUp()
	if spawned != 1 {
		t.Fatalf("spawned = %d, want 1", spawned)
	}
}

func TestEnsureCopilotProxyUpRechecksWireUnderLifecycleLock(t *testing.T) {
	isolateCopilotProxyOps(t)
	oldWired, oldLock := copilotVSCodeWired, acquireCopilotLifecycleLock
	t.Cleanup(func() {
		copilotVSCodeWired, acquireCopilotLifecycleLock = oldWired, oldLock
	})
	held := make(chan struct{})
	releaseCh := make(chan struct{})
	acquireCopilotLifecycleLock = func() (func(), error) {
		close(held)
		<-releaseCh
		return func() {}, nil
	}
	wiredChecks := 0
	copilotVSCodeWired = func() bool {
		wiredChecks++
		return false // unwire completed while Ensure waited on lock
	}
	spawned := 0
	proxySpawn = func(*exec.Cmd) error {
		spawned++
		return errors.New("must not spawn after down")
	}
	done := make(chan struct{})
	go func() {
		EnsureCopilotProxyUp()
		close(done)
	}()
	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatal("Ensure did not acquire lifecycle lock")
	}
	close(releaseCh)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Ensure hung")
	}
	if wiredChecks == 0 {
		t.Fatal("wire check must run under lock")
	}
	if spawned != 0 {
		t.Fatalf("spawned after concurrent down: %d", spawned)
	}
}

func TestEnsureCopilotProxyUpSkipsWhenLockFails(t *testing.T) {
	isolateCopilotProxyOps(t)
	oldWired, oldLock := copilotVSCodeWired, acquireCopilotLifecycleLock
	t.Cleanup(func() {
		copilotVSCodeWired, acquireCopilotLifecycleLock = oldWired, oldLock
	})
	acquireCopilotLifecycleLock = func() (func(), error) { return nil, errors.New("busy") }
	copilotVSCodeWired = func() bool { t.Fatal("must not check wire without lock"); return true }
	proxySpawn = func(*exec.Cmd) error { t.Fatal("must not spawn"); return nil }
	EnsureCopilotProxyUp()
}

func TestStopCopilotProxyStopsMatchingOwner(t *testing.T) {
	isolateCopilotProxyOps(t)
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile, _ := copilotProxyFiles()
	if err := writeProxyOwnership(pidFile, proxyOwnership{PID: 4130, Executable: bin, Args: copilotProxyArgs(), Start: "start"}); err != nil {
		t.Fatal(err)
	}
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: copilotProxyArgs(), Start: "start"}, nil
	}
	proxyKill = func(*os.Process) error { return nil }
	proxyGone = func(*os.Process) bool { return true }
	copilotProxyLiveProbe = func(time.Duration) bool { return false }
	proxySleep = func(time.Duration) {}
	proxyNow = func() time.Time { return time.Unix(100, 0) }
	proxyWait = func(*os.Process) error { return errors.New("already exited") }
	if err := StopCopilotProxy(); err != nil {
		t.Fatalf("StopCopilotProxy = %v", err)
	}
	if _, ok := util.ReadFileSafe(pidFile); ok {
		t.Fatal("Copilot ownership record survived stop")
	}
}
