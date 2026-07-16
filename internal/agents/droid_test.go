package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// --- MCP management ---

func TestConfigureDroidMcp_WritesEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	changed, file := ConfigureDroidMcp("codegraph")
	if !changed {
		t.Fatal("expected changed=true on first configure")
	}
	if file == "" {
		t.Fatal("expected file path")
	}
	raw, _ := os.ReadFile(file)
	if len(raw) == 0 {
		t.Fatal("expected non-empty MCP config")
	}
}

// Verify enabledTools array written for Droid v0.170.0+ Deferred Context Engine.
func TestConfigureDroidMcp_WritesEnabledTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, file := ConfigureDroidMcp("codegraph")
	raw, _ := os.ReadFile(file)
	cfg := util.TryParseJsonc(string(raw))
	servers, _ := cfg.Get("mcpServers")
	sm, _ := servers.(*util.OrderedMap)
	entry, _ := sm.Get("codegraph")
	em, _ := entry.(*util.OrderedMap)
	et, ok := em.Get("enabledTools")
	if !ok {
		t.Fatal("expected enabledTools field in MCP entry")
	}
	arr, ok := et.([]any)
	if !ok {
		t.Fatalf("expected enabledTools to be array, got %T", et)
	}
	if len(arr) != len(CodegraphDroidToolNames) {
		t.Fatalf("expected %d tools, got %d", len(CodegraphDroidToolNames), len(arr))
	}
}

func TestConfigureDroidMcp_IdempotentWithEnabledTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ConfigureDroidMcp("codegraph")
	changed, _ := ConfigureDroidMcp("codegraph")
	if changed {
		t.Fatal("expected idempotent with enabledTools present")
	}
}

func TestConfigureDroidMcp_Idempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ConfigureDroidMcp("codegraph")
	changed, _ := ConfigureDroidMcp("codegraph")
	if changed {
		t.Fatal("expected idempotent (no change on second call)")
	}
}

func TestDroidMcpHas_True(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ConfigureDroidMcp("codegraph")
	if !DroidMcpHas("codegraph") {
		t.Fatal("expected DroidMcpHas to be true after configure")
	}
}

func TestDroidMcpHas_False(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if DroidMcpHas("codegraph") {
		t.Fatal("expected DroidMcpHas to be false before configure")
	}
}

func TestRemoveDroidMcp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ConfigureDroidMcp("codegraph")
	ok := RemoveDroidMcp("codegraph")
	if !ok {
		t.Fatal("expected remove to return true")
	}
	if DroidMcpHas("codegraph") {
		t.Fatal("expected codegraph removed")
	}
}

func TestRemoveDroidMcp_NotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if RemoveDroidMcp("codegraph") {
		t.Fatal("expected false when nothing to remove")
	}
}

// --- RTK hook ---

func TestInstallDroidRtkHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if HasDroidRtkHook() {
		t.Fatal("expected no hook before install")
	}
	InstallDroidRtkHook()
	if !HasDroidRtkHook() {
		t.Fatal("expected hook after install")
	}
}

func TestInstallDroidRtkHook_Idempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	InstallDroidRtkHook()
	raw1, _ := os.ReadFile(droidHooksFile())
	InstallDroidRtkHook()
	raw2, _ := os.ReadFile(droidHooksFile())
	if string(raw1) != string(raw2) {
		t.Fatal("expected idempotent (same content after double install)")
	}
}

func TestRemoveDroidRtkHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	InstallDroidRtkHook()
	RemoveDroidRtkHook()
	if HasDroidRtkHook() {
		t.Fatal("expected hook removed")
	}
}

// --- CodeGraph hook ---

func TestInstallDroidCodegraphIndexHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if HasDroidCodegraphIndexHook() {
		t.Fatal("expected no hook before install")
	}
	InstallDroidCodegraphIndexHook()
	if !HasDroidCodegraphIndexHook() {
		t.Fatal("expected hook after install")
	}
}

func TestRemoveDroidCodegraphIndexHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	InstallDroidCodegraphIndexHook()
	RemoveDroidCodegraphIndexHook()
	if HasDroidCodegraphIndexHook() {
		t.Fatal("expected hook removed")
	}
}

// --- Context-Mode: no PreToolUse hook (MCP + AGENTS.md only) ---

func TestDroidContextModeHook_RemovedFromStaleInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	InstallDroidRtkHook()
	RemoveDroidCtxModePreToolUse()
	if HasDroidCtxModePreToolUse() {
		t.Fatal("expected ctx-mode hook absent after Remove")
	}
}

// --- combined hooks ---

func TestDroidHooks_MultipleIndependent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	InstallDroidRtkHook()
	InstallDroidCodegraphIndexHook()

	if !HasDroidRtkHook() {
		t.Fatal("rtk hook missing")
	}
	if !HasDroidCodegraphIndexHook() {
		t.Fatal("codegraph hook missing")
	}
	if HasDroidCtxModePreToolUse() {
		t.Fatal("ctx-mode hook should never be installed (MCP-only)")
	}

	// Remove one, others remain
	RemoveDroidRtkHook()
	if HasDroidRtkHook() {
		t.Fatal("rtk hook should be removed")
	}
	if !HasDroidCodegraphIndexHook() {
		t.Fatal("codegraph hook should remain")
	}
}

// --- flat hooks schema ---

func TestDroidHooks_FlatSchema(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	InstallDroidRtkHook()
	raw, _ := os.ReadFile(droidHooksFile())
	s := string(raw)
	if containsStr(s, `"hooks":{`) {
		t.Fatal("hooks.json must use flat schema, not nested")
	}
	if !containsStr(s, `"PreToolUse"`) {
		t.Fatal("expected top-level PreToolUse event")
	}
}

// --- agent manifest ---

func TestDroidRegistered(t *testing.T) {
	a := core.GetAgent("droid")
	if a == nil {
		t.Fatal("droid agent not registered")
	}
	if a.ID != "droid" {
		t.Fatalf("expected ID 'droid', got %q", a.ID)
	}
	if a.CLIBin != "droid" {
		t.Fatalf("expected CLIBin 'droid', got %q", a.CLIBin)
	}
	if a.ConfigDir() != filepath.Join(os.Getenv("HOME"), ".factory") {
		t.Fatalf("unexpected config dir: %q", a.ConfigDir())
	}
}

// --- helpers ---

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
