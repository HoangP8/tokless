package tools

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

func claudeRefusalHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})
	return util.ClaudeCodePaths().GlobalJSON
}

// claudeEntryJSON renders a stdio mcpServers entry; extra is raw JSON members (",\"env\":{}").
func claudeEntryJSON(command string, args []string, extra string) string {
	a, _ := json.Marshal(args)
	c, _ := json.Marshal(command)
	return `{"type":"stdio","command":` + string(c) + `,"args":` + string(a) + extra + `}`
}

func seedClaude(t *testing.T, path, toolID, entry string) string {
	t.Helper()
	cfg := `{"mcpServers":{"` + toolID + `":` + entry + `}}`
	if err := util.WriteFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func claudeEntry(t *testing.T, path, toolID string) *util.OrderedMap {
	t.Helper()
	raw, _ := util.ReadFileSafe(path)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("unparseable %s", raw)
	}
	servers, _ := cfg.Get("mcpServers")
	sm, _ := servers.(*util.OrderedMap)
	if sm == nil {
		t.Fatalf("no mcpServers: %s", raw)
	}
	v, _ := sm.Get(toolID)
	em, _ := v.(*util.OrderedMap)
	if em == nil {
		t.Fatalf("no entry %s: %s", toolID, raw)
	}
	return em
}

func captureWire(t *testing.T, fn func() bool) (bool, string) {
	t.Helper()
	var ok bool
	out, _ := util.CaptureLogs(func() error { ok = fn(); return nil })
	return ok, out
}

func TestCtxWireClaudeRefusesForeignEntry(t *testing.T) {
	path := claudeRefusalHome(t)

	ok, out := captureWire(t, func() bool { ok, _ := ctxWireClaude(core.RunOpts{DryRun: true}); return ok })
	if !ok || !strings.Contains(out, "would: claude mcp add") {
		t.Fatalf("dry-run on fresh home: ok=%v out=%q", ok, out)
	}

	foreign := seedClaude(t, path, "context-mode", claudeEntryJSON("my-wrapper", []string{"run-mcp", "--context-mode", "/x/context-mode"}, ""))
	ok, out = captureWire(t, func() bool { ok, _ := ctxWireClaude(core.RunOpts{DryRun: true}); return ok })
	if ok || strings.Contains(out, "would: claude mcp add") || !strings.Contains(out, "not a tokless-managed entry") {
		t.Fatalf("dry-run with foreign entry: ok=%v out=%q", ok, out)
	}
	ok, out = captureWire(t, func() bool { ok, _ := ctxWireClaude(core.RunOpts{}); return ok })
	if ok {
		t.Fatal("wire with foreign entry must report false")
	}
	if n := strings.Count(out, "not wired"); n != 1 || !strings.Contains(out, "not a tokless-managed entry") {
		t.Fatalf("want exactly one refusal warning, got %q", out)
	}
	if HasOwner("claude", "context-mode") {
		t.Fatal("refused wire must not write owner")
	}
	if raw, _ := util.ReadFileSafe(path); raw != foreign {
		t.Fatalf("foreign config changed: %s", raw)
	}
}

