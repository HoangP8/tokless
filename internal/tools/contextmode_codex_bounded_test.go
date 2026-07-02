package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// TestWireCodexManual_BoundedShape verifies that wireCodexManual writes MCP +
// AGENTS.md with CONTEXT-MODE marker block plus a single minimal context-mode
// redirect PreToolUse hook.
func TestWireCodexManual_BoundedShape(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	defer util.SetHomeOverride("")

	if !wireCodexManual() {
		t.Fatal("wireCodexManual returned false")
	}

	hooksPath := filepath.Join(tmp, ".codex", "hooks.json")
	hooksData, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("context-mode should create hooks.json with the redirect hook: %v", err)
	}
	hooks := string(hooksData)
	if !strings.Contains(hooks, "context-mode hook codex pretooluse") {
		t.Fatalf("hooks.json missing context-mode redirect hook:\n%s", hooks)
	}
	if !strings.Contains(hooks, `"PreToolUse"`) || !strings.Contains(hooks, `local_shell|shell|shell_command|exec_command|Bash|Shell|apply_patch|Edit|Write|grep_files|ctx_execute|ctx_execute_file|ctx_batch_execute|ctx_fetch_and_index|ctx_search|ctx_index|mcp__`) {
		t.Fatalf("hooks.json redirect hook should match upstream PreToolUse config:\n%s", hooks)
	}
	// It must not reinstate any legacy multi-event context-mode hooks.
	for _, bad := range []string{"SessionStart", "PreCompact", "context-mode hook codex sessionstart", "context-mode hook codex posttooluse"} {
		if strings.Contains(hooks, bad) {
			t.Fatalf("hooks.json should not contain legacy %q:\n%s", bad, hooks)
		}
	}

	cfgPath := filepath.Join(tmp, ".codex", "config.toml")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	t.Logf("=== config.toml ===\n%s=== end ===", string(cfgData))

	agentsPath := filepath.Join(tmp, ".codex", "AGENTS.md")
	agentsData, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	agents := string(agentsData)
	t.Logf("=== AGENTS.md ===\n%s=== end ===", agents)

	if !strings.Contains(agents, "## Context Tools (context-mode)") {
		t.Error("AGENTS.md missing Context Tools section heading")
	}

	for _, bad := range []string{"context_window_protection"} {
		if strings.Contains(agents, bad) {
			t.Errorf("AGENTS.md contains forbidden marker %q", bad)
		}
	}

	cfg := string(cfgData)
	if !strings.Contains(cfg, "[mcp_servers.context_mode]") {
		t.Error("config.toml missing [mcp_servers.context_mode]")
	}
	if strings.Contains(cfg, "[mcp_servers.context-mode]") {
		t.Error("config.toml still has legacy [mcp_servers.context-mode]")
	}
	if !strings.Contains(cfg, "hooks = true") {
		t.Error("config.toml should enable Codex hooks like upstream context-mode config")
	}
	if !ctxVerifyCodex() {
		t.Error("ctxVerifyCodex returned false for MCP + AGENTS.md install")
	}
}

