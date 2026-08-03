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
	root, err = os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return root
}

func withKiloGlobal(t *testing.T) string {
	t.Helper()
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "global"))
	return util.KiloPathsResolved().Config
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

func TestKiloDetectsOfficialInstallerBinaryWithoutPATH(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("PATH", "")
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))

	bin := filepath.Join(home, ".kilo", "bin", "kilo")
	if util.IsWin {
		bin += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	d := kilo.Detect()
	if !d.Installed || d.Source != "cli" {
		t.Fatalf("Kilo official installer binary should detect as cli, got %+v", d)
	}
}

func TestKiloGlobalConfigPreservesMCPAndInstructions(t *testing.T) {
	root := withKiloProject(t)
	config := withKiloGlobal(t)
	projectConfig := filepath.Join(root, ".kilo", "kilo.jsonc")
	if err := util.WriteFile(config, `{"provider":"user","instructions":["user.md"],"mcp":{"foreign":{"type":"local","command":["foreign"],"enabled":true}}}`); err != nil {
		t.Fatal(err)
	}
	changed, file, err := ConfigureKiloMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || file != config {
		t.Fatalf("ConfigureKiloMcpSafe = %v, %q", changed, file)
	}
	if _, err := os.Stat(projectConfig); err == nil {
		t.Fatal("project Kilo config created")
	}
	raw, ok := util.ReadFileSafe(config)
	if !ok || !strings.Contains(raw, `"foreign"`) || !strings.Contains(raw, `"user.md"`) {
		t.Fatalf("user config not preserved: %s", raw)
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); changed || err != nil {
		t.Fatal("second Kilo configure was not idempotent")
	}
	if !KiloMcpConfigured("context-mode") || !KiloMcpMatches("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}) {
		t.Fatal("exact Kilo MCP verification failed")
	}
}

