//go:build linux

package headroom

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestProxyAutostartUnitBody(t *testing.T) {
	body := proxyAutostartUnitBody("/home/u/.local/bin/tokless")
	bin := `/home/u/.local/bin/tokless`
	for _, want := range []string{
		"__proxy-run",
		bin,
		"WantedBy=default.target",
		"Type=simple",
		"Restart=on-failure",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("unit missing %q:\n%s", want, body)
		}
	}
	spaced := proxyAutostartUnitBody(`/tmp/tokless bin/tokless`)
	if !strings.Contains(spaced, `"/tmp/tokless bin/tokless"`) {
		t.Fatalf("spaces not quoted:\n%s", spaced)
	}
}

func TestStopProxyAutostartUnitPropagatesFailure(t *testing.T) {
	oldStop := stopProxyAutostartUnit
	t.Cleanup(func() { stopProxyAutostartUnit = oldStop })
	stopProxyAutostartUnit = func() error { return os.ErrPermission }
	if err := stopProxyAutostartUnit(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("stopProxyAutostartUnit = %v, want permission error", err)
	}
}

func TestProxyAutostartConfiguredIncludesInactiveManagedUnit(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := util.WriteFile(filepath.Join(config, "systemd", "user", proxyAutostartUnit), proxyAutostartUnitBody(util.ToklessPersistedAbs())); err != nil {
		t.Fatal(err)
	}
	if !ProxyAutostartConfigured() {
		t.Fatal("managed inactive unit must count as configured")
	}
}

func TestProxyAutostartUnitPathIgnoresRelativeXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	want := filepath.Join(util.Home(), ".config", "systemd", "user", proxyAutostartUnit)
	if got := proxyAutostartUnitPath(); got != want {
		t.Fatalf("proxyAutostartUnitPath() = %q, want %q", got, want)
	}
}

func fakeSystemctl(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "systemctl.log")
	script := `#!/bin/sh
echo "$@" >> "$SYSTEMCTL_LOG"
case "$*" in
  *is-active*)
    printf '%s\n' "$FAKE_IS_ACTIVE"
    if [ "$FAKE_IS_ACTIVE" = "active" ]; then exit 0; fi
    exit 3
    ;;
  *stop*)
    exit "${FAKE_STOP_EXIT:-0}"
    ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYSTEMCTL_LOG", logPath)
	t.Setenv("PATH", binDir)
	return logPath
}

func writeByokGatewayUnit(t *testing.T) {
	t.Helper()
	unitDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(unitDir, byokGatewayAutostartUnit)
	if err := os.WriteFile(unitPath, []byte("[Unit]\n# tokless-managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSystemctlLog(t *testing.T, logPath string) string {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(raw)
}

func TestStopByokGatewaySupervisorSkipsWhenUnitMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	logPath := fakeSystemctl(t)
	if err := stopByokGatewaySupervisor(); err != nil {
		t.Fatalf("stopByokGatewaySupervisor() = %v, want nil", err)
	}
	if log := readSystemctlLog(t, logPath); log != "" {
		t.Fatalf("systemctl must not run when unit is absent, ran: %s", log)
	}
}

func TestStopByokGatewaySupervisorSkipsInactiveUnit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeByokGatewayUnit(t)
	logPath := fakeSystemctl(t)
	t.Setenv("FAKE_IS_ACTIVE", "inactive")
	if err := stopByokGatewaySupervisor(); err != nil {
		t.Fatalf("stopByokGatewaySupervisor() = %v, want nil", err)
	}
	log := readSystemctlLog(t, logPath)
	if !strings.Contains(log, "is-active") {
		t.Fatalf("inactive unit must be probed with is-active, log: %s", log)
	}
	if strings.Contains(log, "stop ") {
		t.Fatalf("inactive unit must not be stopped, log: %s", log)
	}
}

func TestStopByokGatewaySupervisorStopsActiveUnit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeByokGatewayUnit(t)
	logPath := fakeSystemctl(t)
	t.Setenv("FAKE_IS_ACTIVE", "active")
	if err := stopByokGatewaySupervisor(); err != nil {
		t.Fatalf("stopByokGatewaySupervisor() = %v, want nil", err)
	}
	log := readSystemctlLog(t, logPath)
	if !strings.Contains(log, "stop "+byokGatewayAutostartUnit) {
		t.Fatalf("active unit must be stopped, log: %s", log)
	}
}

func TestStopByokGatewaySupervisorPropagatesStopFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeByokGatewayUnit(t)
	_ = fakeSystemctl(t)
	t.Setenv("FAKE_IS_ACTIVE", "active")
	t.Setenv("FAKE_STOP_EXIT", "1")
	if err := stopByokGatewaySupervisor(); err == nil {
		t.Fatal("systemctl stop failure must propagate")
	}
}
