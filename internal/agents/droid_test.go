package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func setTestHome(t *testing.T) {
	t.Helper()
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
}

// --- MCP management ---

func TestConfigureDroidMcp_WritesEntry(t *testing.T) {
	setTestHome(t)
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
	setTestHome(t)
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
	setTestHome(t)
	ConfigureDroidMcp("codegraph")
	changed, _ := ConfigureDroidMcp("codegraph")
	if changed {
		t.Fatal("expected idempotent with enabledTools present")
	}
}

func TestConfigureDroidMcp_Idempotent(t *testing.T) {
	setTestHome(t)
	ConfigureDroidMcp("codegraph")
	changed, _ := ConfigureDroidMcp("codegraph")
	if changed {
		t.Fatal("expected idempotent (no change on second call)")
	}
}

func TestDroidMcpHas_True(t *testing.T) {
	setTestHome(t)
	ConfigureDroidMcp("codegraph")
	if !DroidMcpHas("codegraph") {
		t.Fatal("expected DroidMcpHas to be true after configure")
	}
}

func TestDroidMcpHas_False(t *testing.T) {
	setTestHome(t)
	if DroidMcpHas("codegraph") {
		t.Fatal("expected DroidMcpHas to be false before configure")
	}
}

func TestRemoveDroidMcp(t *testing.T) {
	setTestHome(t)
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
	setTestHome(t)
	if RemoveDroidMcp("codegraph") {
		t.Fatal("expected false when nothing to remove")
	}
}

// --- RTK hook ---

func TestInstallDroidRtkHook(t *testing.T) {
	setTestHome(t)
	if HasDroidRtkHook() {
		t.Fatal("expected no hook before install")
	}
	InstallDroidRtkHook()
	if !HasDroidRtkHook() {
		t.Fatal("expected hook after install")
	}
}

func TestInstallDroidRtkHook_Idempotent(t *testing.T) {
	setTestHome(t)
	InstallDroidRtkHook()
	raw1, _ := os.ReadFile(droidHooksFile())
	InstallDroidRtkHook()
	raw2, _ := os.ReadFile(droidHooksFile())
	if string(raw1) != string(raw2) {
		t.Fatal("expected idempotent (same content after double install)")
	}
}

func TestInstallDroidRtkHookMigratesBackslashAndDeduplicates(t *testing.T) {
	setTestHome(t)
	raw := `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"C:\\old\\tokless.exe rtk-hook droid"}]},{"matcher":"Execute","hooks":[{"type":"command","command":"D:\\old\\tokless.exe rtk-hook droid"}]},{"matcher":"Execute","hooks":[{"type":"command","command":"echo user"}]}]}`
	if err := util.WriteFile(droidHooksFile(), raw); err != nil {
		t.Fatal(err)
	}

	InstallDroidRtkHook()
	got, _ := os.ReadFile(droidHooksFile())
	if strings.Count(string(got), "rtk-hook droid") != 1 {
		t.Fatalf("managed hooks not deduplicated: %s", got)
	}
	if !strings.Contains(string(got), "echo user") {
		t.Fatalf("user hook removed: %s", got)
	}
}

func TestInstallDroidRtkHookPreservesWrapper(t *testing.T) {
	setTestHome(t)
	raw := `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"custom-wrapper rtk-hook droid"}]}]}`
	if err := util.WriteFile(droidHooksFile(), raw); err != nil {
		t.Fatal(err)
	}
	InstallDroidRtkHook()
	got, _ := os.ReadFile(droidHooksFile())
	if !strings.Contains(string(got), "custom-wrapper rtk-hook droid") || strings.Count(string(got), "rtk-hook droid") != 2 {
		t.Fatalf("wrapper claimed or managed hook missing: %s", got)
	}
}

func TestRemoveDroidRtkHook(t *testing.T) {
	setTestHome(t)
	InstallDroidRtkHook()
	RemoveDroidRtkHook()
	if HasDroidRtkHook() {
		t.Fatal("expected hook removed")
	}
}

