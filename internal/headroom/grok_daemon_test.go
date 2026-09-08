package headroom

import (
	"encoding/json"
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

func TestGrokOwnershipValidCases(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	pidFile, _ := grokProxyFiles()
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
		t.Fatal(err)
	}

	if grokOwnershipValid() {
		t.Fatal("missing record must be invalid")
	}
	if err := os.WriteFile(pidFile, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if grokOwnershipValid() {
		t.Fatal("garbage record must be invalid")
	}
	if err := os.WriteFile(pidFile, []byte(`{"pid":999999,"executable":"/nonexistent","args":["proxy"],"start_fingerprint":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if grokOwnershipValid() {
		t.Fatal("dead-pid record must be invalid")
	}
}

func TestStopGrokOAuthProxyRetainsRecordForHealthyUnownedListener(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Setenv("TOKLESS_GROK_PROXY_PORT", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/livez" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"service": "headroom-proxy"})
	}))
	defer server.Close()
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	t.Setenv("TOKLESS_GROK_PROXY_PORT", port)

	pidFile, _ := grokProxyFiles()
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidFile, []byte(`{"pid":999999,"executable":"/nonexistent","args":["proxy"],"start_fingerprint":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldIdentity, oldGone := proxyIdentity, proxyGone
	proxyIdentity = func(int) (processIdentityInfo, error) { return processIdentityInfo{}, os.ErrNotExist }
	proxyGone = func(*os.Process) bool { return true }
	t.Cleanup(func() { proxyIdentity, proxyGone = oldIdentity, oldGone })

	if err := StopGrokOAuthProxy(); err == nil || !strings.Contains(err.Error(), "healthy listener remains") {
		t.Fatalf("StopGrokOAuthProxy error = %v", err)
	}
	if _, ok := util.ReadFileSafe(pidFile); !ok {
		t.Fatal("ownership record removed beside healthy listener")
	}
}

func TestStartGrokOAuthProxyRetainsRecordWhenRollbackProcessSurvives(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	t.Setenv("TOKLESS_GROK_PROXY_PORT", "")
	proxySpawn = func(cmd *exec.Cmd) error {
		cmd.Process = &os.Process{Pid: 4247}
		return nil
	}
	args := grokProxyArgs(util.GrokOAuthProxyPort())
	proxyIdentity = func(int) (processIdentityInfo, error) {
		return processIdentityInfo{Executable: bin, Args: args, Start: "start"}, nil
	}
	proxyKill = func(*os.Process) error { return nil }
	proxyWait = func(*os.Process) error { return os.ErrProcessDone }
	proxyGone = func(*os.Process) bool { return false }
	proxyLiveZProbe = func(time.Duration) bool { return false }
	now := time.Unix(100, 0)
	proxyNow = func() time.Time { return now }
	proxySleep = func(time.Duration) { now = now.Add(proxyReadyTimeout) }

	if err := StartGrokOAuthProxy(); err == nil || !strings.Contains(err.Error(), "did not stop") {
		t.Fatalf("StartGrokOAuthProxy error = %v", err)
	}
	pidFile, _ := grokProxyFiles()
	if _, ok := util.ReadFileSafe(pidFile); !ok {
		t.Fatal("rollback removed ownership record while process remained alive")
	}
}

func TestGrokRollbackRefusesVerifiedReplacementIdentity(t *testing.T) {
	isolateProxyOps(t)
	proc := &os.Process{Pid: 4249}
	expected := processIdentityInfo{Executable: "/bin/headroom", Args: []string{"proxy"}, Start: "original"}
	identity := processIdentityInfo{Executable: "/bin/replacement", Args: []string{"proxy"}, Start: "replacement"}
	killed := false
	proxyIdentity = func(int) (processIdentityInfo, error) { return identity, nil }
	proxyKill = func(*os.Process) error { killed = true; return nil }

	if err := grokRollbackWithIdentity(proc, "", &expected, os.ErrInvalid); err == nil {
		t.Fatal("grokRollbackWithIdentity returned nil")
	}
	if killed {
		t.Fatal("verified replacement process was killed")
	}
}

func TestStartGrokOAuthProxyRefusesIdentityChangeDuringReadiness(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	args := grokProxyArgs(util.GrokOAuthProxyPort())
	proxySpawn = func(cmd *exec.Cmd) error { cmd.Process = &os.Process{Pid: 4250}; return nil }
	identityCalls := 0
	proxyIdentity = func(int) (processIdentityInfo, error) {
		identityCalls++
		start := "start"
		if identityCalls > 1 {
			start = "changed"
		}
		return processIdentityInfo{Executable: bin, Args: append([]string(nil), args...), Start: start}, nil
	}
	proxyKill = func(*os.Process) error { return nil }
	proxyWait = func(*os.Process) error { return os.ErrProcessDone }
	proxyGone = func(*os.Process) bool { return true }
	proxyLiveZProbe = func(time.Duration) bool { return false }
	if err := StartGrokOAuthProxy(); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("StartGrokOAuthProxy error = %v, want readiness identity refusal", err)
	}
}

func TestStartGrokOAuthProxyCleansUpWhenIdentityNeverSurfaces(t *testing.T) {
	isolateProxyOps(t)
	bin := proxyTestBin(t)
	proxySpawn = func(cmd *exec.Cmd) error { cmd.Process = &os.Process{Pid: 4251}; return nil }
	proxyIdentity = func(int) (processIdentityInfo, error) { return processIdentityInfo{}, os.ErrNotExist }
	killed, waited := false, false
	proxyKill = func(*os.Process) error { killed = true; return nil }
	proxyWait = func(*os.Process) error { waited = true; return nil }
	now := time.Unix(100, 0)
	proxyNow = func() time.Time { return now }
	proxySleep = func(d time.Duration) { now = now.Add(d) }
	if err := StartGrokOAuthProxy(); err == nil || !strings.Contains(err.Error(), "refusing to signal") {
		t.Fatalf("StartGrokOAuthProxy error = %v, want fail-closed identity error", err)
	}
	if !killed || !waited {
		t.Fatalf("unverified direct child cleanup kill=%v wait=%v", killed, waited)
	}
	_ = bin
}

func TestStopGrokOAuthProxyRefusesReplacementRecordDuringStaleCleanup(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	pidFile, _ := grokProxyFiles()
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"pid":4248,"executable":"/nonexistent","args":["proxy"],"start_fingerprint":"old"}`
	replacement := `{"pid":4249,"executable":"/nonexistent","args":["proxy"],"start_fingerprint":"new"}`
	if err := os.WriteFile(pidFile, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	oldIdentity, oldGone := proxyIdentity, proxyGone
	proxyIdentity = func(int) (processIdentityInfo, error) { return processIdentityInfo{}, os.ErrNotExist }
	proxyGone = func(*os.Process) bool {
		if err := os.WriteFile(pidFile, []byte(replacement), 0o600); err != nil {
			t.Fatal(err)
		}
		return true
	}
	t.Cleanup(func() { proxyIdentity, proxyGone = oldIdentity, oldGone })

	if err := StopGrokOAuthProxy(); err == nil || !strings.Contains(err.Error(), "replacement record") {
		t.Fatalf("StopGrokOAuthProxy error = %v", err)
	}
	got, ok := util.ReadFileSafe(pidFile)
	if !ok || got != replacement {
		t.Fatalf("replacement ownership record = %q (ok=%v)", got, ok)
	}
}