func TestKiloGlobalWireAndRemovalPreservesUserConfig(t *testing.T) {
	config := withKiloGlobal(t)
	if err := util.WriteFile(config, `{"instructions":["user.md"],"mcp":{"foreign":{"type":"local","command":["foreign"],"enabled":true}}}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConfigureKiloMcpSafe("codegraph", []string{"tokless", "run-mcp", "--agent", "kilo", "codegraph", "serve", "--mcp"}); err != nil {
		t.Fatal(err)
	}
	if !RemoveKiloMcp("codegraph") || KiloMcpConfigured("codegraph") {
		t.Fatal("Kilo MCP removal failed")
	}
	raw, _ := util.ReadFileSafe(config)
	if !strings.Contains(raw, "foreign") || !strings.Contains(raw, "user.md") {
		t.Fatalf("removal damaged user config: %s", raw)
	}
}

func TestKiloProjectFileLegacyResolutionOnly(t *testing.T) {
	root := withKiloProject(t)
	if got := KiloProjectFile(); got != filepath.Join(root, ".kilo") {
		t.Fatalf("KiloProjectFile() = %s", got)
	}
	if got := KiloProjectFile("plugin", "tokless-rtk.ts"); got != filepath.Join(root, ".kilo", "plugin", "tokless-rtk.ts") {
		t.Fatalf("KiloProjectFile nested = %s", got)
	}
	root2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(root2, ".git"), []byte("gitdir: /tmp/worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if got := KiloProjectFile("kilo.jsonc"); got != filepath.Join(root2, ".kilo", "kilo.jsonc") {
		t.Fatalf("linked worktree KiloProjectFile = %s", got)
	}
}

func TestKiloStateFirstRecoveryWritesMissingConfig(t *testing.T) {
	withKiloProject(t)
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if err := kiloStateSet("context-mode", command); err != nil {
		t.Fatal(err)
	}
	if util.Exists(config) {
		t.Fatal("config should not exist yet")
	}
	changed, _, err := ConfigureKiloMcpSafe("context-mode", command)
	if err != nil || !changed || !KiloMcpMatches("context-mode", command) {
		t.Fatalf("state-first recovery = %v, %v", changed, err)
	}
}

func TestKiloExistingOwnedConfigIsNotRewritten(t *testing.T) {
	withKiloProject(t)
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if err := util.WriteFile(config, `{"provider":"user","mcp":{"context-mode":{"type":"local","command":["tokless","run-mcp","--context-mode","context-mode"],"enabled":true},"foreign":{"type":"local","command":["foreign"],"enabled":true}}}`); err != nil {
		t.Fatal(err)
	}
	if err := kiloStateSet("context-mode", command); err != nil {
		t.Fatal(err)
	}
	changed, _, err := ConfigureKiloMcpSafe("context-mode", command)
	if err != nil || changed {
		t.Fatalf("owned config was unexpectedly rewritten: %v, %v", changed, err)
	}
	raw, ok := util.ReadFileSafe(config)
	if !ok || !strings.Contains(raw, `"provider":"user"`) || !strings.Contains(raw, `"foreign"`) || !strings.Contains(raw, `"enabled":true`) {
		t.Fatalf("existing global content not preserved: %s", raw)
	}
}

func TestKiloWriteConflictPreservesEditAndRetryRewires(t *testing.T) {
	withKiloProject(t)
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if err := util.WriteFile(config, `{"provider":"user"}`); err != nil {
		t.Fatal(err)
	}
	defer func() { kiloBeforeReplaceHook = nil }()
	kiloBeforeReplaceHook = func(path string) {
		if path == config {
			_ = util.WriteFile(path, `{"provider":"concurrent"}`)
			kiloBeforeReplaceHook = nil
		}
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", command); err == nil || changed {
		t.Fatal("global write conflict not reported")
	}
	if raw, _ := util.ReadFileSafe(config); raw != `{"provider":"concurrent"}` {
		t.Fatalf("concurrent global edit lost: %q", raw)
	}
	if _, exists, _ := kiloStateRead(); exists {
		t.Fatal("ownership state left behind after failed wire")
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", command); err != nil || !changed {
		t.Fatalf("retry after conflict failed to wire: %v, %v", changed, err)
	}
	raw, _ := util.ReadFileSafe(config)
	if !strings.Contains(raw, "concurrent") || !KiloMcpConfigured("context-mode") {
		t.Fatalf("retry lost concurrent edit or entry: %s", raw)
	}
}

func TestKiloConflictEntryIsNotAdoptedOrRemoved(t *testing.T) {
	withKiloProject(t)
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if err := util.WriteFile(config, `{"provider":"user"}`); err != nil {
		t.Fatal(err)
	}
	defer func() { kiloBeforeReplaceHook = nil }()
	kiloBeforeReplaceHook = func(path string) {
		if path == config {
			_ = util.WriteFile(path, `{"mcp":{"context-mode":{"type":"local","command":["tokless","run-mcp","--context-mode","context-mode"],"enabled":true}}}`)
			kiloBeforeReplaceHook = nil
		}
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", command); err == nil || changed {
		t.Fatal("config conflict not reported")
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", command); err == nil || changed {
		t.Fatal("retry adopted user MCP entry")
	}
	if RemoveKiloMcp("context-mode") {
		t.Fatal("unwire removed user MCP entry")
	}
	raw, _ := util.ReadFileSafe(config)
	if !strings.Contains(raw, `"context-mode"`) {
		t.Fatal("user MCP entry was deleted")
	}
}

func TestKiloUnwireWriteConflictPreservesEdit(t *testing.T) {
	withKiloProject(t)
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if err := util.WriteFile(config, `{"provider":"user","mcp":{"context-mode":{"type":"local","command":["tokless","run-mcp","--context-mode","context-mode"],"enabled":true}}}`); err != nil {
		t.Fatal(err)
	}
	if err := kiloStateSet("context-mode", command); err != nil {
		t.Fatal(err)
	}
	defer func() { kiloBeforeReplaceHook = nil }()
	kiloBeforeReplaceHook = func(path string) {
		if path == config {
			_ = util.WriteFile(path, `{"provider":"concurrent"}`)
			kiloBeforeReplaceHook = nil
		}
	}
	if RemoveKiloMcp("context-mode") {
		t.Fatal("unwire conflict reported success")
	}
	if raw, _ := util.ReadFileSafe(config); raw != `{"provider":"concurrent"}` {
		t.Fatalf("concurrent unwire edit lost: %q", raw)
	}
	state, exists, _ := kiloStateRead()
	if !exists {
		t.Fatal("ownership state removed after config conflict")
	}
	if _, ok := state["context-mode"]; !ok {
		t.Fatal("ownership state record removed after config conflict")
	}
	if !RemoveKiloMcp("context-mode") {
		t.Fatal("clean unwire retry failed")
	}
}

func TestKiloExistingEntryWithoutStateIsNotAdopted(t *testing.T) {
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if err := util.WriteFile(config, `{"mcp":{"context-mode":{"type":"local","command":["tokless","run-mcp","--context-mode","context-mode"],"enabled":true}}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", command); err == nil || changed {
		t.Fatal("existing MCP entry adopted without ownership state")
	}
}

func TestKiloStateWithDifferentCommandRefuses(t *testing.T) {
	config := withKiloGlobal(t)
	if err := util.WriteFile(config, `{"provider":"user"}`); err != nil {
		t.Fatal(err)
	}
	if err := kiloStateSet("context-mode", []string{"tokless", "run-mcp", "--context-mode", "other"}); err != nil {
		t.Fatal(err)
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); err == nil || changed {
		t.Fatal("ownership state with different command was accepted")
	}
}

func TestKiloRefusesJSONCCommentConfig(t *testing.T) {
	config := withKiloGlobal(t)
	if err := util.WriteFile(config, "// user comment\n{\"provider\":\"user\"}\n"); err != nil {
		t.Fatal(err)
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); err == nil || changed {
		t.Fatal("JSONC-comment config was rewritten")
	}
	if RemoveKiloMcp("context-mode") {
		t.Fatal("JSONC-comment config was modified on unwire")
	}
	raw, _ := util.ReadFileSafe(config)
	if raw != "// user comment\n{\"provider\":\"user\"}\n" {
		t.Fatalf("JSONC config changed: %q", raw)
	}
}

func TestKiloRefusesUnparseableConfig(t *testing.T) {
	config := withKiloGlobal(t)
	if err := util.WriteFile(config, "{malformed"); err != nil {
		t.Fatal(err)
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "context-mode"}); err == nil || changed {
		t.Fatal("malformed config was overwritten")
	}
}

