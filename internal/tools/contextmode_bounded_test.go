package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// TestContextModeForeignMcpEntryFailsVerify proves a pre-existing direct
// context-mode MCP entry (not routed through `tokless run-mcp --context-mode`)
// cannot pass verify, then proves the bounded entry does.
func TestContextModeForeignMcpEntryFailsVerify(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})
	piDir := filepath.Join(tmp, "pi")
	t.Setenv("PI_CODING_AGENT_DIR", piDir)

	tests := []struct {
		agent   string
		paths   []string
		foreign string
		verify  func() bool
		wire    func() bool
	}{
		{
			agent:   "claude",
			paths:   []string{util.ClaudeCodePaths().GlobalJSON},
			foreign: `{"mcpServers":{"context-mode":{"type":"stdio","command":"context-mode","args":[]}}}`,
			verify:  ctxVerifyClaude,
			wire:    func() bool { ok, _ := ctxWireClaude(core.RunOpts{}); return ok },
		},
		{
			agent:   "opencode",
			paths:   []string{util.OpenCodePathsResolved().Config},
			foreign: `{"mcp":{"context-mode":{"type":"local","command":["context-mode"],"enabled":true}}}`,
			verify:  ctxVerifyOpenCode,
			wire:    func() bool { ok, _ := ctxWireOpenCode(core.RunOpts{}); return ok },
		},
		{
			agent:   "codex",
			paths:   []string{util.CodexPathsResolved().Config},
			foreign: "[mcp_servers.context_mode]\ncommand = \"context-mode\"\nargs = []\n",
			verify:  ctxVerifyCodex,
			wire:    func() bool { ok, _ := ctxWireCodex(core.RunOpts{}); return ok },
		},
		{
			agent:   "droid",
			paths:   []string{filepath.Join(tmp, ".factory", "mcp.json")},
			foreign: `{"mcpServers":{"context-mode":{"command":"context-mode","args":[]}}}`,
			verify:  ctxVerifyDroid,
			wire:    func() bool { ok, _ := ctxWireDroid(core.RunOpts{}); return ok },
		},
		{
			agent:   "pi",
			paths:   []string{filepath.Join(piDir, "mcp.json")},
			foreign: `{"mcpServers":{"context-mode":{"command":"context-mode","args":[],"exposure":"direct"}}}`,
			verify:  ctxVerifyPi,
			wire: func() bool {
				c, _ := agents.ConfigurePiMcp("context-mode")
				return c && ctxVerifyPi()
			},
		},
		{
			agent:   "antigravity",
			paths:   []string{util.AntigravityPathsResolved().McpConfigCLI, filepath.Join(util.Home(), ".gemini", "antigravity-cli", "mcp_config.json")},
			foreign: `{"mcpServers":{"context-mode":{"command":"context-mode","trust":true}}}`,
			verify:  ctxVerifyAntigravity,
			wire:    func() bool { ok, _ := ctxWireAntigravity(core.RunOpts{}); return ok },
		},
		{
			agent:   "copilot",
			paths:   []string{util.CopilotPathsResolved().McpConfig},
			foreign: `{"mcpServers":{"context-mode":{"type":"local","command":"context-mode","args":[]}}}`,
			verify:  ctxVerifyCopilot,
			wire:    func() bool { ok, _ := ctxWireCopilot(core.RunOpts{}); return ok },
		},
		{
			agent:   "cursor",
			paths:   []string{util.CursorGlobalMcpPath()},
			foreign: `{"mcpServers":{"context-mode":{"type":"stdio","command":"context-mode","args":[]}}}`,
			verify:  func() bool { return *contextMode.VerifyFor["cursor"]() },
			wire:    func() bool { ok, _ := contextMode.WireFor["cursor"](core.RunOpts{}); return ok },
		},
		{
			agent:   "grok",
			paths:   []string{filepath.Join(tmp, ".grok", "config.toml")},
			foreign: "[mcp_servers.context-mode]\ncommand = \"context-mode\"\nargs = []\n",
			verify:  ctxVerifyGrok,
			wire:    func() bool { ok, _ := ctxWireGrok(core.RunOpts{}); return ok },
		},
		{
			agent:   "omp",
			paths:   []string{filepath.Join(agents.OmpAgentDirResolved(), "mcp.json")},
			foreign: `{"mcpServers":{"context-mode":{"type":"stdio","command":"context-mode","args":[]}}}`,
			verify:  func() bool { return *contextMode.VerifyFor["omp"]() },
			wire:    func() bool { ok, _ := contextMode.WireFor["omp"](core.RunOpts{}); return ok },
		},
		{
			agent:   "kilo",
			paths:   []string{util.KiloPathsResolved().Config},
			foreign: `{"mcp":{"context-mode":{"type":"stdio","command":"context-mode","args":[]}}}`,
			verify:  func() bool { return *contextMode.VerifyFor["kilo"]() },
			wire:    func() bool { ok, _ := ctxWireKilo(core.RunOpts{}); return ok },
		},
		{
			agent:   "cline",
			paths:   []string{util.ClinePathsResolved().McpConfig},
			foreign: `{"mcpServers":{"context-mode":{"type":"stdio","command":"context-mode","args":[]}}}`,
			verify:  func() bool { return *contextMode.VerifyFor["cline"]() },
			wire:    func() bool { ok, _ := ctxWireCline(core.RunOpts{}); return ok },
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.agent, func(t *testing.T) {
			for _, p := range tc.paths {
				if err := util.EnsureDir(filepath.Dir(p)); err != nil {
					t.Fatal(err)
				}
				if err := util.WriteFile(p, tc.foreign); err != nil {
					t.Fatal(err)
				}
			}
			if tc.agent == "codex" {
				// Keep the instructions marker so only the MCP shape can fail.
				WriteOwner("codex", "context-mode")
			}
			if tc.verify() {
				t.Fatalf("%s: foreign context-mode MCP entry passed verify", tc.agent)
			}
			for _, p := range tc.paths {
				_ = os.Remove(p)
			}
			if !tc.wire() {
				t.Fatalf("%s: wire did not report success", tc.agent)
			}
			if !tc.verify() {
				t.Fatalf("%s: bounded context-mode MCP entry failed verify", tc.agent)
			}
		})
	}
}

