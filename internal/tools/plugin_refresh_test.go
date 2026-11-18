package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// TestRefreshManagedPluginsRewritesStaleOwnedOnly proves update migrates every
// stale tokless plugin (issue #70), leaves foreign files alone, and creates nothing.
func TestRefreshManagedPluginsRewritesStaleOwnedOnly(t *testing.T) {
	home := opencode2TestHome(t)
	for _, k := range []string{"PI_CONFIG_DIR", "PI_CODING_AGENT_DIR", "CLINE_DIR", "CLINE_DATA_DIR", "AMP_CONFIG_DIR", "KILO_CONFIG_DIR", "OPENCODE_CONFIG_DIR_V2"} {
		t.Setenv(k, "")
	}
	exe := "/opt/tokless/bin/tokless"
	util.PinToklessPersisted(exe)
	t.Cleanup(func() { util.PinToklessPersisted("") })
	current, old := managedPlugins(exe), managedPlugins("/old/tokless")
	for i, f := range current {
		if !strings.HasPrefix(f.path, home) {
			t.Fatalf("%s escapes the test home", f.path)
		}
		stale := strings.ReplaceAll(old[i].want, ", windowsHide: true", "")
		if stale == f.want || !f.owned(stale) {
			t.Fatalf("%s: bad stale fixture", f.path)
		}
		if err := util.WriteFile(f.path, stale); err != nil {
			t.Fatal(err)
		}
	}
	for run := 0; run < 2; run++ {
		RefreshManagedPlugins()
		for _, f := range current {
			if raw, _ := util.ReadFileSafe(f.path); raw != f.want {
				t.Fatalf("run %d: %s not refreshed:\n%s", run, f.path, raw)
			}
		}
	}

	foreign := "// user plugin\nexport default {}\n"
	if err := util.WriteFile(ampRtkPluginPath(), foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ompRtkExtensionPath()); err != nil {
		t.Fatal(err)
	}
	RefreshManagedPlugins()
	if raw, _ := util.ReadFileSafe(ampRtkPluginPath()); raw != foreign {
		t.Fatalf("foreign plugin rewritten:\n%s", raw)
	}
	if util.Exists(ompRtkExtensionPath()) {
		t.Fatal("refresh recreated an unwired plugin")
	}
}
