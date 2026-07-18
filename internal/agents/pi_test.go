package agents

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func setPiTestHome(t *testing.T) {
	t.Helper()
	util.SetHomeOverride(t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", "")
}

func TestPiAgentDir(t *testing.T) {
	setPiTestHome(t)
	if got := piAgentDir(); got != filepath.Join(util.Home(), ".pi", "agent") {
		t.Fatalf("default: %q", got)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "/custom/pi")
	if got := piAgentDir(); got != "/custom/pi" {
		t.Fatalf("abs override: %q", got)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "~/mypi")
	if got := piAgentDir(); got != filepath.Join(util.Home(), "mypi") {
		t.Fatalf("tilde: %q", got)
	}
}

func TestPiRegistered(t *testing.T) {
	setPiTestHome(t)
	a := core.GetAgent("pi")
	if a == nil || a.CLIBin != "pi" {
		t.Fatal("pi not registered")
	}
}

func TestPiSourceIdentity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"npm:pi-mcp-adapter@1.0.0", "npm:pi-mcp-adapter"},
		{"npm:@scope/pkg@1.0.0", "npm:@scope/pkg"},
		{"git:github.com/a/b@v1", "git:github.com/a/b"},
		{"https://github.com/a/b", "git:github.com/a/b"},
		{"", ""},
	}
	for _, c := range cases {
		if got := piSourceIdentity(c.in); got != c.want {
			t.Errorf("piSourceIdentity(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestPiPackagesAddRemove(t *testing.T) {
	setPiTestHome(t)
	if !piPackagesAdd(PiSrcMcpAdapter) {
		t.Fatal("add")
	}
	if piPackagesAdd(PiSrcMcpAdapter) {
		t.Fatal("idempotent")
	}
	if !PiSourceHas(PiSrcMcpAdapter) {
		t.Fatal("has")
	}
	if !piPackagesRemove(PiSrcMcpAdapter) {
		t.Fatal("remove")
	}
	if PiSourceHas(PiSrcMcpAdapter) {
		t.Fatal("gone")
	}
}

func TestPiInstallSource_TestMode(t *testing.T) {
	setPiTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")
	if !PiInstallSource(PiSrcMcpAdapter) {
		t.Fatal("install")
	}
	if !PiSourceHas(PiSrcMcpAdapter) {
		t.Fatal("missing after install")
	}
	// unpin
	cfg := piPackagesLoad()
	cfg.Set("packages", []any{"npm:pi-mcp-adapter@2.5.4"})
	_ = util.WriteFile(piSettingsFile(), util.StringifyJSON(cfg))
	if !PiInstallSource(PiSrcMcpAdapter) {
		t.Fatal("unpin install")
	}
	if got := PiPackageSettingsSource(PiSrcMcpAdapter); got != PiSrcMcpAdapter {
		t.Fatalf("still pinned: %q", got)
	}
}

func TestPiPurgeCodegraph(t *testing.T) {
	setPiTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")
	piPackagesAdd("npm:@vndv/pi-codegraph")
	piPackagesAdd("npm:pi-mcp-adapter")
	piPackagesAdd("npm:user-other")
	if PiPurgeCodegraphPackages() != 1 {
		t.Fatal("purge count")
	}
	if PiSourceHas("npm:@vndv/pi-codegraph") {
		t.Fatal("legacy remains")
	}
	if !PiSourceHas("npm:pi-mcp-adapter") || !PiSourceHas("npm:user-other") {
		t.Fatal("must keep adapter + user")
	}
}

func TestPiPackageList(t *testing.T) {
	list := PiPackageList()
	if len(list) != 1 || list[0] != PiSrcMcpAdapter {
		t.Fatalf("%v", list)
	}
}

func TestConfigurePiMcp(t *testing.T) {
	setPiTestHome(t)
	changed, f := ConfigurePiMcp("codegraph")
	if !changed || f == "" {
		t.Fatal("configure")
	}
	if !PiMcpHas("codegraph") {
		t.Fatal("has")
	}
	raw, _ := util.ReadFileSafe(f)
	if !strings.Contains(raw, "codegraph") || !strings.Contains(raw, "serve") {
		t.Fatalf("%s", raw)
	}
	if changed2, _ := ConfigurePiMcp("codegraph"); changed2 {
		t.Fatal("idempotent")
	}
	ConfigurePiMcp("context-mode")
	if !RemovePiMcp("codegraph") {
		t.Fatal("remove")
	}
	if PiMcpHas("codegraph") || !PiMcpHas("context-mode") {
		t.Fatal("surgical remove")
	}
	RemovePiMcp("context-mode")
	if PiMcpHasAny() {
		t.Fatal("empty")
	}
}

func TestPiUpdatePackages_Unpins(t *testing.T) {
	setPiTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")
	cfg := piPackagesLoad()
	cfg.Set("packages", []any{"npm:pi-mcp-adapter@2.5.4", "npm:user-pkg@1.0.0"})
	_ = util.WriteFile(piSettingsFile(), util.StringifyJSON(cfg))
	PiUpdatePackages()
	if got := PiPackageSettingsSource(PiSrcMcpAdapter); got != PiSrcMcpAdapter {
		t.Fatalf("adapter pin: %q", got)
	}
	if !PiSourceHas("npm:user-pkg") {
		t.Fatal("user package lost")
	}
}