// TestContextModeOpenCodePluginFailsVerify: the wire path removes the plugin and
// writes the bounded MCP entry, so a leftover plugin entry is a bypass.
func TestContextModeOpenCodePluginFailsVerify(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})

	path := util.OpenCodePathsResolved().Config
	if c, _ := agents.ConfigureOpenCodeMcp("context-mode"); !c {
		t.Fatal("openCode configure did not write bounded MCP")
	}
	if !ctxVerifyOpenCode() {
		t.Fatal("bounded opencode MCP without plugin must verify")
	}
	cfg := util.TryParseJsonc(mustRead(t, path))
	if cfg == nil {
		t.Fatalf("parse %s", path)
	}
	cfg.Set("plugin", []any{"context-mode"})
	if err := util.WriteFile(path, util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}
	if ctxVerifyOpenCode() {
		t.Fatal("plugin entry must fail opencode verify")
	}
}

// TestContextModeCodexLegacyHyphenFailsVerify: a legacy hyphen block is a bypass
// even when a valid underscore block is also present.
func TestContextModeCodexLegacyHyphenFailsVerify(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })

	WriteOwner("codex", "context-mode")
	cfgPath := util.CodexPathsResolved().Config
	spawn := util.McpSpawnFor("context-mode")
	block := util.NewTomlBlock("mcp_servers.context_mode")
	block.Set("command", spawn.Command)
	block.Set("args", spawn.Args)
	raw := util.UpsertBlock("[mcp_servers.context-mode]\ncommand = \"context-mode\"\n\n", block, false)
	if err := util.WriteFile(cfgPath, raw); err != nil {
		t.Fatal(err)
	}
	if ctxVerifyCodex() {
		t.Fatal("legacy hyphen block must fail codex verify")
	}
	if !wireCodexManual() {
		t.Fatal("wireCodexManual returned false")
	}
	if !ctxVerifyCodex() {
		t.Fatal("after wire, hyphen block removed but verify still fails")
	}
}

// TestParseCodexContextModeBlock covers the small raw TOML parser.
func TestParseCodexContextModeBlock(t *testing.T) {
	b := parseCodexContextModeBlock("[mcp_servers.context_mode]\ncommand = \"tokless\"\nargs = [\"run-mcp\", \"--context-mode\", \"npx\"]\nenabled = true # keep\n")
	if b.command != "tokless" {
		t.Fatalf("command = %q", b.command)
	}
	if !mcpStrArrEq(b.args, []string{"run-mcp", "--context-mode", "npx"}) {
		t.Fatalf("args = %#v", b.args)
	}
}

// TestContextModeLooseVsStrictVerify proves WiredAny (loose) detects legacy
// context-mode wiring while VerifyFor (strict) rejects it — so resync heals
// but doctor/init still flag bypass states.
func TestContextModeLooseVsStrictVerify(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(tmp)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
	})

	path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(path, `{"plugin":["context-mode@0.0.1"]}`); err != nil {
		t.Fatal(err)
	}

	// Loose gate must see the legacy plugin entry.
	r := ctxWiredAnyOpenCode()
	if !r {
		t.Fatal("ctxWiredAnyOpenCode must be true for legacy plugin entry")
	}
	// Strict verify must reject the plugin-only entry (no bounded MCP).
	if ctxVerifyOpenCode() {
		t.Fatal("ctxVerifyOpenCode must be false for plugin-only entry")
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		t.Fatalf("read %s", path)
	}
	return raw
}
