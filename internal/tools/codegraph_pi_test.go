package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func piCgHome(t *testing.T) {
	t.Helper()
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("HOME", util.Home())
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("TOKLESS_TEST", "1")
}

func TestCodegraphPiWireUnwire(t *testing.T) {
	piCgHome(t)
	wire, unwire := codegraph.WireFor["pi"], codegraph.UnwireFor["pi"]
	if wire == nil || unwire == nil {
		t.Fatal("missing maps")
	}
	ran, err := wire(core.RunOpts{})
	if err != nil || !ran {
		t.Fatalf("wire: %v %v", ran, err)
	}
	if !agents.PiMcpHas("codegraph") || !piCodegraphIndexExtensionPresent() {
		t.Fatal("mcp or index missing")
	}
	if !agents.PiSourceHas(agents.PiSrcMcpAdapter) {
		t.Fatal("adapter missing")
	}
	ext, _ := util.ReadFileSafe(piCodegraphIndexPath())
	for _, w := range []string{"session_start", "tool_result", "sync", "init"} {
		if !strings.Contains(ext, w) {
			t.Errorf("index missing %q", w)
		}
	}
	// keep second mcp so adapter survives
	agents.ConfigurePiMcp("context-mode")
	if _, err := unwire(core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if agents.PiMcpHas("codegraph") || piCodegraphIndexExtensionPresent() {
		t.Fatal("codegraph not cleaned")
	}
	if !agents.PiSourceHas(agents.PiSrcMcpAdapter) {
		t.Fatal("adapter should stay with other MCP")
	}
}

func TestCodegraphPiUnwireDropsAdapterWhenLast(t *testing.T) {
	piCgHome(t)
	if _, err := codegraph.WireFor["pi"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := codegraph.UnwireFor["pi"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if agents.PiSourceHas(agents.PiSrcMcpAdapter) {
		t.Fatal("adapter should drop when last MCP")
	}
}

func TestCodegraphPiDryRun(t *testing.T) {
	piCgHome(t)
	ran, err := codegraph.WireFor["pi"](core.RunOpts{DryRun: true})
	if err != nil || !ran {
		t.Fatalf("%v %v", ran, err)
	}
	if agents.PiMcpHas("codegraph") || piCodegraphIndexExtensionPresent() {
		t.Fatal("dry-run wrote files")
	}
}

func TestCodegraphPiPurgeLegacy(t *testing.T) {
	piCgHome(t)
	_ = util.EnsureDir(agents.PiAgentDirResolved())
	cfg := util.NewOrderedMap()
	cfg.Set("packages", []any{"npm:@vndv/pi-codegraph", "npm:user-keep"})
	_ = util.WriteFile(filepath.Join(agents.PiAgentDirResolved(), "settings.json"), util.StringifyJSON(cfg))
	if _, err := codegraph.WireFor["pi"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if agents.PiSourceHas("npm:@vndv/pi-codegraph") {
		t.Fatal("legacy remains")
	}
	if !agents.PiSourceHas("npm:user-keep") {
		t.Fatal("user package lost")
	}
}
