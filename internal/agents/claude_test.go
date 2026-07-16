package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		"mcp__context-mode__.*",
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
