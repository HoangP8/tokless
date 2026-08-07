package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// withClineGlobal pins CLINE_DIR to a temp dir and clears the other overrides.
func withClineGlobal(t *testing.T) util.ClinePaths {
	t.Helper()
	t.Setenv("CLINE_DIR", filepath.Join(t.TempDir(), "cline"))
	t.Setenv("CLINE_DATA_DIR", "")
	t.Setenv("CLINE_MCP_SETTINGS_PATH", "")
	return util.ClinePathsResolved()
}

func TestClinePathsResolvedDefaults(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("CLINE_DIR", "")
	t.Setenv("CLINE_DATA_DIR", "")
	t.Setenv("CLINE_MCP_SETTINGS_PATH", "")
	p := util.ClinePathsResolved()
	if p.Dir != filepath.Join(home, ".cline") {
		t.Fatalf("Dir = %q", p.Dir)
	}
	if p.DataDir != filepath.Join(p.Dir, "data") {
		t.Fatalf("DataDir = %q", p.DataDir)
	}
	if p.McpConfig != filepath.Join(p.Dir, "data", "settings", "cline_mcp_settings.json") {
		t.Fatalf("McpConfig = %q", p.McpConfig)
	}
	if p.HooksDir != filepath.Join(p.Dir, "hooks") || p.RulesDir != filepath.Join(p.Dir, "rules") {
		t.Fatalf("HooksDir/RulesDir = %q / %q", p.HooksDir, p.RulesDir)
	}
	if p.Instructions != filepath.Join(p.Dir, "rules", "AGENTS.md") {
		t.Fatalf("Instructions = %q", p.Instructions)
	}
	if ClineInstructionsPath() != p.Instructions {
		t.Fatalf("ClineInstructionsPath = %q", ClineInstructionsPath())
	}
}

func TestClinePathsResolvedEnvOverrides(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("CLINE_DIR", filepath.Join(home, "custom-cline"))
	t.Setenv("CLINE_DATA_DIR", filepath.Join(home, "custom-data"))
	t.Setenv("CLINE_MCP_SETTINGS_PATH", filepath.Join(home, "custom.json"))
	p := util.ClinePathsResolved()
	if p.Dir != filepath.Join(home, "custom-cline") || p.DataDir != filepath.Join(home, "custom-data") ||
		p.McpConfig != filepath.Join(home, "custom.json") {
		t.Fatalf("env overrides ignored: %+v", p)
	}
	if p.HooksDir != filepath.Join(p.Dir, "hooks") || p.Instructions != filepath.Join(p.Dir, "rules", "AGENTS.md") {
		t.Fatalf("derived paths must follow CLINE_DIR: %+v", p)
	}
}

