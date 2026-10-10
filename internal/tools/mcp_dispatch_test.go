package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func withMcpDispatchTest(t *testing.T) {
	t.Helper()
	chdirTemp(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func TestMcpAdapterUnknownAgent(t *testing.T) {
	if mcpAdapter("unknown") != nil {
		t.Fatal("unknown agent should have no adapter")
	}
}

func TestMcpToolManifestCoverage(t *testing.T) {
	for _, agent := range core.ListAgents() {
		for _, tool := range []*core.ToolManifest{codegraph, contextMode, projectmem} {
			if tool.WireFor[agent.ID] == nil || tool.UnwireFor[agent.ID] == nil || tool.VerifyFor[agent.ID] == nil || tool.WiredAnyFor[agent.ID] == nil {
				t.Errorf("%s lacks complete MCP coverage for %s", tool.ID, agent.ID)
			}
		}
	}
}

func TestMcpCommandCodegraphIsWrapped(t *testing.T) {
	cmd := agents.McpCommand("claude", "codegraph")
	if len(cmd) == 0 {
		t.Fatal("empty codegraph command")
	}
	if !strings.Contains(strings.Join(cmd, " "), "run-mcp") {
		t.Fatalf("codegraph command is not auto-index wrapped: %v", cmd)
	}
	if pm := agents.McpCommand("claude", "projectmem"); len(pm) == 0 {
		t.Fatal("empty projectmem command")
	}
}

func TestMcpDispatchRoundTrip(t *testing.T) {
	withMcpDispatchTest(t)
	if !mcpConfigure("claude", "context-mode") {
		t.Fatal("mcpConfigure claude failed")
	}
	if !mcpHas("claude", "context-mode", false) {
		t.Fatal("strict mcpHas false after configure")
	}
	if !mcpHas("claude", "context-mode", true) {
		t.Fatal("loose mcpHas false after configure")
	}
	if !mcpRemove("claude", "context-mode") {
		t.Fatal("mcpRemove claude failed")
	}
	if mcpHas("claude", "context-mode", false) {
		t.Fatal("mcpHas true after remove")
	}
}

func TestMcpDispatchUnknownAgentIsNoop(t *testing.T) {
	if !mcpConfigure("unknown", "context-mode") {
		t.Fatal("unknown configure should be a no-op success")
	}
	if !mcpRemove("unknown", "context-mode") {
		t.Fatal("unknown remove should be a no-op success")
	}
	if mcpHas("unknown", "context-mode", false) {
		t.Fatal("unknown has should be false")
	}
}

func TestCodegraphVerifyWrappedVsUnwrapped(t *testing.T) {
	withMcpDispatchTest(t)

	// Strict (wrapped) shape: should pass codegraphVerify
	if !mcpConfigure("claude", "codegraph") {
		t.Fatal("mcpConfigure claude codegraph failed")
	}
	if got := codegraphVerify("claude"); !got {
		t.Fatal("codegraphVerify(claude) should pass after wrapped configure")
	}

	a := agents.McpAdapterFor("claude")
	if a == nil {
		t.Fatal("no claude adapter")
	}

	// Unwrapped shape: bare "codegraph" command with args ["serve", "--mcp"]
	bareCmd := []string{"codegraph", "serve", "--mcp"}
	if reply := a.ConfigureMcp("codegraph", core.WithCommand(bareCmd)); reply.Err != nil {
		t.Fatalf("ConfigureMcp bare: %v", reply.Err)
	}
	if got := codegraphVerify("claude"); !got {
		t.Fatal("codegraphVerify(claude) should pass with bare 'codegraph' command")
	}

	// Unwrapped shape: resolved path from PickMcpSpawn
	unwrapped := util.PickMcpSpawn("codegraph", "serve", "--mcp")
	unwrappedCmd := append([]string{unwrapped.Command}, unwrapped.Args...)
	if reply := a.ConfigureMcp("codegraph", core.WithCommand(unwrappedCmd)); reply.Err != nil {
		t.Fatalf("ConfigureMcp unwrapped: %v", reply.Err)
	}
	if got := codegraphVerify("claude"); !got {
		t.Fatal("codegraphVerify(claude) should pass with unwrapped codegraph entry")
	}
}

func TestProjectmemVerifyBehavior(t *testing.T) {
	withMcpDispatchTest(t)

	// Wrapped projectmem should pass projectmemVerify
	if !mcpConfigure("claude", "projectmem") {
		t.Fatal("mcpConfigure claude projectmem failed")
	}
	if got := projectmemVerify("claude"); !got {
		t.Fatal("projectmemVerify(claude) should pass after configure")
	}

	// Loose (anyShape) check should also pass
	if !mcpHas("claude", "projectmem", true) {
		t.Fatal("loose mcpHas should pass for projectmem after configure")
	}
}
