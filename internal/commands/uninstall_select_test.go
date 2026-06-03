package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// Non-interactive (no TTY in tests) + explicit flags: selective uninstall must
// remove only the chosen tool from the chosen agent, preserving the rest.
func TestUninstallSelectiveFlags(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	oc := filepath.Join(dir, ".config", "opencode")
	os.MkdirAll(oc, 0o755)
	// fake an opencode install (config dir present) + wired caveman + codegraph
	os.WriteFile(filepath.Join(oc, "opencode.json"),
		[]byte(`{"plugin":["./plugins/caveman/plugin.js","context-mode"],"mcp":{"caveman-shrink":{"type":"local"},"codegraph":{"type":"local"}}}`), 0o644)
	os.WriteFile(filepath.Join(oc, "AGENTS.md"), []byte("<!-- caveman-begin -->\nx\n<!-- caveman-end -->\n"), 0o644)

	code := RunUninstall(InitOptions{Agents: []string{"opencode"}, Tools: []string{"caveman"}})
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	b, _ := os.ReadFile(filepath.Join(oc, "opencode.json"))
	s := string(b)
	if strings.Contains(s, "caveman") {
		t.Fatal("caveman not fully removed from opencode.json")
	}
	if !strings.Contains(s, "codegraph") || !strings.Contains(s, "context-mode") {
		t.Fatal("non-selected tools were wrongly removed")
	}
	if amd, _ := os.ReadFile(filepath.Join(oc, "AGENTS.md")); strings.Contains(string(amd), "caveman-begin") {
		t.Fatal("caveman ruleset not removed")
	}
}

func ctxToolForTest(t *testing.T) *core.ToolManifest {
	t.Helper()
	for _, tl := range core.ListTools() {
		if tl.ID == "context-mode" {
			return tl
		}
	}
	t.Fatal("context-mode tool not registered")
	return nil
}

func opencodePlugins(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	cfg := util.TryParseJsonc(string(data))
	var out []string
	if pv, ok := cfg.Get("plugin"); ok {
		if arr, ok := pv.([]any); ok {
			for _, p := range arr {
				if s, ok := p.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// The reliability guarantee: after `tokless update`, resync re-wires context-mode
// on a wired agent, normalizing any stale pin back to the bare @latest entry.
func TestResyncWiring_NormalizesContextModeToBare(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	ocDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(ocDir, 0o755); err != nil {
		t.Fatal(err)
	}
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	defer util.SetHomeOverride("")

	ocJSON := filepath.Join(ocDir, "opencode.json")
	if err := os.WriteFile(ocJSON, []byte(`{"plugin":["context-mode@1.0.162"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	resyncWiring([]*core.ToolManifest{ctxToolForTest(t)})

	got := opencodePlugins(t, ocJSON)
	if len(got) != 1 || got[0] != "context-mode" {
		t.Fatalf("resync must normalize to bare: got %v want [context-mode]", got)
	}
}

// resync must NOT newly wire context-mode into an agent that never had it.
func TestResyncWiring_SkipsUnwiredAgent(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	ocDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(ocDir, 0o755); err != nil {
		t.Fatal(err)
	}
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	defer util.SetHomeOverride("")

	ocJSON := filepath.Join(ocDir, "opencode.json")
	if err := os.WriteFile(ocJSON, []byte(`{"plugin":["other@1.0.0"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	resyncWiring([]*core.ToolManifest{ctxToolForTest(t)})

	got := opencodePlugins(t, ocJSON)
	for _, p := range got {
		if p == "context-mode@1.0.162" || p == "context-mode" {
			t.Fatalf("resync must not newly wire an unwired agent, got %v", got)
		}
	}
}
