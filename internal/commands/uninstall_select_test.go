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

// Non-interactive (no TTY in tests) + explicit flags: selective uninstall must
// remove only the chosen tool from the chosen agent, preserving the rest.
func TestUninstallSelectiveFlags(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	oc := filepath.Join(dir, ".config", "opencode")
	os.MkdirAll(oc, 0o755)
	// fake an opencode install (config dir present) + wired caveman + codegraph
	os.WriteFile(filepath.Join(oc, "opencode.json"),
		[]byte(`{"plugin":["./plugins/caveman/plugin.js","context-mode"],"mcp":{"caveman-shrink":{"type":"local"},"codegraph":{"type":"local"}}}`), 0o644)
	os.WriteFile(filepath.Join(oc, "AGENTS.md"), []byte("## Caveman\nkeep me\n"), 0o644)

	code := RunUninstall(InitOptions{Agents: []string{"opencode"}, Tools: []string{"caveman"}})
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	b, _ := os.ReadFile(filepath.Join(oc, "opencode.json"))
	s := string(b)
	if !strings.Contains(s, "codegraph") || !strings.Contains(s, "context-mode") {
		t.Fatal("non-selected tools were wrongly removed")
	}
	if amd, _ := os.ReadFile(filepath.Join(oc, "AGENTS.md")); strings.Contains(string(amd), "## Caveman") {
		t.Fatal("caveman ruleset not removed")
	}
}

func TestUninstallSelectiveKiloDoesNotCleanupProjectConfig(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Cleanup(func() { util.SetHomeOverride("") })
	global := util.KiloPathsResolved().Dir
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if _, _, err = agents.ConfigureKiloMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, ".kilo", "kilo.jsonc")
	if code := RunUninstall(InitOptions{Agents: []string{"kilo"}, Tools: []string{"context-mode"}}); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatalf("selective uninstall created project Kilo config: %v", err)
	}
	if _, err := os.Stat(util.KiloPathsResolved().Config); err != nil {
		t.Fatalf("selective uninstall removed global Kilo config: %v", err)
	}
	if agents.KiloMcpConfigured("context-mode") {
		t.Fatal("selective uninstall left global Kilo MCP entry wired")
	}
}

func TestUninstallKiloLeavesLegacyProjectArtifactsUntouched(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Cleanup(func() { util.SetHomeOverride("") })

	global := util.KiloPathsResolved().Dir
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	oc := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(oc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oc, "opencode.json"), []byte(`{"mcp":{"foreign":{"type":"local"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if _, _, err = agents.ConfigureKiloMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(root, ".kilo")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyConfig := filepath.Join(legacyDir, "kilo.jsonc")
	legacyMarker := filepath.Join(legacyDir, ".tokless-kilo-config-created")
	if err := os.WriteFile(legacyConfig, []byte(`{"provider":"user"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyMarker, []byte("tokless\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := RunUninstall(InitOptions{}); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	for _, path := range []string{legacyConfig, legacyMarker, legacyDir} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("legacy Kilo project artifact was removed: %s (err=%v)", path, err)
		}
	}
	if agents.KiloMcpConfigured("context-mode") {
		t.Fatal("full uninstall left global Kilo MCP entry wired")
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
// on a wired agent.
func TestResyncWiring_RepinsContextModeVersion(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	ocDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(ocDir, 0o755); err != nil {
		t.Fatal(err)
	}
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	defer util.SetHomeOverride("")

	ocJSON := filepath.Join(ocDir, "opencode.json")
	if err := os.WriteFile(ocJSON, []byte(`{"plugin":["context-mode@0.0.1"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	resyncWiring([]*core.ToolManifest{ctxToolForTest(t)})

	got := opencodePlugins(t, ocJSON)
	if len(got) != 0 {
		t.Fatalf("resync must remove context-mode plugin: got %v", got)
	}
	raw, err := os.ReadFile(ocJSON)
	if err != nil || !strings.Contains(string(raw), "\"context-mode\"") || !strings.Contains(string(raw), "--context-mode") {
		t.Fatalf("resync must wire bounded context-mode MCP: %s", raw)
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
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
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

func TestResyncInstructionWiring_RefreshesStaleBody(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	defer util.SetHomeOverride("")

	path := filepath.Join(home, ".config", "opencode", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# User notes\n\nkeep me\n\n## Response Style (caveman)\nstale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resyncInstructionWiring()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "stale") {
		t.Fatalf("stale instruction body retained:\n%s", body)
	}
	if !strings.Contains(body, "# User notes") || !strings.Contains(body, "keep me") {
		t.Fatalf("user content lost:\n%s", body)
	}
	if !strings.Contains(body, "## Response Style (caveman)") {
		t.Fatalf("caveman section missing:\n%s", body)
	}
}
