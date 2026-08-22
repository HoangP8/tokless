package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// assertContextModeProxy rejects upstream context-mode registrations. Every
// agent config must launch tokless first, with context-mode as its inner MCP.
func assertContextModeProxy(t *testing.T, registration, raw string) {
	t.Helper()
	want := util.PickMcpSpawn("context-mode")
	outer := regexp.MustCompile(`(?s)(?:"command"\s*:\s*(?:"|\[\s*")|command\s*=\s*")` + regexp.QuoteMeta(want.Command) + `"`)
	if !outer.MatchString(raw) {
		t.Fatalf("%s outer command is not tokless run-mcp proxy %q:\n%s", registration, want.Command, raw)
	}
	if !regexp.MustCompile(`(?s)run-mcp.*--context-mode.*context-mode`).MatchString(raw) {
		t.Fatalf("%s writes raw or incomplete context-mode registration:\n%s", registration, raw)
	}
	if regexp.MustCompile(`(?m)(?:"command"\s*:\s*"context-mode"|command\s*=\s*"context-mode")`).MatchString(raw) {
		t.Fatalf("%s uses context-mode as outer command instead of tokless proxy:\n%s", registration, raw)
	}
}

func TestContextModeRegistrationsUseToklessProxy(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})

	tests := []struct {
		name string
		wire func() (bool, string)
	}{
		{"claude", func() (bool, string) { return agents.ConfigureClaudeMcp("context-mode") }},
		{"opencode", func() (bool, string) { return agents.ConfigureOpenCodeMcp("context-mode") }},
		{"codex", func() (bool, string) { return agents.ConfigureCodexMcp("context-mode") }},
		{"antigravity", func() (bool, string) { return agents.ConfigureAntigravityMcp("context-mode") }},
		{"copilot-cli", func() (bool, string) { return agents.ConfigureCopilotMcp("context-mode") }},
		{"copilot-ide", func() (bool, string) { return agents.ConfigureCopilotIdeMcp("context-mode") }},
		{"droid", func() (bool, string) { return agents.ConfigureDroidMcp("context-mode") }},
		{"pi", func() (bool, string) { return agents.ConfigurePiMcp("context-mode") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed, file := tt.wire()
			if !changed || file == "" {
				t.Fatalf("registration did not write: changed=%v file=%q", changed, file)
			}
			raw, ok := util.ReadFileSafe(file)
			if !ok {
				t.Fatalf("read registration %s", file)
			}
			assertContextModeProxy(t, tt.name, raw)
		})
	}
}

// TestWireCodexManual_BoundedShape verifies that wireCodexManual writes MCP +
// AGENTS.md without a context-mode PreToolUse hook.
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
	if _, err := os.Stat(hooksPath); !os.IsNotExist(err) {
		t.Fatalf("context-mode should not create hooks.json, err=%v", err)
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
	if !ctxVerifyCodex() {
		t.Error("ctxVerifyCodex returned false for MCP + AGENTS.md install")
	}
}

func TestCtxWireCodexWithoutCLI(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TOKLESS_TEST", "")
	t.Cleanup(func() { util.SetHomeOverride("") })

	if ok, err := ctxWireCodex(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("ctxWireCodex without CLI = %v, %v; want true, nil", ok, err)
	}
	if !ctxVerifyCodex() {
		t.Fatal("Codex shared config was not wired")
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

func TestCtxVerifyCodexRejectsLegacyPreToolUseHook(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	defer util.SetHomeOverride("")

	if !wireCodexManual() {
		t.Fatal("wireCodexManual returned false")
	}
	hooks := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"context-mode hook codex pretooluse"}]}]}}`
	if err := os.WriteFile(filepath.Join(tmp, ".codex", "hooks.json"), []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}
	if ctxVerifyCodex() {
		t.Fatal("ctxVerifyCodex accepted legacy context-mode PreToolUse hook")
	}
}

func TestCtxVerifyCodexRejectsProjectLegacyPreToolUseHook(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	util.SetHomeOverride(filepath.Join(tmp, "home"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	defer util.SetHomeOverride("")
	if err := os.MkdirAll(filepath.Join(project, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldwd, _ := os.Getwd()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldwd) }()
	if !wireCodexManual() {
		t.Fatal("wireCodexManual returned false")
	}
	hooks := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"context-mode hook codex pretooluse"}]}]}}`
	if err := os.WriteFile(filepath.Join(project, ".codex", "hooks.json"), []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}
	if ctxVerifyCodex() {
		t.Fatal("ctxVerifyCodex accepted project legacy context-mode hook")
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

func TestWireAntigravity_McpAndGeminiNoContextModeHook(t *testing.T) {
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
	for _, bad := range []string{"context-mode hook antigravity-cli pretooluse", "run_command|view_file|grep_search|web_fetch|read_url_content", "PostToolUse", "Stop", "context-mode hook gemini", "beforetool"} {
		if strings.Contains(hooks, bad) {
			t.Fatalf("antigravity hooks should not contain context-mode hook artifact %q:\n%s", bad, hooks)
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

func TestCtxVerifyAntigravity_RequiresNoHook(t *testing.T) {
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

	agents.InstallAntigravityContextModeHook()
	if ctxVerifyAntigravity() {
		t.Fatal("ctxVerifyAntigravity should be false when context-mode hook exists")
	}
}

func TestCleanupLegacyAntigravityContextMode_DropsAllContextModeHooks(t *testing.T) {
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
	s := ""
	if err == nil {
		s = string(data)
	}
	for _, bad := range []string{"context-mode hook", "tokless context-mode-hook", "context-mode hook agy sessionstart", "SessionStart"} {
		if strings.Contains(s, bad) {
			t.Fatalf("context-mode hook remains %q:\n%s", bad, s)
		}
	}
}

func coreRunOptsForTest() core.RunOpts { return core.RunOpts{} }