func TestCtxWireClaudeRepairsDriftedTarget(t *testing.T) {
	path := claudeRefusalHome(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "context-mode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	want := util.ContextModeSpawnFor("claude")
	stale := append([]string{}, want.Args...)
	stale[len(stale)-1] = "/old/mise/installs/node/26.12.0/bin/context-mode"
	if mcpStrArrEq(toAny(stale), want.Args) {
		t.Fatalf("fixture not stale: %v", want.Args)
	}
	seedClaude(t, path, "context-mode", claudeEntryJSON(want.Command, stale, `,"env":{"KEEP":"1"}`))
	if !ctxVerifyClaude() {
		t.Fatal("verify must accept owned entry with drifted target")
	}

	if ok, _ := ctxWireClaude(core.RunOpts{}); !ok {
		t.Fatal("wire must repair drifted target")
	}
	entry := claudeEntry(t, path, "context-mode")
	cmd, _ := entry.Get("command")
	args, _ := entry.Get("args")
	if cmd != want.Command || !mcpStrArrEq(args, want.Args) {
		t.Fatalf("entry not rewritten: command=%v args=%v want=%v", cmd, args, want)
	}
	if env, _ := entry.Get("env"); env == nil {
		t.Fatal("env not preserved")
	}
	if !HasOwner("claude", "context-mode") {
		t.Fatal("repaired wire must write owner")
	}
}

func TestCtxWireClaudeExtraEnvIsNoOpSuccess(t *testing.T) {
	path := claudeRefusalHome(t)
	want := util.ContextModeSpawnFor("claude")
	cfg := seedClaude(t, path, "context-mode", claudeEntryJSON(want.Command, want.Args, `,"env":{"KEEP":"1"}`))
	ok, out := captureWire(t, func() bool { ok, _ := ctxWireClaude(core.RunOpts{}); return ok })
	if !ok || strings.Contains(out, "not wired") {
		t.Fatalf("no-op with extra env: ok=%v out=%q", ok, out)
	}
	if raw, _ := util.ReadFileSafe(path); raw != cfg {
		t.Fatalf("no-op rewrote file: %s", raw)
	}
	if !HasOwner("claude", "context-mode") {
		t.Fatal("no-op success must write owner")
	}
}

func TestCtxWireClaudeWriteFailureNotWired(t *testing.T) {
	path := claudeRefusalHome(t)
	want := util.ContextModeSpawnFor("claude")
	cfg := seedClaude(t, path, "context-mode", claudeEntryJSON(want.Command, []string{"run-mcp", "--context-mode", "--ctx-agent", "claude-code", "/old/bin/context-mode"}, ""))
	util.SetWriteFileOverride(func(p, c string) error {
		if p == path {
			return errors.New("read-only")
		}
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		return os.WriteFile(p, []byte(c), 0o644)
	})
	t.Cleanup(func() { util.SetWriteFileOverride(nil) })

	ok, out := captureWire(t, func() bool { ok, _ := ctxWireClaude(core.RunOpts{}); return ok })
	if ok || !strings.Contains(out, "could not update ~/.claude.json") {
		t.Fatalf("write failure: ok=%v out=%q", ok, out)
	}
	if HasOwner("claude", "context-mode") {
		t.Fatal("failed write must not write owner")
	}
	if raw, _ := util.ReadFileSafe(path); raw != cfg {
		t.Fatalf("file changed despite write failure: %s", raw)
	}
}

func TestCtxVerifyClaudeDeadCommandStaysRed(t *testing.T) {
	path := claudeRefusalHome(t)
	want := util.ContextModeSpawnFor("claude")
	seedClaude(t, path, "context-mode", claudeEntryJSON("/gone/forever/tokless", want.Args, ""))
	if want.Command == "/gone/forever/tokless" {
		t.Skip("spawn command collides with fixture")
	}
	if ctxVerifyClaude() {
		t.Fatal("verify must stay red when the tokless command drifted")
	}
}

func TestProjectmemClaudeDriftRepairedForeignWarned(t *testing.T) {
	path := claudeRefusalHome(t)
	want := util.McpSpawnForAgent("claude", "projectmem")
	seedClaude(t, path, "projectmem", claudeEntryJSON(want.Command, []string{"run-mcp", "--tool", "projectmem", "/old/bin/pjm-mcp"}, ""))
	if !projectmemConfigureMcp("claude") {
		t.Fatal("projectmem drift must be repaired")
	}
	args, _ := claudeEntry(t, path, "projectmem").Get("args")
	if !mcpStrArrEq(args, want.Args) {
		t.Fatalf("projectmem args = %v want %v", args, want.Args)
	}

	foreign := seedClaude(t, path, "projectmem", claudeEntryJSON("my-wrapper", []string{"pjm-mcp"}, ""))
	ok, out := captureWire(t, func() bool { return projectmemConfigureMcp("claude") })
	if ok || !strings.Contains(out, "projectmem not wired") || !strings.Contains(out, "not a tokless-managed entry") {
		t.Fatalf("foreign projectmem: ok=%v out=%q", ok, out)
	}
	if raw, _ := util.ReadFileSafe(path); raw != foreign {
		t.Fatalf("foreign config changed: %s", raw)
	}
}

func TestProjectmemWireClaudeDryRunReportsRefusal(t *testing.T) {
	path := claudeRefusalHome(t)
	wire := projectmem.WireFor["claude"]
	dry := func() (bool, string) {
		return captureWire(t, func() bool { ok, _ := wire(core.RunOpts{DryRun: true}); return ok })
	}
	if ok, out := dry(); !ok || !strings.Contains(out, "would add projectmem MCP to claude") {
		t.Fatalf("fresh dry-run: ok=%v out=%q", ok, out)
	}
	seedClaude(t, path, "projectmem", claudeEntryJSON("my-wrapper", []string{"pjm-mcp"}, ""))
	ok, out := dry()
	if ok || strings.Contains(out, "would add projectmem MCP") || !strings.Contains(out, "projectmem would not be wired") {
		t.Fatalf("foreign dry-run: ok=%v out=%q", ok, out)
	}
	// The refusal is claude-only: other agents ignore ~/.claude.json.
	codex := projectmem.WireFor["codex"]
	ok, out = captureWire(t, func() bool { ok, _ := codex(core.RunOpts{DryRun: true}); return ok })
	if !ok || !strings.Contains(out, "would add projectmem MCP to codex") || strings.Contains(out, "Claude Code") {
		t.Fatalf("codex dry-run leaked claude refusal: ok=%v out=%q", ok, out)
	}
}
