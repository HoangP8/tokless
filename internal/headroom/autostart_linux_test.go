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
	if err := util.WriteFile(filepath.Join(config, "systemd", "user", proxyAutostartUnit), proxyAutostartUnitBody(util.ToklessAbs())); err != nil {
		t.Fatal(err)
	}
	if !ProxyAutostartConfigured() {
		t.Fatal("managed inactive unit must count as configured")
	}
}
