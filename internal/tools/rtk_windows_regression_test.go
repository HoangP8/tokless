package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func claudeHookJSON(command string) string {
	return `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + command + `"}]}]}}`
}

func TestClaudeRtkHookCommandWindowsPath(t *testing.T) {
	origIsWin := util.IsWin
	defer func() { util.IsWin = origIsWin }()
	util.IsWin = true

	got := claudeRtkHookCommand(`C:\Users\user\AppData\Local\Programs\tokless\tokless.exe`)
	want := "C:/Users/user/AppData/Local/Programs/tokless/tokless.exe rtk-hook claude"
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestClaudeRtkHookCommandSpacedPathFallsBackToPATH(t *testing.T) {
	origIsWin := util.IsWin
	defer func() { util.IsWin = origIsWin }()
	util.IsWin = true

	if got := claudeRtkHookCommand(`C:\Program Files\tokless\tokless.exe`); got != "tokless rtk-hook claude" {
		t.Fatalf("command = %q", got)
	}
}

func TestOverrideClaudeRtkHookMigratesManagedCommandsOnly(t *testing.T) {
	dir := t.TempDir()
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")

	cp := util.ClaudeCodePaths()
	if err := os.MkdirAll(cp.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		command string
		changed bool
	}{
		{"upstream", "rtk hook claude", true},
		{"old tokless path", `C:\Users\user\tokless.exe rtk-hook claude`, true},
		{"wrapper", "custom-wrapper rtk-hook claude", false},
		{"echo", "echo rtk-hook claude", false},
		{"chained", "tokless rtk-hook claude && echo done", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(cp.Settings, []byte(claudeHookJSON(strings.ReplaceAll(tt.command, `\`, `\\`))), 0o644); err != nil {
				t.Fatal(err)
			}
			overrideClaudeRtkHook()
			raw, err := os.ReadFile(cp.Settings)
			if err != nil {
				t.Fatal(err)
			}
			cfg := util.TryParseJsonc(string(raw))
			hooks, _ := cfg.Get("hooks")
			pre, _ := hooks.(*util.OrderedMap).Get("PreToolUse")
			group := pre.([]any)[0].(*util.OrderedMap)
			groupHooks, _ := group.Get("hooks")
			command, _ := groupHooks.([]any)[0].(*util.OrderedMap).Get("command")
			got, _ := command.(string)
			if tt.changed {
				if got != claudeRtkHookCommand(util.ToklessAbs()) {
					t.Fatalf("command = %q", got)
				}
			} else if got != tt.command {
				t.Fatalf("user command changed: got %q, want %q", got, tt.command)
			}
		})
	}
}

func TestRemoveClaudeRtkHookGroupPreservesUserSiblings(t *testing.T) {
	dir := t.TempDir()
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")
	cp := util.ClaudeCodePaths()

	cfg := util.NewOrderedMap()
	hooks := util.NewOrderedMap()
	entry := util.NewOrderedMap()
	entry.Set("matcher", "Bash")
	managed := util.NewOrderedMap()
	managed.Set("type", "command")
	managed.Set("command", "tokless rtk-hook claude")
	user := util.NewOrderedMap()
	user.Set("type", "command")
	user.Set("command", "custom-wrapper rtk-hook claude")
	sibling := util.NewOrderedMap()
	sibling.Set("type", "command")
	sibling.Set("command", "echo user")
	entry.Set("hooks", []any{managed, user, sibling})
	hooks.Set("PreToolUse", []any{entry})
	cfg.Set("hooks", hooks)
	if err := util.WriteFile(cp.Settings, util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	removeClaudeRtkHookGroup()
	raw, _ := os.ReadFile(cp.Settings)
	if strings.Contains(string(raw), `"command": "tokless rtk-hook claude"`) || !strings.Contains(string(raw), "custom-wrapper rtk-hook claude") || !strings.Contains(string(raw), "echo user") {
		t.Fatalf("unexpected hooks after remove: %s", raw)
	}
}

func TestClaudeRtkHookManaged(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"rtk hook claude", true},
		{"tokless rtk-hook claude", true},
		{`C:\bin\tokless.exe rtk-hook claude`, true},
		{"C:/bin/tokless.exe rtk-hook claude", true},
		{"custom-wrapper rtk-hook claude", false},
		{"echo rtk-hook claude", false},
		{"tokless rtk-hook claude --extra", false},
		{"tokless rtk-hook claude && echo done", false},
		{"other-tokless.exe rtk-hook claude", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := claudeRtkHookManaged(tt.command); got != tt.want {
			t.Errorf("managed(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

func TestOverrideClaudeRtkHookPreservesMixedGroup(t *testing.T) {
	dir := t.TempDir()
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")
	cp := util.ClaudeCodePaths()

	cfg := util.NewOrderedMap()
	hooks := util.NewOrderedMap()
	entry := util.NewOrderedMap()
	entry.Set("matcher", "Bash")
	managed := util.NewOrderedMap()
	managed.Set("type", "command")
	managed.Set("command", "rtk hook claude")
	user := util.NewOrderedMap()
	user.Set("type", "command")
	user.Set("command", "echo user")
	entry.Set("hooks", []any{managed, user})
	hooks.Set("PreToolUse", []any{entry})
	cfg.Set("hooks", hooks)
	if err := util.WriteFile(cp.Settings, util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	overrideClaudeRtkHook()
	raw, _ := os.ReadFile(cp.Settings)
	if strings.Count(string(raw), "rtk-hook claude") != 1 || !strings.Contains(string(raw), "echo user") {
		t.Fatalf("mixed group damaged: %s", raw)
	}
}

func TestOverrideClaudeRtkHookDeduplicatesManagedOnly(t *testing.T) {
	dir := t.TempDir()
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")
	cp := util.ClaudeCodePaths()

	cfg := util.NewOrderedMap()
	hooks := util.NewOrderedMap()
	groups := []any{}
	for _, cmd := range []string{"rtk hook claude", `C:\old\tokless.exe rtk-hook claude`, "echo user"} {
		entry := util.NewOrderedMap()
		entry.Set("matcher", "Bash")
		hook := util.NewOrderedMap()
		hook.Set("type", "command")
		hook.Set("command", cmd)
		entry.Set("hooks", []any{hook})
		groups = append(groups, entry)
	}
	hooks.Set("PreToolUse", groups)
	cfg.Set("hooks", hooks)
	if err := util.WriteFile(cp.Settings, util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	overrideClaudeRtkHook()
	raw, _ := os.ReadFile(cp.Settings)
	if strings.Count(string(raw), "rtk-hook claude") != 1 || !strings.Contains(string(raw), "echo user") {
		t.Fatalf("unexpected hooks: %s", raw)
	}
}
