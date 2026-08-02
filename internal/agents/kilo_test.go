package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func withKiloProject(t *testing.T) string {
	t.Helper()
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
	return root
}

func TestKiloConfigResolutionIndependentOfOpenCode(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("KILO_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(home, "other"))
	got := util.KiloPathsResolved()
	want := filepath.Join(home, "xdg", "kilo")
	if got.Dir != want || got.Config != filepath.Join(want, "kilo.jsonc") {
		t.Fatalf("Kilo paths = %+v, want %s", got, want)
	}
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(home, "custom"))
	if got := util.KiloPathsResolved().Dir; got != filepath.Join(home, "custom") {
		t.Fatalf("KILO_CONFIG_DIR ignored: %s", got)
	}
}

func TestKiloInstructionsPathUsesGlobalAgentsFile(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(home, "kilo"))
	want := filepath.Join(home, "kilo", "AGENTS.md")
	if got := KiloInstructionsPath(); got != want {
		t.Fatalf("KiloInstructionsPath = %s, want %s", got, want)
	}
}

func TestKiloProjectOnlyConfigPreservesMCPAndInstructions(t *testing.T) {
	root := withKiloProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "global"))
	config := filepath.Join(root, ".kilo", "kilo.jsonc")
	if err := util.WriteFile(config, `{"provider":"user","instructions":["user.md"],"mcp":{"foreign":{"type":"local","command":["foreign"],"enabled":true}}}`); err != nil {
		t.Fatal(err)
	}
	changed, file := ConfigureKiloMcp("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"})
	if !changed || file != config {
		t.Fatalf("ConfigureKiloMcp = %v, %q", changed, file)
	}
	if _, err := os.Stat(filepath.Join(util.KiloPathsResolved().Dir, "AGENTS.md")); err == nil {
		t.Fatal("shared Kilo AGENTS.md written")
	}
	raw, ok := util.ReadFileSafe(config)
	if !ok || !strings.Contains(raw, `"foreign"`) || !strings.Contains(raw, `"user.md"`) {
		t.Fatalf("user config not preserved: %s", raw)
	}
	if changed, _ := ConfigureKiloMcp("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); changed {
		t.Fatal("second Kilo configure was not idempotent")
	}
	if !KiloMcpConfigured("context-mode") || !KiloMcpMatches("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}) {
		t.Fatal("exact Kilo MCP verification failed")
	}
}

func TestKiloProjectOnlyAndRemoval(t *testing.T) {
	root := withKiloProject(t)
	if KiloProjectAvailable() == false || KiloProjectConfigPath() != filepath.Join(root, ".kilo", "kilo.jsonc") {
		t.Fatal("Kilo project path not detected")
	}
	if err := util.WriteFile(KiloProjectConfigPath(), `{"instructions":["user.md"],"mcp":{"foreign":{"type":"local","command":["foreign"],"enabled":true}}}`); err != nil {
		t.Fatal(err)
	}
	ConfigureKiloMcp("codegraph", []string{"tokless", "run-mcp", "--agent", "kilo", "codegraph", "serve", "--mcp"})
	if !RemoveKiloMcp("codegraph") || KiloMcpConfigured("codegraph") {
		t.Fatal("Kilo MCP removal failed")
	}
	raw, _ := util.ReadFileSafe(KiloProjectConfigPath())
	if !strings.Contains(raw, "foreign") || !strings.Contains(raw, "user.md") {
		t.Fatalf("removal damaged user config: %s", raw)
	}
}

func TestKiloLinkedWorktreeGitFileAccepted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /tmp/worktree\n"), 0o644); err != nil {
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
	if !KiloProjectAvailable() || KiloProjectConfigPath() != filepath.Join(root, ".kilo", "kilo.jsonc") {
		t.Fatal("Kilo linked worktree was not detected")
	}
}

func TestCleanupKiloProjectRemovesToklessOnlyConfig(t *testing.T) {
	root := withKiloProject(t)
	ConfigureKiloMcp("context-mode", []string{"tokless", "run-mcp"})
	if !RemoveKiloMcp("context-mode") {
		t.Fatal("Kilo MCP removal failed")
	}
	CleanupKiloProject()
	if _, err := os.Stat(filepath.Join(root, ".kilo")); !os.IsNotExist(err) {
		t.Fatalf("Tokless-only .kilo directory remains: %v", err)
	}
}

func TestCleanupKiloProjectPreservesForeignConfig(t *testing.T) {
	root := withKiloProject(t)
	config := filepath.Join(root, ".kilo", "kilo.jsonc")
	content := "// user comment\n{\"provider\":\"user\"}\n"
	if err := util.WriteFile(config, content); err != nil {
		t.Fatal(err)
	}
	ConfigureKiloMcp("context-mode", []string{"tokless", "run-mcp"})
	RemoveKiloMcp("context-mode")
	CleanupKiloProject()
	raw, ok := util.ReadFileSafe(config)
	if !ok || !strings.Contains(raw, "user") {
		t.Fatalf("foreign Kilo config was removed: %s", raw)
	}
}

func TestCleanupKiloProjectPreservesPreexistingSchemaOnlyConfig(t *testing.T) {
	root := withKiloProject(t)
	config := filepath.Join(root, ".kilo", "kilo.jsonc")
	if err := util.WriteFile(config, `{"$schema":"https://app.kilo.ai/config.json"}`); err != nil {
		t.Fatal(err)
	}
	ConfigureKiloMcp("context-mode", []string{"tokless", "run-mcp"})
	RemoveKiloMcp("context-mode")
	CleanupKiloProject()
	if _, err := os.Stat(config); err != nil {
		t.Fatalf("pre-existing schema-only config removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".kilo", kiloCreatedMarker)); !os.IsNotExist(err) {
		t.Fatalf("unexpected ownership marker: %v", err)
	}
}

func TestCleanupKiloProjectPreservesPackageArtifacts(t *testing.T) {
	root := withKiloProject(t)
	ConfigureKiloMcp("context-mode", []string{"tokless", "run-mcp"})
	if err := util.WriteFile(filepath.Join(root, ".kilo", "package.json"), "{}\n"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(root, ".kilo", "package-lock.json"), "{}\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".kilo", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	RemoveKiloMcp("context-mode")
	CleanupKiloProject()
	for _, name := range []string{"package.json", "package-lock.json", "node_modules"} {
		if _, err := os.Stat(filepath.Join(root, ".kilo", name)); err != nil {
			t.Fatalf("package artifact %s removed: %v", name, err)
		}
	}
}