// TestWireCodexManual_Idempotent verifies that running wireCodexManual twice
// produces the same output (no duplicate entries, no drift).
func TestWireCodexManual_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	defer util.SetHomeOverride("")

	if !wireCodexManual() {
		t.Fatal("first wireCodexManual returned false")
	}
	first, err := os.ReadFile(filepath.Join(tmp, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read first: %v", err)
	}

	if !wireCodexManual() {
		t.Fatal("second wireCodexManual returned false")
	}
	second, err := os.ReadFile(filepath.Join(tmp, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read second: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("wireCodexManual not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestWireCodexManual_PreservesUserHook verifies that wireCodexManual does NOT
// overwrite hooks.json entries that don't belong to context-mode (rtk, user hooks).
func TestWireCodexManual_PreservesUserHook(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	defer util.SetHomeOverride("")

	codexDir := filepath.Join(tmp, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userHooks := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/usr/bin/user-guard.py","timeout":20}]}]}}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(userHooks), 0o644); err != nil {
		t.Fatal(err)
	}

	if !wireCodexManual() {
		t.Fatal("wireCodexManual returned false")
	}
	data, err := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "/usr/bin/user-guard.py") {
		t.Errorf("user hook overwritten (must be preserved):\n%s", data)
	}
}

func TestRemoveCodexContextModeHooks_RemovesLegacyContextModeEvents(t *testing.T) {
	existing := util.TryParseJsonc(`{
		"hooks":{
			"PreToolUse":[
				{"matcher":"Bash","hooks":[{"type":"command","command":"tokless context-mode-hook codex pretooluse"}]},
				{"matcher":"Bash","hooks":[{"type":"command","command":"echo user"}]}
			],
			"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"context-mode hook codex sessionstart"}]}],
			"PreCompact":[{"matcher":"","hooks":[{"type":"command","command":"context-mode hook codex precompact"}]}],
			"Stop":[{"matcher":"","hooks":[{"type":"command","command":"context-mode hook codex stop"}]}],
			"PostToolUse":[{"matcher":"","hooks":[{"type":"command","command":"context-mode hook codex posttooluse"}]}],
			"UserPromptSubmit":[{"matcher":"","hooks":[{"type":"command","command":"context-mode hook codex userpromptsubmit"}]}]
		}
	}`)
	if existing == nil {
		t.Fatal("failed to parse fixture")
	}

	next := util.StringifyJSON(removeCodexContextModeHooks(existing))
	if strings.Contains(next, "tokless context-mode-hook codex") {
		t.Fatalf("legacy tokless context-mode hook not removed:\n%s", next)
	}
	if !strings.Contains(next, "echo user") {
		t.Fatalf("user hook was removed:\n%s", next)
	}
	if strings.Contains(next, "context-mode hook codex") {
		t.Fatalf("context-mode hook still present:\n%s", next)
	}
	for _, dropped := range []string{"SessionStart", "PreCompact", "Stop", "PostToolUse", "UserPromptSubmit"} {
		if strings.Contains(next, `"`+dropped+`"`) {
			t.Fatalf("legacy event %s still present:\n%s", dropped, next)
		}
	}
}

func TestWireCodexManual_CleansProjectLocalCodexHooks(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	project := filepath.Join(tmp, "project")
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	defer util.SetHomeOverride("")
	if err := os.MkdirAll(filepath.Join(project, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := "[mcp_servers.context-mode]\ncommand = \"context-mode\"\nenabled = true\n\n[other]\nvalue = true\n"
	if err := os.WriteFile(filepath.Join(project, ".codex", "config.toml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldwd) }()
	legacyHooks := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"context-mode hook codex sessionstart"}]}],"PreToolUse":[{"hooks":[{"type":"command","command":"echo user"}]}]}}`
	if err := os.WriteFile(filepath.Join(project, ".codex", "hooks.json"), []byte(legacyHooks), 0o644); err != nil {
		t.Fatal(err)
	}
	if !wireCodexManual() {
		t.Fatal("wireCodexManual returned false")
	}
	data, err := os.ReadFile(filepath.Join(project, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "context-mode hook codex") || strings.Contains(s, "SessionStart") {
		t.Fatalf("project-local context-mode hook remains:\n%s", s)
	}
	if !strings.Contains(s, "echo user") {
		t.Fatalf("project-local user hook removed:\n%s", s)
	}
	configData, err := os.ReadFile(filepath.Join(project, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(configData)
	if strings.Contains(config, "[mcp_servers.context-mode]") || strings.Contains(config, "[mcp_servers.context_mode]") {
		t.Fatalf("project-local context-mode MCP block remains:\n%s", config)
	}
	if !strings.Contains(config, "[other]") {
		t.Fatalf("project-local unrelated config removed:\n%s", config)
	}
}

func TestWireAntigravity_McpAndGeminiMinimalContextModeHook(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("TOKLESS_TEST", "1")
	defer util.SetHomeOverride("")

	geminiPath := filepath.Join(tmp, ".gemini", "GEMINI.md")
	if err := os.MkdirAll(filepath.Dir(geminiPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(geminiPath, []byte("# User rules\n\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pluginHooks := filepath.Join(tmp, ".gemini", "config", "plugins", "context-mode", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(pluginHooks), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginHooks, []byte(`{"PreToolUse":[{"hooks":[{"command":"context-mode hook gemini-cli beforetool"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := ctxWireAntigravity(coreRunOptsForTest())
	if err != nil {
		t.Fatalf("ctxWireAntigravity error: %v", err)
	}
	if !ok {
		t.Fatal("ctxWireAntigravity returned false")
	}
	if !ctxVerifyAntigravity() {
		t.Fatal("ctxVerifyAntigravity returned false after wire")
	}

	geminiData, err := os.ReadFile(geminiPath)
	if err != nil {
		t.Fatalf("read GEMINI.md: %v", err)
	}
	gemini := string(geminiData)
	for _, want := range []string{"# User rules", "keep me", "## Context Tools (context-mode)"} {
		if !strings.Contains(gemini, want) {
			t.Fatalf("GEMINI.md missing %q:\n%s", want, gemini)
		}
	}
	if strings.Contains(gemini, "context_window_protection") || strings.Contains(gemini, "Codex CLI hooks provide") || strings.Contains(gemini, "context-mode hook codex") || strings.Contains(gemini, "context-mode hook gemini") {
		t.Fatalf("routing block should stay MCP + MD only:\n%s", gemini)
	}

	legacyRouting := filepath.Join(tmp, ".gemini", "config", "tokless", "context-mode-routing.md")
	if _, err := os.Stat(legacyRouting); err == nil {
		t.Fatalf("unexpected intermediate routing file written: %s", legacyRouting)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat legacy routing file: %v", err)
	}

	hooksPath := filepath.Join(tmp, ".gemini", "config", "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read antigravity hooks: %v", err)
	}
	hooks := string(data)
	for _, want := range []string{"context-mode hook antigravity-cli pretooluse", `"PreToolUse"`, "run_command|view_file|grep_search|web_fetch|read_url_content"} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("antigravity context-mode hook missing %q:\n%s", want, hooks)
		}
	}
	for _, bad := range []string{"PostToolUse", "Stop", "context-mode hook gemini", "beforetool"} {
		if strings.Contains(hooks, bad) {
			t.Fatalf("antigravity context-mode hook has unwanted %q:\n%s", bad, hooks)
		}
	}
	if _, err := os.Stat(filepath.Dir(pluginHooks)); err == nil {
		t.Fatalf("antigravity context-mode plugin hook dir should be removed: %s", filepath.Dir(pluginHooks))
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat plugin hook dir: %v", err)
	}

	settingsPath := filepath.Join(tmp, ".gemini", "antigravity-cli", "settings.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read antigravity settings: %v", err)
	}
	if strings.Contains(string(settingsData), "command(echo)") {
		t.Fatalf("context-mode hook should not add tokless echo permission:\n%s", settingsData)
	}
}

func TestCtxVerifyAntigravity_RequiresHook(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("TOKLESS_TEST", "1")
	defer util.SetHomeOverride("")

	geminiPath := filepath.Join(tmp, ".gemini", "GEMINI.md")
	if err := os.MkdirAll(filepath.Dir(geminiPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(geminiPath, []byte("# User rules\n\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if ctxVerifyAntigravity() {
		t.Fatal("ctxVerifyAntigravity should be false before wire")
	}
	if _, err := ctxWireAntigravity(coreRunOptsForTest()); err != nil {
		t.Fatalf("wire: %v", err)
	}
	if !ctxVerifyAntigravity() {
		t.Fatal("ctxVerifyAntigravity should be true after wire")
	}

	agents.RemoveAntigravityContextModeHook()
	if ctxVerifyAntigravity() {
		t.Fatal("ctxVerifyAntigravity should be false after hook removed")
	}
}

func TestCleanupLegacyAntigravityContextMode_PreservesMinimalHook(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	defer util.SetHomeOverride("")

	hooksPath := filepath.Join(tmp, ".gemini", "config", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{
		"hooks":{
			"PreToolUse":[
				{"matcher":"run_command|view_file|grep_search|web_fetch|read_url_content","hooks":[{"type":"command","command":"context-mode hook antigravity-cli pretooluse","timeout":10}]},
				{"matcher":"Bash","hooks":[{"type":"command","command":"tokless context-mode-hook agy pretooluse"}]}
			],
			"SessionStart":[{"hooks":[{"type":"command","command":"context-mode hook agy sessionstart"}]}]
		}
	}`
	if err := os.WriteFile(hooksPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	agents.CleanupLegacyAntigravityContextMode()

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "context-mode hook antigravity-cli pretooluse") {
		t.Fatalf("minimal hook removed:\n%s", s)
	}
	for _, bad := range []string{"tokless context-mode-hook", "context-mode hook agy sessionstart", "SessionStart"} {
		if strings.Contains(s, bad) {
			t.Fatalf("legacy hook remains %q:\n%s", bad, s)
		}
	}
}

func coreRunOptsForTest() core.RunOpts { return core.RunOpts{} }
