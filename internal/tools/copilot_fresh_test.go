package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func copilotTestHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	return tmp
}

func TestCopilotPathsResolvedDefaults(t *testing.T) {
	tmp := copilotTestHome(t)
	p := util.CopilotPathsResolved()
	if p.Dir != filepath.Join(tmp, ".copilot") {
		t.Fatalf("Dir: want %s, got %s", filepath.Join(tmp, ".copilot"), p.Dir)
	}
	if p.McpConfig != filepath.Join(tmp, ".copilot", "mcp-config.json") {
		t.Fatalf("McpConfig mismatch")
	}
	if p.Instructions != filepath.Join(tmp, ".copilot", "copilot-instructions.md") {
		t.Fatalf("Instructions mismatch")
	}
	if p.HooksDir != filepath.Join(tmp, ".copilot", "hooks") {
		t.Fatalf("HooksDir mismatch")
	}
}

func TestCopilotPathsResolvedEnv(t *testing.T) {
	copilotTestHome(t)
	tmp := t.TempDir()
	t.Setenv("COPILOT_HOME", tmp)
	p := util.CopilotPathsResolved()
	if p.Dir != tmp {
		t.Fatalf("COPILOT_HOME not honored: got %s", p.Dir)
	}
}

func TestCopilotPathsResolvedXDG(t *testing.T) {
	copilotTestHome(t)
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("COPILOT_HOME", "")
	p := util.CopilotPathsResolved()
	if p.Dir != filepath.Join(tmp, "copilot") {
		t.Fatalf("XDG_CONFIG_HOME not honored: got %s", p.Dir)
	}
}

func TestConfigureCopilotMcp_Codegraph(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	changed, f := agents.ConfigureCopilotMcp("codegraph")
	if !changed {
		t.Fatal("first call should change")
	}
	if f != util.CopilotPathsResolved().McpConfig {
		t.Fatalf("wrong file: %s", f)
	}

	raw, ok := util.ReadFileSafe(f)
	if !ok {
		t.Fatal("MCP config not written")
	}
	if !strings.Contains(raw, "mcpServers") {
		t.Fatal("missing mcpServers key")
	}
	if !strings.Contains(raw, "codegraph") {
		t.Fatal("missing codegraph entry")
	}
	if !strings.Contains(raw, `"local"`) {
		t.Fatal("missing type:local in raw JSON")
	}
	if !strings.Contains(raw, `"tools"`) {
		t.Fatal("missing tools key")
	}
	if !strings.Contains(raw, `"*"`) {
		t.Fatal("missing wildcard tool")
	}
	if strings.Contains(raw, "run-mcp") {
		t.Fatal("codegraph should spawn directly, not via run-mcp proxy")
	}

	// Idempotent re-run
	changed2, _ := agents.ConfigureCopilotMcp("codegraph")
	if changed2 {
		t.Fatal("re-run should not change")
	}
}

func TestConfigureCopilotMcp_ContextMode(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	changed, f := agents.ConfigureCopilotMcp("context-mode")
	if !changed {
		t.Fatal("first call should change")
	}

	raw, _ := util.ReadFileSafe(f)
	if !strings.Contains(raw, "context-mode") {
		t.Fatal("missing context-mode entry")
	}
	if strings.Contains(raw, "run-mcp") {
		t.Fatal("context-mode should NOT be autoindex wrapped")
	}
}

func TestCopilotMcpHas(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	if agents.CopilotMcpHas("codegraph") {
		t.Fatal("should not have before install")
	}
	agents.ConfigureCopilotMcp("codegraph")
	if !agents.CopilotMcpHas("codegraph") {
		t.Fatal("should have after install")
	}
}

func TestRemoveCopilotMcp(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	agents.ConfigureCopilotMcp("codegraph")
	if !agents.CopilotMcpHas("codegraph") {
		t.Fatal("precondition: should have codegraph")
	}
	removed := agents.RemoveCopilotMcp("codegraph")
	if !removed {
		t.Fatal("should remove")
	}
	if agents.CopilotMcpHas("codegraph") {
		t.Fatal("should not have after remove")
	}
	removed2 := agents.RemoveCopilotMcp("codegraph")
	if removed2 {
		t.Fatal("re-remove should be no-op")
	}
}