func TestClineIgnoresSharedConfigWithoutCLI(t *testing.T) {
	p := withClineGlobal(t)
	t.Setenv("PATH", t.TempDir())
	if err := util.EnsureDir(filepath.Dir(p.McpConfig)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(p.McpConfig, "{}"); err != nil {
		t.Fatal(err)
	}
	if got := cline.Detect(); got.Installed || got.Source != "" {
		t.Fatalf("Cline detection = %+v, want not installed (CLI-only)", got)
	}
}

func TestClineDoesNotDetectEmptyDir(t *testing.T) {
	withClineGlobal(t)
	t.Setenv("PATH", t.TempDir())
	if got := cline.Detect(); got.Installed || got.Source != "" {
		t.Fatalf("Cline detection = %+v, want not installed", got)
	}
}

func TestClineDetectsCLI(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	binName := "cline"
	if util.IsWin {
		binName = "cline.EXE"
	}
	bin := filepath.Join(binDir, binName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cline.Detect(); !got.Installed || got.Source != "cli" {
		t.Fatalf("Cline detection = %+v, want cli", got)
	}
}

func TestConfigureClineMcpSafeCreatesEntryAndState(t *testing.T) {
	p := withClineGlobal(t)
	cmd := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	changed, file, err := ConfigureClineMcpSafe("context-mode", cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || file != p.McpConfig {
		t.Fatalf("ConfigureClineMcpSafe = %v, %q", changed, file)
	}
	raw, ok := util.ReadFileSafe(p.McpConfig)
	if !ok || !strings.Contains(raw, `"mcpServers"`) || !strings.Contains(raw, `"context-mode"`) {
		t.Fatalf("mcp settings missing entry: %s", raw)
	}
	if _, err := os.Stat(clineStatePath()); err != nil {
		t.Fatal("ownership state not written")
	}
	if !ClineMcpConfigured("context-mode") || !ClineMcpMatches("context-mode", cmd) {
		t.Fatal("exact Cline MCP verification failed")
	}

	// Idempotent re-run.
	if changed, _, err := ConfigureClineMcpSafe("context-mode", cmd); changed || err != nil {
		t.Fatal("second Cline configure was not idempotent")
	}
}

func TestConfigureClineMcpSafeRefusesForeignEntry(t *testing.T) {
	p := withClineGlobal(t)
	if err := util.EnsureDir(filepath.Dir(p.McpConfig)); err != nil {
		t.Fatal(err)
	}
	foreign := `{"mcpServers":{"context-mode":{"command":"other","args":[],"env":{},"disabled":false}}}`
	if err := util.WriteFile(p.McpConfig, foreign); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConfigureClineMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); err == nil {
		t.Fatal("foreign Cline MCP entry adopted without ownership state")
	}
	raw, _ := util.ReadFileSafe(p.McpConfig)
	if !strings.Contains(raw, `"other"`) {
		t.Fatalf("foreign config was modified: %s", raw)
	}
}

func TestConfigureClineMcpSafePreservesForeignServers(t *testing.T) {
	p := withClineGlobal(t)
	if err := util.EnsureDir(filepath.Dir(p.McpConfig)); err != nil {
		t.Fatal(err)
	}
	keep := `{"mcpServers":{"filesystem":{"command":"npx","args":["-y","server"],"env":{},"disabled":false}}}`
	if err := util.WriteFile(p.McpConfig, keep); err != nil {
		t.Fatal(err)
	}
	cmd := []string{"tokless", "run-mcp", "--agent", "cline", "codegraph", "serve", "--mcp"}
	if _, _, err := ConfigureClineMcpSafe("codegraph", cmd); err != nil {
		t.Fatal(err)
	}
	raw, _ := util.ReadFileSafe(p.McpConfig)
	if !strings.Contains(raw, `"filesystem"`) {
		t.Fatalf("foreign server lost: %s", raw)
	}
}

func TestRemoveClineMcpRemovesEntryAndCleansEmptyServers(t *testing.T) {
	p := withClineGlobal(t)
	cmd := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if _, _, err := ConfigureClineMcpSafe("context-mode", cmd); err != nil {
		t.Fatal(err)
	}
	if !RemoveClineMcp("context-mode") {
		t.Fatal("owned Cline MCP entry not removed")
	}
	raw, _ := util.ReadFileSafe(p.McpConfig)
	if strings.Contains(raw, "context-mode") {
		t.Fatalf("entry left behind: %s", raw)
	}
	if strings.Contains(raw, "mcpServers") {
		t.Fatalf("empty mcpServers key should be deleted: %s", raw)
	}
	if _, err := os.Stat(clineStatePath()); !os.IsNotExist(err) {
		t.Fatal("ownership state left behind")
	}
	if RemoveClineMcp("context-mode") {
		t.Fatal("remove on missing entry should report false")
	}
}

func TestClineMcpMatchesNegative(t *testing.T) {
	p := withClineGlobal(t)
	cmd := []string{"tokless", "run-mcp", "--agent", "cline", "codegraph", "serve", "--mcp"}
	if _, _, err := ConfigureClineMcpSafe("codegraph", cmd); err != nil {
		t.Fatal(err)
	}
	if !ClineMcpMatches("codegraph", cmd) {
		t.Fatal("positive match failed")
	}
	if ClineMcpMatches("codegraph", []string{"tokless", "run-mcp", "--agent", "cline", "codegraph", "serve2", "--mcp"}) {
		t.Fatal("different args should not match")
	}
	if ClineMcpMatches("context-mode", cmd) {
		t.Fatal("missing entry should not match")
	}

	// A disabled entry never matches.
	disabled := `{"mcpServers":{"codegraph":{"command":"tokless","args":["run-mcp","--agent","cline","codegraph","serve","--mcp"],"env":{},"disabled":true}}}`
	if err := util.WriteFile(p.McpConfig, disabled); err != nil {
		t.Fatal(err)
	}
	if ClineMcpMatches("codegraph", cmd) {
		t.Fatal("disabled entry should not match")
	}
}

func TestConfigureClineMcpSafeRejectsJSONCComments(t *testing.T) {
	p := withClineGlobal(t)
	if err := util.EnsureDir(filepath.Dir(p.McpConfig)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(p.McpConfig, "{\n  // a comment\n  \"mcpServers\": {}\n}"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConfigureClineMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); err == nil {
		t.Fatal("JSONC comments accepted")
	}
}
