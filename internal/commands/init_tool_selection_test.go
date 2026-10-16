package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func withSelHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	util.SetHomeOverride(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Cleanup(func() { util.SetHomeOverride("") })
	return dir
}

// seedAgentInstalled makes an agent detectable in the test home.
func seedAgentInstalled(t *testing.T, agentID string) {
	t.Helper()
	agent := core.GetAgent(agentID)
	if agent == nil {
		t.Fatalf("agent %q not registered", agentID)
	}
	if agent.ConfigDir != nil {
		if err := os.MkdirAll(agent.ConfigDir(), 0o755); err != nil {
			t.Fatalf("seed %s config dir: %v", agentID, err)
		}
	}
	if agentID == "cline" {
		binDir := t.TempDir()
		bin := filepath.Join(binDir, agent.CLIBin)
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("seed %s binary: %v", agentID, err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
}

// withUnwireStub swaps one tool/agent unwire function for the duration of a test.
func withUnwireStub(t *testing.T, toolID, agentID string, fn core.AgentFn) {
	t.Helper()
	tool := core.GetTool(toolID)
	if tool == nil {
		t.Fatalf("tool %q not registered", toolID)
	}
	saved, existed := tool.UnwireFor[agentID]
	tool.UnwireFor[agentID] = fn
	t.Cleanup(func() {
		if existed {
			tool.UnwireFor[agentID] = saved
		} else {
			delete(tool.UnwireFor, agentID)
		}
	})
}

func seedInitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TOKLESS_TEST", "1")
	t.Setenv("TOKLESS_INSTALLER_RUN", "1")
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func wiredFor(t *testing.T, toolID, agentID string) bool {
	t.Helper()
	verify, ok := core.GetTool(toolID).VerifyFor[agentID]
	if !ok {
		t.Fatalf("tool %q has no VerifyFor %q", toolID, agentID)
	}
	v := verify()
	return v != nil && *v
}

func TestResolveToolSelectionFirstRunNonInteractiveFull(t *testing.T) {
	withSelHome(t)
	all := core.ListTools()
	if len(all) == 0 {
		t.Fatal("no tools registered")
	}
	tools, mode := resolveToolSelection(InitOptions{}, all)
	if mode != "full" || len(tools) != len(all) {
		t.Fatalf("first non-interactive: mode=%s len=%d", mode, len(tools))
	}
	saved := saveSelectionIfNeeded(mode, tools, false)
	if saved != "full" {
		t.Fatalf("expected full persistence, got %q", saved)
	}
	if util.LoadSelection() == nil || util.LoadSelection().Mode != "full" {
		t.Fatal("expected persisted full mode")
	}
}

func TestResolveToolSelectionExplicitPersistsSelected(t *testing.T) {
	withSelHome(t)
	all := core.ListTools()
	tools, mode := resolveToolSelection(InitOptions{Tools: []string{"rtk", "codegraph"}}, all)
	if mode != "selected" || len(tools) != 2 {
		t.Fatalf("explicit tools: mode=%s len=%d", mode, len(tools))
	}
	saved := saveSelectionIfNeeded(mode, tools, false)
	if saved != "selected" {
		t.Fatalf("expected selected persistence, got %q", saved)
	}
	if util.LoadSelection() == nil || len(util.LoadSelection().Tools) != 2 {
		t.Fatal("expected persisted selected mode")
	}
	tools2, mode2 := resolveToolSelection(InitOptions{}, all)
	if mode2 != "selected" || len(tools2) != 2 {
		t.Fatalf("re-run should reuse: mode=%s len=%d", mode2, len(tools2))
	}
}

func TestResolveToolSelectionDryRunDoesNotPersist(t *testing.T) {
	withSelHome(t)
	all := core.ListTools()
	tools, mode := resolveToolSelection(InitOptions{Tools: []string{"rtk"}, DryRun: true}, all)
	_ = saveSelectionIfNeeded(mode, tools, true)
	if util.LoadSelection() != nil {
		t.Fatal("dry-run must not persist")
	}
}

// TestRunInitPersistsSelectionAndReusesIt checks persistence after success.
func TestRunInitPersistsSelectionAndReusesIt(t *testing.T) {
	seedInitEnv(t)
	withSelHome(t)
	seedAgentInstalled(t, "claude")

	if code := RunInit(InitOptions{Agents: []string{"claude"}, Tools: []string{"principles", "context-mode"}}); code != 0 {
		t.Fatalf("RunInit returned %d", code)
	}
	saved := util.LoadSelection()
	if saved == nil || saved.Mode != "selected" || len(saved.Tools) != 2 {
		t.Fatalf("selection not persisted after successful init: %+v", saved)
	}
	if !wiredFor(t, "context-mode", "claude") {
		t.Fatal("selected context-mode not wired")
	}
	body := readFileString(t, util.ClaudeCodePaths().Instructions)
	if !strings.Contains(body, "## Principles") {
		t.Fatal("selected principles section missing from Claude instructions")
	}

	// The saved selection must be reused.
	if code := RunInit(InitOptions{Agents: []string{"claude"}}); code != 0 {
		t.Fatalf("re-run RunInit returned %d", code)
	}
	if !wiredFor(t, "context-mode", "claude") {
		t.Fatal("saved-selection re-run dropped context-mode wiring")
	}
	body = readFileString(t, util.ClaudeCodePaths().Instructions)
	if !strings.Contains(body, "## Principles") {
		t.Fatal("saved-selection re-run dropped principles instructions")
	}
	if strings.Contains(body, "## Response Style (caveman)") {
		t.Fatal("re-run reset selection to full and wired caveman")
	}
}

// TestExcludedToolUnwireRemovesManagedConfigOnly checks managed-entry removal.
func TestExcludedToolUnwireRemovesManagedConfigOnly(t *testing.T) {
	seedInitEnv(t)
	withSelHome(t)
	seedAgentInstalled(t, "opencode")

	if changed, _ := agents.ConfigureOpenCodeMcp("context-mode"); !changed {
		t.Fatal("seed context-mode MCP entry")
	}
	configPath := util.OpenCodePathsResolved().Config
	raw, ok := util.ReadFileSafe(configPath)
	if !ok {
		t.Fatal("read seeded config")
	}
	cfg := util.TryParseJsonc(raw)
	mcpVal, _ := cfg.Get("mcp")
	mcp, _ := mcpVal.(*util.OrderedMap)
	mcp.Set("foreign", map[string]any{"type": "local"})
	if err := util.WriteFile(configPath, util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	// Persist a selection that excludes context-mode; the next init must unwind it.
	if err := util.SaveSelection("selected", []string{"principles"}); err != nil {
		t.Fatal(err)
	}
	if code := RunInit(InitOptions{Agents: []string{"opencode"}}); code != 0 {
		t.Fatalf("RunInit returned %d", code)
	}

	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfgMap map[string]json.RawMessage
	if err := json.Unmarshal(cfgBytes, &cfgMap); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfgMap["mcp"]; !ok {
		t.Fatal("mcp section removed entirely")
	}
	var mcpMap map[string]json.RawMessage
	if err := json.Unmarshal(cfgMap["mcp"], &mcpMap); err != nil {
		t.Fatal(err)
	}
	if _, ok := mcpMap["foreign"]; !ok {
		t.Fatal("foreign MCP entry should survive excluded tool unwire")
	}
	if _, ok := mcpMap["context-mode"]; ok {
		t.Fatal("context-mode MCP entry should be removed after deselect")
	}
}

// TestRunInitReportsFailureWhenExcludedUnwireFails checks failure reporting.
func TestRunInitReportsFailureWhenExcludedUnwireFails(t *testing.T) {
	seedInitEnv(t)
	withSelHome(t)
	seedAgentInstalled(t, "claude")

	if code := RunInit(InitOptions{Agents: []string{"claude"}, Tools: []string{"principles", "context-mode"}}); code != 0 {
		t.Fatalf("seed RunInit returned %d", code)
	}
	if err := util.SaveSelection("selected", []string{"principles"}); err != nil {
		t.Fatal(err)
	}
	withUnwireStub(t, "context-mode", "claude", func(core.RunOpts) (bool, error) {
		return false, errors.New("stubbed removal failure")
	})

	if code := RunInit(InitOptions{Agents: []string{"claude"}}); code != 1 {
		t.Fatalf("expected exit 1 when excluded unwire fails, got %d", code)
	}
	if !wiredFor(t, "context-mode", "claude") {
		t.Fatal("failed unwire must leave existing wiring in place")
	}
}

// TestRunInitDryRunDoesNotPersistOrUnwire checks dry-run side effects.
func TestRunInitDryRunDoesNotPersistOrUnwire(t *testing.T) {
	seedInitEnv(t)
	withSelHome(t)
	seedAgentInstalled(t, "claude")

	if code := RunInit(InitOptions{Agents: []string{"claude"}, Tools: []string{"principles", "context-mode"}}); code != 0 {
		t.Fatalf("seed RunInit returned %d", code)
	}
	if err := util.SaveSelection("selected", []string{"principles"}); err != nil {
		t.Fatal(err)
	}
	if code := RunInit(InitOptions{Agents: []string{"claude"}, DryRun: true}); code != 0 {
		t.Fatalf("dry-run RunInit returned %d", code)
	}
	saved := util.LoadSelection()
	if saved == nil || len(saved.Tools) != 1 || saved.Tools[0] != "principles" {
		t.Fatalf("dry-run rewrote persisted selection: %+v", saved)
	}
	if !wiredFor(t, "context-mode", "claude") {
		t.Fatal("dry-run must not unwire excluded wiring")
	}

	// Fresh home: a dry-run with explicit tools must not create a selection file.
	withSelHome(t)
	seedAgentInstalled(t, "claude")
	if code := RunInit(InitOptions{Agents: []string{"claude"}, Tools: []string{"context-mode"}, DryRun: true}); code != 0 {
		t.Fatalf("fresh dry-run RunInit returned %d", code)
	}
	if util.LoadSelection() != nil {
		t.Fatal("dry-run must not create a selection file")
	}
}

// TestRunInitSkipsRequestedButNotInstalledAgents checks handling of absent agents.
func TestRunInitEmptySelectionStillUnwiresManagedConfig(t *testing.T) {
	seedInitEnv(t)
	withSelHome(t)
	seedAgentInstalled(t, "claude")
	if code := RunInit(InitOptions{Agents: []string{"claude"}, Tools: []string{"context-mode"}}); code != 0 {
		t.Fatalf("seed RunInit returned %d", code)
	}
	configPath := util.ClaudeCodePaths().GlobalJSON
	if err := util.SaveSelection("selected", []string{}); err != nil {
		t.Fatal(err)
	}
	if code := RunInit(InitOptions{Agents: []string{"claude"}, Tools: []string{}}); code != 0 {
		t.Fatalf("empty-selection RunInit returned %d", code)
	}
	if wiredFor(t, "context-mode", "claude") {
		t.Fatal("empty selection left context-mode configured")
	}
	if raw, ok := util.ReadFileSafe(configPath); ok && strings.Contains(raw, "context-mode") {
		t.Fatalf("managed MCP config remains after empty-selection init: %s", raw)
	}
}

func TestRunInitSkipsRequestedButNotInstalledAgents(t *testing.T) {
	seedInitEnv(t)
	withSelHome(t)
	// Restrict PATH so ambient agent binaries cannot make the agent look installed.
	t.Setenv("PATH", t.TempDir())

	if code := RunInit(InitOptions{Agents: []string{"opencode"}, Tools: []string{"principles"}}); code != 0 {
		t.Fatalf("RunInit returned %d", code)
	}
	if wiredFor(t, "principles", "opencode") {
		t.Fatal("not-installed agent must not be wired")
	}
	if util.Exists(util.OpenCodePathsResolved().Instructions) {
		t.Fatal("not-installed agent must not have instructions written")
	}
}

func TestRunExcludedToolsUnwireReportsFailure(t *testing.T) {
	tool := &core.ToolManifest{
		ID: "managed", Label: "Managed",
		VerifyFor: map[string]core.VerifyFn{"agent": func() *bool { return core.BoolPtr(true) }},
		UnwireFor: map[string]core.AgentFn{"agent": func(core.RunOpts) (bool, error) { return false, errors.New("boom") }},
	}
	failed := runExcludedToolsUnwire("agent", []*core.ToolManifest{tool}, nil, InitOptions{})
	if len(failed) != 1 || failed[0] != "Managed" {
		t.Fatalf("unwire failure not surfaced: %v", failed)
	}
}

// TestRunExcludedToolsUnwireFailsWhenPresenceRemains checks presence-confirmed failure.
func TestRunExcludedToolsUnwireFailsWhenPresenceRemains(t *testing.T) {
	tool := &core.ToolManifest{
		ID: "managed", Label: "Managed",
		WiredAnyFor: map[string]core.VerifyFn{"agent": func() *bool { return core.BoolPtr(true) }},
		VerifyFor:   map[string]core.VerifyFn{"agent": func() *bool { return core.BoolPtr(true) }},
		UnwireFor:   map[string]core.AgentFn{"agent": func(core.RunOpts) (bool, error) { return false, nil }},
	}
	failed := runExcludedToolsUnwire("agent", []*core.ToolManifest{tool}, nil, InitOptions{})
	if len(failed) != 1 || failed[0] != "Managed" {
		t.Fatalf("presence-confirmed unwire failure not surfaced: %v", failed)
	}
}

// TestRunExcludedToolsUnwireToleratesNotConfiguredNoOp checks absent-config no-op handling.
func TestRunExcludedToolsUnwireToleratesNotConfiguredNoOp(t *testing.T) {
	tool := &core.ToolManifest{
		ID: "readiness", Label: "Readiness",
		VerifyFor: map[string]core.VerifyFn{"agent": func() *bool { return core.BoolPtr(true) }},
		UnwireFor: map[string]core.AgentFn{"agent": func(core.RunOpts) (bool, error) { return false, nil }},
	}
	failed := runExcludedToolsUnwire("agent", []*core.ToolManifest{tool}, nil, InitOptions{})
	if len(failed) != 0 {
		t.Fatalf("no-op unwire with no presence signal must not fail: %v", failed)
	}
}

func TestCodegraphAutoIndexHookGatedBySelectionExcluded(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	dir := withSelHome(t)
	proj := filepath.Join(dir, "proj")
	os.MkdirAll(filepath.Join(proj, ".git"), 0o755)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	// Exclude codegraph
	util.SaveSelection("selected", []string{"rtk"})
	if code := RunCodegraphIndexHook(); code != 0 {
		t.Fatalf("excluded hook should return 0, got %d", code)
	}
	if util.Exists(filepath.Join(proj, ".codegraph")) {
		t.Fatal("codegraph index should not be built while excluded")
	}

	// Include codegraph
	util.SaveSelection("full", nil)
	if code := RunCodegraphIndexHook(); code != 0 {
		t.Fatalf("included hook should run, got %d", code)
	}
	if !util.Exists(filepath.Join(proj, ".codegraph")) {
		t.Fatal("codegraph index should exist while selected")
	}
}
