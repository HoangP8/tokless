package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// headroom's own installer rewrote this file and dropped keys it didn't
// recognise. Ours must only ever touch the one variable.
func TestSetClaudeEnvKeepsEverythingElse(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"effortLevel":"max","env":{"KEEP_ME":"1"},"permissions":{"allow":["WebSearch"]}}`
	if err := os.WriteFile(settings, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if !SetClaudeEnv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8787") {
		t.Fatal("expected a change")
	}
	raw, _ := os.ReadFile(settings)
	body := string(raw)
	for _, want := range []string{`"effortLevel"`, `"max"`, `"KEEP_ME"`, `"WebSearch"`, "http://127.0.0.1:8787"} {
		if !strings.Contains(body, want) {
			t.Errorf("lost %s from settings:\n%s", want, body)
		}
	}
	if SetClaudeEnv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8787") {
		t.Error("writing the same value twice should be a no-op")
	}
	if got := ClaudeEnv("ANTHROPIC_BASE_URL"); got != "http://127.0.0.1:8787" {
		t.Errorf("ClaudeEnv = %q", got)
	}

	if !SetClaudeEnv("ANTHROPIC_BASE_URL", "") {
		t.Fatal("expected removal to change the file")
	}
	raw, _ = os.ReadFile(settings)
	body = string(raw)
	if strings.Contains(body, "ANTHROPIC_BASE_URL") {
		t.Errorf("variable survived removal:\n%s", body)
	}
	for _, want := range []string{`"effortLevel"`, `"KEEP_ME"`, `"WebSearch"`} {
		if !strings.Contains(body, want) {
			t.Errorf("removal lost %s:\n%s", want, body)
		}
	}
}

func TestAllowClaudeMcpToolProjectLocalPreservesAndAppends(t *testing.T) {
	proj := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	settings := filepath.Join(proj, ".claude", "settings.local.json")
	seed := `{
  "permissions": {
    "allow": [
      "WebSearch",
      "mcp__context-mode__ctx_search"
    ]
  }
}
`
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	AllowClaudeMcpToolProjectLocal("context-mode")
	AllowClaudeMcpToolProjectLocal("codegraph")
	AllowClaudeMcpToolProjectLocal("context-mode")

	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"WebSearch",
		"mcp__context-mode__ctx_search",
		"mcp__context-mode__ctx_execute",
		"mcp__context-mode__ctx_execute_file",
		"mcp__context-mode__ctx_batch_execute",
		"mcp__context-mode__ctx_index",
		"mcp__context-mode__ctx_fetch_and_index",
		"mcp__codegraph__.*",
		"mcp__codegraph__codegraph_explore",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
		if strings.Count(got, `"`+want+`"`) != 1 {
			t.Fatalf("duplicate %q in %s", want, got)
		}
	}
	if strings.Contains(got, "mcp__context-mode__.*") {
		t.Fatalf("context-mode wildcard should be migrated: %s", got)
	}
}

func TestAllowClaudeMcpToolProjectLocalCreatesFile(t *testing.T) {
	proj := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	AllowClaudeMcpToolProjectLocal("codegraph")

	raw, err := os.ReadFile(filepath.Join(proj, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "mcp__codegraph__.*") || !strings.Contains(got, "mcp__codegraph__codegraph_explore") {
		t.Fatalf("missing codegraph permissions in %s", got)
	}
}
