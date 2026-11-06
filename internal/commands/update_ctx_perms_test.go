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

			if out := dryRunUpdate(t); strings.Contains(out, "add tool permissions") {
				t.Fatalf("unowned wiring must not be flagged:\n%s", out)
			}
			tools.WriteOwner(tc.agent, "context-mode")
			if out := dryRunUpdate(t); !strings.Contains(out, "add tool permissions") {
				t.Fatalf("missing ctx_batch permission must be stale:\n%s", out)
			}

			resyncWiring([]*core.ToolManifest{ctxToolForTest(t)})
			raw, _ := util.ReadFileSafe(filepath.Join(home, tc.file))
			if !strings.Contains(raw, "ctx_batch\"") || !strings.Contains(raw, tc.keep) || !strings.Contains(raw, "ctx_batch_execute") {
				t.Fatalf("resync must add ctx_batch and keep existing entries:\n%s", raw)
			}
			if out := dryRunUpdate(t); strings.Contains(out, "add tool permissions") {
				t.Fatalf("second pass must be clean:\n%s", out)
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
