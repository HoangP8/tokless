package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

// Update heals stale tool permissions, keeps user entries, then reports up to date.
func TestRunUpdateAddsMissingCtxToolPermissions(t *testing.T) {
	oldTools := []string{"ctx_search", "ctx_execute", "ctx_execute_file", "ctx_batch_execute", "ctx_index", "ctx_fetch_and_index"}
	jsonOf := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, tc := range []struct {
		name, agent string
		seed        func(t *testing.T, home string, spawn util.McpSpawn)
		file        string
		keep        string
	}{
		{
			name: "claude", agent: "claude",
			seed: func(t *testing.T, home string, spawn util.McpSpawn) {
				var allow []string
				for _, n := range oldTools {
					allow = append(allow, "mcp__context-mode__"+n)
				}
				allow = append(allow, "Bash(ls)")
				write(t, filepath.Join(home, ".claude.json"), jsonOf(map[string]any{"mcpServers": map[string]any{
					"context-mode": map[string]any{"type": "stdio", "command": spawn.Command, "args": spawn.Args},
				}}))
				write(t, filepath.Join(home, ".claude", "settings.json"), jsonOf(map[string]any{"permissions": map[string]any{"allow": allow}}))
			},
			file: filepath.Join(".claude", "settings.json"),
			keep: "Bash(ls)",
		},
		{
			name: "droid", agent: "droid",
			seed: func(t *testing.T, home string, spawn util.McpSpawn) {
				write(t, filepath.Join(home, ".factory", "mcp.json"), jsonOf(map[string]any{"mcpServers": map[string]any{
					"context-mode": map[string]any{"command": spawn.Command, "args": spawn.Args, "enabledTools": oldTools},
				}}))
			},
			file: filepath.Join(".factory", "mcp.json"),
			keep: "ctx_search",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctxAgentEnv(t, false)
			t.Setenv("TOKLESS_TEST", "1")
			seedAgentInstalled(t, tc.agent)
			home := util.Home()
			tc.seed(t, home, util.McpSpawnFor("context-mode"))

			if tools.CtxToolPermsStale() {
				t.Fatal("unowned wiring must not be flagged")
			}
			tools.WriteOwner(tc.agent, "context-mode")
			if !tools.CtxToolPermsStale() {
				t.Fatal("missing ctx_batch permission must be stale")
			}

			if out := noInstallUpdate(t); strings.Contains(out, "↑") {
				t.Fatalf("permission heal must not look like an upgrade:\n%s", out)
			}
			raw, _ := util.ReadFileSafe(filepath.Join(home, tc.file))
			if !strings.Contains(raw, "ctx_batch\"") || !strings.Contains(raw, tc.keep) || !strings.Contains(raw, "ctx_batch_execute") {
				t.Fatalf("resync must add ctx_batch and keep existing entries:\n%s", raw)
			}
			if tools.CtxToolPermsStale() {
				t.Fatal("second pass must be clean")
			}
		})
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An owned entry without --ctx-agent is stale for update; resync writes the host id.
func TestUpdateRefreshesContextModeHostFlags(t *testing.T) {
	ctxAgentEnv(t, false)
	t.Setenv("TOKLESS_TEST", "1")
	seedAgentInstalled(t, "claude")
	home := util.Home()
	spawn := util.McpSpawnFor("context-mode") // pre-flag shape
	body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"context-mode": map[string]any{"type": "stdio", "command": spawn.Command, "args": spawn.Args},
	}})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, ".claude.json"), string(body))
	if tools.CtxSpawnStale() {
		t.Fatal("unowned wiring must not be flagged")
	}
	tools.WriteOwner("claude", "context-mode")
	if !tools.CtxSpawnStale() {
		t.Fatal("owned entry without --ctx-agent must be stale")
	}
	if out := noInstallUpdate(t); strings.Contains(out, "↑") {
		t.Fatalf("host id heal must not look like an upgrade:\n%s", out)
	}
	raw, _ := util.ReadFileSafe(filepath.Join(home, ".claude.json"))
	if !strings.Contains(raw, "--ctx-agent") || !strings.Contains(raw, "claude-code") || tools.CtxSpawnStale() {
		t.Fatalf("resync must write --ctx-agent claude-code:\n%s", raw)
	}
}

// noInstallUpdate runs a real scoped update with Install stubbed; it fails if Install runs.
func noInstallUpdate(t *testing.T) string {
	t.Helper()
	tool := ctxToolForTest(t)
	old := tool.Install
	tool.Install = func(core.RunOpts) (bool, error) {
		t.Error("wiring-only refresh must not reinstall context-mode")
		return true, nil
	}
	t.Cleanup(func() { tool.Install = old })
	return captureStdout(t, func() { RunUpdate(InitOptions{Yes: true, Tools: []string{"context-mode"}}) })
}

// A stale entry tokless cannot rewrite must not reinstall or re-report on every update.
func TestUpdateUnfixableCtxWiringIsQuiet(t *testing.T) {
	ctxAgentEnv(t, false)
	t.Setenv("TOKLESS_TEST", "1")
	seedAgentInstalled(t, "claude")
	home := util.Home()
	spawn := util.ContextModeSpawnFor("claude")
	allow := []string{"mcp__context-mode__ctx_search"} // ctx_batch missing
	body, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"context-mode": map[string]any{"type": "stdio", "command": spawn.Command, "args": spawn.Args},
	}})
	write(t, filepath.Join(home, ".claude.json"), string(body))
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{"allow": allow}})
	write(t, filepath.Join(home, ".claude", "settings.json"), string(settings))
	tools.WriteOwner("claude", "context-mode")
	dir := filepath.Join(home, ".claude")
	if err := os.Chmod(dir, 0o555); err != nil { // wire cannot write: stays stale
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if !tools.CtxToolPermsStale() {
		t.Fatal("setup: permission must be stale")
	}
	noInstallUpdate(t)
	out := noInstallUpdate(t)
	if strings.Contains(out, "↑") || strings.Contains(out, "permissions") || strings.Contains(out, "failed") {
		t.Fatalf("second update must report nothing for an unfixable entry:\n%s", out)
	}
}