func TestRemoveDroidRtkHookPreservesUserSiblings(t *testing.T) {
	setTestHome(t)
	managed := toklessCommand("rtk-hook", "droid")
	raw := `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"` + managed + `"},{"type":"command","command":"echo user"},{"type":"command","command":"custom-wrapper rtk-hook droid"}]}]}`
	if err := util.WriteFile(droidHooksFile(), raw); err != nil {
		t.Fatal(err)
	}
	RemoveDroidRtkHook()
	got, _ := os.ReadFile(droidHooksFile())
	if strings.Contains(string(got), `"command": "`+managed+`"`) || !strings.Contains(string(got), "echo user") || !strings.Contains(string(got), "custom-wrapper rtk-hook droid") {
		t.Fatalf("unexpected hooks after remove: %s", got)
	}
}

// --- CodeGraph hook ---

func TestInstallDroidCodegraphIndexHook(t *testing.T) {
	setTestHome(t)
	if HasDroidCodegraphIndexHook() {
		t.Fatal("expected no hook before install")
	}
	InstallDroidCodegraphIndexHook()
	if !HasDroidCodegraphIndexHook() {
		t.Fatal("expected hook after install")
	}
}

func TestRemoveDroidCodegraphIndexHook(t *testing.T) {
	setTestHome(t)
	InstallDroidCodegraphIndexHook()
	RemoveDroidCodegraphIndexHook()
	if HasDroidCodegraphIndexHook() {
		t.Fatal("expected hook removed")
	}
}

func TestRemoveDroidCodegraphIndexHookPreservesUserSibling(t *testing.T) {
	setTestHome(t)
	managed := toklessCommand("index", "--auto", "droid")
	raw := `{"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"` + managed + `"},{"type":"command","command":"echo user"}]}]}`
	if err := util.WriteFile(droidHooksFile(), raw); err != nil {
		t.Fatal(err)
	}
	RemoveDroidCodegraphIndexHook()
	got, _ := os.ReadFile(droidHooksFile())
	if strings.Contains(string(got), managed) || !strings.Contains(string(got), "echo user") {
		t.Fatalf("unexpected hooks after remove: %s", got)
	}
}

func TestInstallDroidCodegraphIndexHookMigratesBackslashAndDeduplicates(t *testing.T) {
	setTestHome(t)
	raw := `{"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"C:\\old\\tokless.exe index --auto droid"}]},{"matcher":"","hooks":[{"type":"command","command":"D:\\old\\tokless.exe index --auto droid"}]},{"matcher":"","hooks":[{"type":"command","command":"echo user"}]}]}`
	if err := util.WriteFile(droidHooksFile(), raw); err != nil {
		t.Fatal(err)
	}

	InstallDroidCodegraphIndexHook()
	got, _ := os.ReadFile(droidHooksFile())
	if strings.Count(string(got), "index --auto droid") != 1 {
		t.Fatalf("managed hooks not deduplicated: %s", got)
	}
	if !strings.Contains(string(got), "echo user") {
		t.Fatalf("user hook removed: %s", got)
	}
}

// --- Context-Mode: no PreToolUse hook (MCP + AGENTS.md only) ---

func TestDroidContextModeHook_RemovedFromStaleInstall(t *testing.T) {
	setTestHome(t)
	InstallDroidRtkHook()
	RemoveDroidCtxModePreToolUse()
	if HasDroidCtxModePreToolUse() {
		t.Fatal("expected ctx-mode hook absent after Remove")
	}
}

// --- combined hooks ---

func TestDroidHooks_MultipleIndependent(t *testing.T) {
	setTestHome(t)
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
	setTestHome(t)
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
	setTestHome(t)
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
	if a.ConfigDir() != filepath.Join(util.Home(), ".factory") {
		t.Fatalf("expected %s, got %q", filepath.Join(util.Home(), ".factory"), a.ConfigDir())
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
