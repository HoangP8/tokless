package agents

import (
	"os"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func codexHookFixture(event, matcher string, commands ...string) *util.OrderedMap {
	cfg := util.NewOrderedMap()
	hooks := util.NewOrderedMap()
	groups := make([]any, 0, len(commands))
	for _, command := range commands {
		group := util.NewOrderedMap()
		group.Set("matcher", matcher)
		hook := util.NewOrderedMap()
		hook.Set("type", "command")
		hook.Set("command", command)
		group.Set("hooks", []any{hook})
		groups = append(groups, group)
	}
	hooks.Set(event, groups)
	cfg.Set("hooks", hooks)
	return cfg
}

func TestInstallCodexRtkHookMigratesManagedOnly(t *testing.T) {
	setTestHome(t)
	cfg := codexHookFixture("PreToolUse", codexHookMatcher,
		`C:\old\tokless.exe rtk-hook codex`,
		`D:\old\tokless.exe rtk-hook codex`,
		"custom-wrapper rtk-hook codex",
	)
	if err := util.WriteFile(codexHooksFile(), util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	InstallCodexRtkHook()
	raw, _ := os.ReadFile(codexHooksFile())
	if strings.Count(string(raw), `"command": "`+codexHookCommand()+`"`) != 1 {
		t.Fatalf("managed hooks not deduplicated: %s", raw)
	}
	if !strings.Contains(string(raw), "custom-wrapper rtk-hook codex") {
		t.Fatalf("wrapper removed: %s", raw)
	}
}

func TestRemoveCodexRtkHookPreservesWrapper(t *testing.T) {
	setTestHome(t)
	cfg := codexHookFixture("PreToolUse", codexHookMatcher,
		codexHookCommand(),
		"custom-wrapper rtk-hook codex",
	)
	if err := util.WriteFile(codexHooksFile(), util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}
	RemoveCodexRtkHook()
	raw, _ := os.ReadFile(codexHooksFile())
	if strings.Contains(string(raw), `"command": "`+codexHookCommand()+`"`) || !strings.Contains(string(raw), "custom-wrapper rtk-hook codex") {
		t.Fatalf("unexpected hooks after remove: %s", raw)
	}
}

func TestCodexTransformPreservesUnrelatedEmptyGroups(t *testing.T) {
	empty := util.NewOrderedMap()
	empty.Set("matcher", "user")
	empty.Set("hooks", []any{})
	managed := codexRtkGroup(codexHookCommand())
	out, pos, _, _ := codexTransformManagedGroups([]any{empty, managed}, codexHookMatcher, []string{"rtk-hook", "codex"}, codexRtkGroup(codexHookCommand()))
	if len(out) != 2 || out[0] != empty || pos.group != 1 || pos.hook != 0 {
		t.Fatalf("empty group lost or managed position changed: len=%d pos=%+v", len(out), pos)
	}
}

func TestCodexRtkHookKeepsUserTrustIndicesStable(t *testing.T) {
	setTestHome(t)
	p := util.CodexPathsResolved()
	hooksFile := codexHooksFile()
	cfg := codexHookFixture("PreToolUse", codexHookMatcher,
		`C:\old\tokless.exe rtk-hook codex`,
		`D:\old\tokless.exe rtk-hook codex`,
		"echo user",
	)
	if err := util.WriteFile(hooksFile, util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}
	userKey := hooksFile + ":pre_tool_use:2:0"
	userBlock := util.NewTomlBlock(codexHookStateHeader(userKey))
	userBlock.Set("trusted_hash", "user-trust")
	if err := util.WriteFile(p.Config, util.RenderBlock(userBlock)); err != nil {
		t.Fatal(err)
	}

	InstallCodexRtkHook()
	config, _ := os.ReadFile(p.Config)
	installedUserKey := hooksFile + ":pre_tool_use:1:0"
	if strings.Contains(string(config), codexHookStateHeader(userKey)) || !strings.Contains(string(config), codexHookStateHeader(installedUserKey)) || !strings.Contains(string(config), "user-trust") {
		t.Fatalf("user trust not remapped after install: %s", config)
	}

	RemoveCodexRtkHook()
	config, _ = os.ReadFile(p.Config)
	removedUserKey := hooksFile + ":pre_tool_use:0:0"
	if strings.Contains(string(config), codexHookStateHeader(installedUserKey)) || !strings.Contains(string(config), codexHookStateHeader(removedUserKey)) || !strings.Contains(string(config), "user-trust") {
		t.Fatalf("user trust not remapped on uninstall: %s", config)
	}
}

func TestCodexPermissionHookManagedOwnership(t *testing.T) {
	setTestHome(t)
	cfg := codexHookFixture("PermissionRequest", codexPermHookMatcher,
		`C:\old\tokless.exe codex-perm codex`,
		`D:\old\tokless.exe codex-perm codex`,
		"custom-wrapper codex-perm codex",
	)
	if err := util.WriteFile(codexHooksFile(), util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	InstallCodexPermissionHook()
	raw, _ := os.ReadFile(codexHooksFile())
	if strings.Count(string(raw), `"command": "`+codexPermHookCommand()+`"`) != 1 || !strings.Contains(string(raw), "custom-wrapper codex-perm codex") {
		t.Fatalf("unexpected hooks after install: %s", raw)
	}

	RemoveCodexPermissionHook()
	raw, _ = os.ReadFile(codexHooksFile())
	if strings.Contains(string(raw), `"command": "`+codexPermHookCommand()+`"`) || !strings.Contains(string(raw), "custom-wrapper codex-perm codex") {
		t.Fatalf("unexpected hooks after remove: %s", raw)
	}
}