func TestCopilotRtkHook(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	if agents.HasCopilotRtkHook() {
		t.Fatal("should not have before install")
	}
	agents.InstallCopilotRtkHook()

	hookFile := filepath.Join(util.CopilotPathsResolved().HooksDir, "tokless-rtk.json")
	raw, ok := util.ReadFileSafe(hookFile)
	if !ok {
		t.Fatal("hook file not written")
	}
	if !strings.Contains(raw, "rtk-hook copilot") {
		t.Fatal("missing rtk-hook copilot command")
	}
	if !strings.Contains(raw, "preToolUse") {
		t.Fatal("missing preToolUse event (CLI)")
	}
	if !strings.Contains(raw, "PreToolUse") {
		t.Fatal("missing PreToolUse event (VS Code)")
	}
	if !agents.HasCopilotRtkHook() {
		t.Fatal("should have after install")
	}

	// Wire path
	opts := core.RunOpts{Agent: "copilot"}
	if wire, ok := rtk.WireFor["copilot"]; ok {
		ran, err := wire(opts)
		if err != nil || !ran {
			t.Fatalf("rtk wire copilot: ran=%v err=%v", ran, err)
		}
	} else {
		t.Fatal("rtk.WireFor[copilot] missing")
	}
	if verify, ok := rtk.VerifyFor["copilot"]; ok {
		if v := verify(); v == nil || !*v {
			t.Fatal("rtk verify copilot failed")
		}
	}

	agents.RemoveCopilotRtkHook()
	if agents.HasCopilotRtkHook() {
		t.Fatal("should not have after remove")
	}
}

func TestCopilotContextModeHook(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	if agents.HasCopilotContextModeHook() {
		t.Fatal("should not have before install")
	}
	agents.InstallCopilotContextModeHook()

	hookFile := filepath.Join(util.CopilotPathsResolved().HooksDir, "context-mode.json")
	raw, ok := util.ReadFileSafe(hookFile)
	if !ok {
		t.Fatal("hook file not written")
	}
	if !strings.Contains(raw, "context-mode hook copilot-cli pretooluse") {
		t.Fatal("missing pretooluse hook")
	}
	if !strings.Contains(raw, "context-mode hook copilot-cli posttooluse") {
		t.Fatal("missing posttooluse hook")
	}
	if !strings.Contains(raw, "context-mode hook copilot-cli sessionstart") {
		t.Fatal("missing sessionstart hook")
	}
	if !strings.Contains(raw, "context-mode hook copilot-cli precompact") {
		t.Fatal("missing precompact hook")
	}

	if !agents.HasCopilotContextModeHook() {
		t.Fatal("should have after install")
	}

	agents.RemoveCopilotContextModeHook()
	if agents.HasCopilotContextModeHook() {
		t.Fatal("should not have after remove")
	}
}

func TestCopilotInstructionBlock(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	_ = WriteOwner("copilot", "caveman")
	_ = WriteOwner("copilot", "codegraph")

	instr := util.CopilotPathsResolved().Instructions
	raw, ok := util.ReadFileSafe(instr)
	if !ok {
		t.Fatal("instructions not written")
	}
	if !strings.Contains(raw, "caveman") || !strings.Contains(raw, "codegraph") {
		t.Fatal("instructions missing sections")
	}

	RemoveOwner("copilot", "caveman")
	RemoveOwner("copilot", "codegraph")
	if util.Exists(instr) {
		t.Fatal("instructions should be removed when empty")
	}
}

func TestCopilotContextModeWireUnwire(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	opts := core.RunOpts{Agent: "copilot"}
	ran, err := ctxWireCopilot(opts)
	if err != nil {
		t.Fatalf("ctxWireCopilot failed: %v", err)
	}
	if !ran {
		t.Fatal("ctxWireCopilot returned false")
	}
	if !agents.CopilotMcpHas("context-mode") {
		t.Fatal("context-mode MCP not wired")
	}
	if !agents.HasCopilotContextModeHook() {
		t.Fatal("context-mode hook not installed")
	}

	ran, err = ctxUnwireCopilot(opts)
	if err != nil {
		t.Fatalf("ctxUnwireCopilot failed: %v", err)
	}
	if !ran {
		t.Fatal("ctxUnwireCopilot returned false")
	}
	if agents.CopilotMcpHas("context-mode") {
		t.Fatal("context-mode MCP not removed")
	}
	if agents.HasCopilotContextModeHook() {
		t.Fatal("context-mode hook not removed")
	}
}

func TestCopilotDoctorWireUnwire(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")

	opts := core.RunOpts{Agent: "copilot"}

	// Wire codegraph
	if verify, ok := codegraph.VerifyFor["copilot"]; ok {
		r := verify()
		if r != nil && *r {
			t.Fatal("codegraph should not be wired yet")
		}
	}
	if wire, ok := codegraph.WireFor["copilot"]; ok {
		ran, err := wire(opts)
		if err != nil {
			t.Fatalf("codegraph wire failed: %v", err)
		}
		if !ran {
			t.Fatal("codegraph wire returned false")
		}
	}
	if !agents.CopilotMcpHas("codegraph") {
		t.Fatal("codegraph MCP not wired")
	}

	// Unwire codegraph
	if unwire, ok := codegraph.UnwireFor["copilot"]; ok {
		ran, err := unwire(opts)
		if err != nil {
			t.Fatalf("codegraph unwire failed: %v", err)
		}
		if !ran {
			t.Fatal("codegraph unwire returned false")
		}
	}
	if agents.CopilotMcpHas("codegraph") {
		t.Fatal("codegraph MCP not removed")
	}
}
