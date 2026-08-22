package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func TestCodexUninstall_PreservesUserConfig_Surgical(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	codexDir := filepath.Join(home, ".codex")
	util.EnsureDir(filepath.Join(codexDir, "rules"))
	userConfigToml := `approval_policy = "never"
[mcp_servers.myserver]
command = "myserver"
`
	os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(userConfigToml), 0o644)
	userRules := `# my personal rules
prefix_rule(pattern = ["myapp"], decision = "allow")
`
	os.WriteFile(filepath.Join(codexDir, "rules", "default.rules"), []byte(userRules), 0o644)
	userHooks := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"my-hook"}]}]}}`
	os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(userHooks), 0o644)
	os.WriteFile(filepath.Join(codexDir, "AGENTS.md"), []byte("# My codex notes\n"), 0o644)

	// Wire context-mode + rtk into codex.
	ctxTool := toolByID(t, "context-mode")
	rtkTool := toolByID(t, "rtk")
	for _, tm := range []*core.ToolManifest{rtkTool, ctxTool} {
		if fn, ok := tm.WireFor["codex"]; ok {
			if _, err := fn(core.RunOpts{}); err != nil {
				t.Fatalf("wire %s codex: %v", tm.ID, err)
			}
		}
	}

	// Assert approval_policy preserved.
	cfg, _ := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if v := util.GetTomlTopKey(string(cfg), "approval_policy"); v != "never" {
		t.Fatalf("approval_policy should be user's 'never', got %q (we must not overwrite)", v)
	}
	// Assert rules file preserved.
	rules, _ := os.ReadFile(filepath.Join(codexDir, "rules", "default.rules"))
	if !strings.Contains(string(rules), "my personal rules") {
		t.Fatalf("user rules file was overwritten:\n%s", string(rules))
	}
	// Assert features.hooks was added.
	if !util.HasBlock(string(cfg), "features") {
		t.Fatal("features.hooks block not added by wire")
	}
	// Assert tokless hook groups added.
	hooks, _ := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	if !strings.Contains(string(hooks), "rtk-hook codex") {
		t.Fatal("rtk hook group not added")
	}
	// User's hook still present.
	if !strings.Contains(string(hooks), "my-hook") {
		t.Fatal("user's PreToolUse hook was removed during wire")
	}

	// Unwire both tools (real file ops on the test tempdir — safe, no external calls).
	for _, tm := range []*core.ToolManifest{rtkTool, ctxTool} {
		if fn, ok := tm.UnwireFor["codex"]; ok {
			_, _ = fn(core.RunOpts{})
		}
	}

	// Post-unwire assertions: surgical removal of ONLY tokless injections.
	cfg2, _ := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	cfgStr := string(cfg2)
	// approval_policy still user's "never".
	if v := util.GetTomlTopKey(cfgStr, "approval_policy"); v != "never" {
		t.Fatalf("approval_policy changed after uninstall: %q (must preserve user value)", v)
	}
	// features.hooks removed (no tokless hooks remain).
	if util.HasBlock(cfgStr, "features") {
		t.Fatal("features.hooks block not removed after uninstall")
	}
	// User's mcp_servers.myserver preserved.
	if !util.HasBlock(cfgStr, "mcp_servers.myserver") {
		t.Fatal("user's mcp_servers.myserver was removed — uninstall must be surgical")
	}
	// rules file untouched (was user's, we never overwrote it).
	rules2, _ := os.ReadFile(filepath.Join(codexDir, "rules", "default.rules"))
	if !strings.Contains(string(rules2), "my personal rules") {
		t.Fatalf("user rules file altered after uninstall:\n%s", string(rules2))
	}
	// hooks.json: tokless groups removed, user's hook stays.
	hooks2, _ := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	hooksStr := string(hooks2)
	if strings.Contains(hooksStr, "rtk-hook codex") {
		t.Fatal("rtk hook group not removed")
	}
	if strings.Contains(hooksStr, "context-mode hook codex") {
		t.Fatal("context-mode hook group not removed")
	}
	if !strings.Contains(hooksStr, "my-hook") {
		t.Fatal("user's PreToolUse hook removed — uninstall must preserve user hooks")
	}
}

// Real-world scenario: claude settings has user's permissions.allow.
func TestClaudeRtkUninstall_PreservesUserAllowlist(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))

	cp := util.ClaudeCodePaths()
	util.EnsureDir(cp.Dir)
	userSettings := `{"permissions":{"allow":["Bash(git *)","Read(./**)"]},"env":{"FOO":"bar"}}`
	os.WriteFile(cp.Settings, []byte(userSettings), 0o644)

	// Wire rtk claude (real file ops on tempdir; isTest gates external calls).
	rtkTool := toolByID(t, "rtk")
	if fn, ok := rtkTool.WireFor["claude"]; ok {
		_, _ = fn(core.RunOpts{})
	}
	agents.AllowClaudeBashPattern("Bash(rtk *)")

	// Assert Bash(rtk *) added, user entries preserved.
	raw, _ := os.ReadFile(cp.Settings)
	s := string(raw)
	if !strings.Contains(s, "Bash(rtk *)") {
		t.Fatal("Bash(rtk *) not added by wire")
	}
	if !strings.Contains(s, "Bash(git *)") || !strings.Contains(s, "Read(./**)") {
		t.Fatal("user's allow entries lost during wire")
	}

	// Unwire rtk claude (real file ops — removes Bash(rtk *) only).
	if fn, ok := rtkTool.UnwireFor["claude"]; ok {
		_, _ = fn(core.RunOpts{})
	}

	// Assert Bash(rtk *) removed, user entries preserved, env preserved.
	raw2, _ := os.ReadFile(cp.Settings)
	s2 := string(raw2)
	t.Logf("post-unwire settings.json: %s", s2)
	if strings.Contains(s2, "Bash(rtk *)") {
		t.Fatal("Bash(rtk *) not removed after uninstall")
	}
	if !strings.Contains(s2, "Bash(git *)") {
		t.Fatal("user's Bash(git *) removed — uninstall must be surgical")
	}
	if !strings.Contains(s2, "Read(./**)") {
		t.Fatal("user's Read(./**) removed — uninstall must be surgical")
	}
	// env must survive the parse/stringify round-trip.
	cfg2 := util.TryParseJsonc(s2)
	if cfg2 == nil {
		t.Fatal("settings.json invalid JSON after uninstall")
	}
	// env is top-level in claude settings — must survive uninstall untouched.
	envTop, ok := cfg2.Get("env")
	if !ok {
		t.Fatal("top-level env key lost after uninstall")
	}
	envMap, ok := envTop.(*util.OrderedMap)
	if !ok {
		t.Fatalf("env is not OrderedMap: %T", envTop)
	}
	foo, _ := envMap.Get("FOO")
	if s, _ := foo.(string); s != "bar" {
		t.Fatalf("env.FOO lost after uninstall: %v", foo)
	}
	// permissions.allow must still hold user entries (Bash(rtk *) removed).
	perms2, ok := cfg2.Get("permissions")
	if !ok {
		t.Fatal("permissions key lost after uninstall")
	}
	pm2 := perms2.(*util.OrderedMap)
	allowV, _ := pm2.Get("allow")
	allowArr := allowV.([]any)
	for _, e := range allowArr {
		if s, _ := e.(string); s == "Bash(rtk *)" {
			t.Fatal("Bash(rtk *) not removed from permissions.allow")
		}
	}
}

// Real-world scenario: antigravity settings has user permissions.
func TestAntigravityUninstall_SurgicalAllowlist(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	gemini := filepath.Join(home, ".gemini")
	util.EnsureDir(filepath.Join(gemini, "antigravity-cli"))
	userSettings := `{"permissions":{"allow":["Read(./**)"]}}`
	os.WriteFile(filepath.Join(gemini, "antigravity-cli", "settings.json"), []byte(userSettings), 0o644)

	// Wire rtk (adds command(rtk )) + codegraph (adds mcp(codegraph/*)).
	rtkTool := toolByID(t, "rtk")
	if fn, ok := rtkTool.WireFor["antigravity"]; ok {
		_, _ = fn(core.RunOpts{})
	}
	cgTool := toolByID(t, "codegraph")
	if fn, ok := cgTool.WireFor["antigravity"]; ok {
		_, _ = fn(core.RunOpts{})
	}
	// Apply the real permission additions the wires do (rtk hook adds command(rtk )).
	agents.AllowAntigravityEntry("command(rtk )")
	agents.AllowAntigravityEntry("mcp(codegraph/*)")

	raw, _ := os.ReadFile(filepath.Join(gemini, "antigravity-cli", "settings.json"))
	s := string(raw)
	for _, want := range []string{"command(rtk )", "mcp(codegraph/*)", "Read(./**)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("%q not present after wire: %s", want, s)
		}
	}

	// Unwire rtk only — command(rtk ) removed, mcp(codegraph/*) + user entry stay.
	if fn, ok := rtkTool.UnwireFor["antigravity"]; ok {
		_, _ = fn(core.RunOpts{})
	}
	raw2, _ := os.ReadFile(filepath.Join(gemini, "antigravity-cli", "settings.json"))
	s2 := string(raw2)
	if strings.Contains(s2, "command(rtk )") {
		t.Fatal("command(rtk ) not removed by rtk unwire")
	}
	if !strings.Contains(s2, "mcp(codegraph/*)") {
		t.Fatal("mcp(codegraph/*) wrongly removed by rtk unwire — must stay (codegraph still wired)")
	}
	if !strings.Contains(s2, "Read(./**)") {
		t.Fatal("user's Read(./**) removed by rtk unwire — must be surgical")
	}

	// Unwire codegraph — mcp(codegraph/*) removed, user entry stays.
	if fn, ok := cgTool.UnwireFor["antigravity"]; ok {
		_, _ = fn(core.RunOpts{})
	}
	raw3, _ := os.ReadFile(filepath.Join(gemini, "antigravity-cli", "settings.json"))
	s3 := string(raw3)
	if strings.Contains(s3, "mcp(codegraph/*)") {
		t.Fatal("mcp(codegraph/*) not removed by codegraph unwire")
	}
	if !strings.Contains(s3, "Read(./**)") {
		t.Fatal("user's Read(./**) removed — uninstall must preserve user permissions")
	}
}

func TestAntigravityContextMode_NoPreToolUseHook(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	gemini := filepath.Join(home, ".gemini")
	util.EnsureDir(filepath.Join(gemini, "config"))
	util.EnsureDir(filepath.Join(gemini, "antigravity-cli"))
	os.WriteFile(filepath.Join(gemini, "config", "hooks.json"), []byte(`{
	  "context-mode": {
	    "PreToolUse": [{
	      "matcher": "run_command|view_file|grep_search|web_fetch|read_url_content",
	      "hooks": [{"type": "command", "command": "context-mode hook antigravity-cli pretooluse", "timeout": 10}]
	    }]
	  },
	  "rtk": {
	    "PreToolUse": [{
	      "matcher": "",
	      "hooks": [{"type": "command", "command": "tokless rtk-hook agy", "timeout": 10}]
	    }]
	  }
	}`), 0o644)

	ctxTool := toolByID(t, "context-mode")
	if fn, ok := ctxTool.WireFor["antigravity"]; ok {
		ok, err := fn(core.RunOpts{})
		if err != nil || !ok {
			t.Fatalf("wire context-mode antigravity: ok=%v err=%v", ok, err)
		}
	}

	hooksPath := filepath.Join(gemini, "config", "hooks.json")
	raw, _ := os.ReadFile(hooksPath)
	s := string(raw)
	if strings.Contains(s, "context-mode hook antigravity-cli pretooluse") {
		t.Fatalf("context-mode PreToolUse hook should be removed for antigravity: %s", s)
	}
	if !strings.Contains(s, "rtk-hook agy") {
		t.Fatalf("non-context-mode hooks must survive cleanup: %s", s)
	}
	if !agents.AntigravityMcpHas("context-mode") {
		t.Fatal("context-mode MCP not installed for antigravity")
	}
	if agents.HasAntigravityContextModeHook() {
		t.Fatal("antigravity should not have a context-mode PreToolUse hook")
	}

	if fn, ok := ctxTool.UnwireFor["antigravity"]; ok {
		_, _ = fn(core.RunOpts{})
	}
	raw2, _ := os.ReadFile(hooksPath)
	s2 := string(raw2)
	if strings.Contains(s2, "context-mode") {
		t.Fatalf("context-mode hook group should stay removed after unwire: %s", s2)
	}
	if !strings.Contains(s2, "rtk-hook agy") {
		t.Fatalf("unwire removed unrelated rtk hook: %s", s2)
	}
}

func toolByID(t *testing.T, id string) *core.ToolManifest {
	t.Helper()
	for _, tm := range core.ListTools() {
		if tm.ID == id {
			return tm
		}
	}
	t.Fatalf("tool %q not registered", id)
	return nil
}

func getOrCreateMapTest(cfg *util.OrderedMap, key string) *util.OrderedMap {
	v, ok := cfg.Get(key)
	if ok {
		if m, ok := v.(*util.OrderedMap); ok {
			return m
		}
	}
	m := util.NewOrderedMap()
	cfg.Set(key, m)
	return m
}