func TestKiloMcpExtrasAreUserModifiedAndPreserved(t *testing.T) {
	withKiloProject(t)
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if _, _, err := ConfigureKiloMcpSafe("context-mode", command); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(config, `{"mcp":{"context-mode":{"type":"local","command":["tokless","run-mcp","--context-mode","context-mode"],"enabled":true,"timeout":30,"env":{"USER":"keep"}}}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _, err := ConfigureKiloMcpSafe("context-mode", command); err == nil || changed {
		t.Fatal("wire adopted user-modified MCP entry")
	}
	if RemoveKiloMcp("context-mode") {
		t.Fatal("unwire removed user-modified MCP entry")
	}
	raw, _ := util.ReadFileSafe(config)
	if !strings.Contains(raw, `"timeout":30`) || !strings.Contains(raw, `"USER":"keep"`) {
		t.Fatalf("user MCP fields were not preserved: %s", raw)
	}
}

func TestKiloStrictWindowsSpawnCommands(t *testing.T) {
	for toolID, command := range map[string][]string{
		"context-mode": {`C:\\Users\\me\\tokless.exe`, "run-mcp", "--context-mode", "cmd", "/c", `C:\\tools\\context-mode.cmd`},
		"codegraph":    {`C:\\Users\\me\\tokless.exe`, "run-mcp", "--agent", "kilo", "cmd", "/c", `C:\\tools\\codegraph.bat`, "serve", "--mcp"},
	} {
		if !kiloExpectedCommand(toolID, command) {
			t.Fatalf("valid Windows %s command rejected: %#v", toolID, command)
		}
	}
	if kiloExpectedCommand("context-mode", []string{"tokless", "run-mcp", "--context-mode", "cmd", "/c", `C:\\tools\\codegraph.cmd`}) {
		t.Fatal("context accepted codegraph shim")
	}
}

func TestKiloUnwireRemovesStaleStateOnly(t *testing.T) {
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if err := kiloStateSet("context-mode", command); err != nil {
		t.Fatal(err)
	}
	if !RemoveKiloMcp("context-mode") {
		t.Fatal("valid stale Kilo owner state not removed")
	}
	if _, ok := util.ReadFileSafe(kiloStatePath()); ok {
		t.Fatal("stale Kilo owner state remains")
	}
	if util.Exists(config) {
		t.Fatal("config was created during stale-state cleanup")
	}
}

func TestKiloUnwireRemovesStateWhenConfigHasNoMCP(t *testing.T) {
	config := withKiloGlobal(t)
	command := []string{"tokless", "run-mcp", "--context-mode", "context-mode"}
	if err := util.WriteFile(config, `{"provider":"user"}`); err != nil {
		t.Fatal(err)
	}
	if err := kiloStateSet("context-mode", command); err != nil {
		t.Fatal(err)
	}
	before, _ := util.ReadFileSafe(config)
	if !RemoveKiloMcp("context-mode") {
		t.Fatal("stale state was not removed")
	}
	after, _ := util.ReadFileSafe(config)
	if before != after {
		t.Fatalf("config changed during stale-state recovery: %q != %q", before, after)
	}
}
