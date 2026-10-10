package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func TestJSONMcpAdapterOwnership(t *testing.T) {
	withMcpTest(t)
	p := filepath.Join(t.TempDir(), "config.json")
	a := jsonMcpAdapter("test", func() string { return p }, "mcpServers")
	req := core.McpRequest{ToolID: "probe", Command: []string{"tokless", "mcp"}}
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write(`{"mcpServers":{"probe":{"command":["npx"],"args":[]}}}`)
	before := read()
	if r := a.configure(req); r.Changed || r.Err == nil || read() != before {
		t.Fatal("foreign configure changed file")
	}
	write(`{"mcpServers":{"probe":{"command":["/usr/bin/tokless"],"args":["old"]}}}`)
	if r := a.configure(req); !r.Changed {
		t.Fatalf("owned configure: %+v", r)
	}
	var cfg map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(read()), &cfg); err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(cfg["mcpServers"]["probe"], &entry); err != nil || entry.Command != "tokless" || len(entry.Args) != 1 || entry.Args[0] != "mcp" {
		t.Fatalf("entry not refreshed: %+v %v", entry, err)
	}
	for _, remove := range []bool{false, true} {
		write("{ not json")
		before = read()
		var r core.McpReply
		if remove {
			r = a.remove(req)
		} else {
			r = a.configure(req)
		}
		if r.Changed || r.Err == nil || read() != before {
			t.Fatal("malformed config modified")
		}
	}
	write(`{"mcpServers":{"probe":{"command":["tokless"],"args":[]},"foreign":{"command":["npx"],"args":[]}}}`)
	if r := a.remove(req); !r.Changed || strings.Contains(read(), `"probe"`) || !strings.Contains(read(), `"foreign"`) {
		t.Fatalf("remove failed: %+v", r)
	}
	write(`{"mcpServers":{"foreign":{}}}`)
	before = read()
	if r := a.remove(req); r.Changed || r.Err != nil || read() != before {
		t.Fatal("absent remove wrote")
	}
}

// TestJSONMcpAdapterStringCommandOwnership pins the real kimi/cmd mcp.json
// shape: command as a plain string with "enabled". Such tokless entries must
// count as owned, otherwise every re-wire is refused as foreign (issue #0038).
func TestJSONMcpAdapterStringCommandOwnership(t *testing.T) {
	withMcpTest(t)
	p := filepath.Join(t.TempDir(), "mcp.json")
	a := jsonMcpAdapter("test", func() string { return p }, "mcpServers")
	req := core.McpRequest{ToolID: "context-mode", Command: []string{"/home/u/.local/bin/tokless", "run-mcp", "--context-mode", "/bin/context-mode"}}
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Owned (string command + enabled), pre-existing native shape.
	write(`{"mcpServers":{"context-mode":{"command":"/home/u/.local/bin/tokless","args":["run-mcp","--context-mode","/bin/context-mode"],"enabled":true}}}`)
	if r := a.configure(req); r.Err != nil || !r.Changed {
		t.Fatalf("owned string-command configure refused: %+v", r)
	}
	if !a.has(req) {
		t.Fatal("has false after configure")
	}
	// Foreign string command still refused.
	write(`{"mcpServers":{"context-mode":{"command":"/usr/bin/npx","args":[]}}}`)
	before, _ := os.ReadFile(p)
	if r := a.configure(req); r.Err == nil || r.Changed || string(mustRead(t, p)) != string(before) {
		t.Fatalf("foreign configure not refused: %+v", r)
	}
	// Foreign remove refused too.
	if r := a.remove(req); r.Err == nil {
		t.Fatalf("foreign remove not refused: %+v", r)
	}
}

// TestJSONMcpAdapterDottedNestedPath pins zcode's nested mcp.servers schema.
// configure must navigate/create the nested object, never emit a literal
// top-level "mcp.servers" key, and refresh existing type:"local" entries to
// type:"stdio" (zcode drops non-stdio entries at runtime).
func TestJSONMcpAdapterDottedNestedPath(t *testing.T) {
	withMcpTest(t)
	p := filepath.Join(t.TempDir(), "config.json")
	a := jsonMcpAdapter("zcode", func() string { return p }, "mcp.servers")
	req := core.McpRequest{ToolID: "context-mode", Command: []string{"/home/u/.local/bin/tokless", "run-mcp", "--context-mode", "/bin/context-mode"}}
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Existing nested native entry (type local, string command).
	write(`{"mcp":{"servers":{"context-mode":{"type":"local","command":"/home/u/.local/bin/tokless","args":["run-mcp","--context-mode","/bin/context-mode"],"enabled":true}}}}`)
	if r := a.configure(req); r.Err != nil || !r.Changed {
		t.Fatalf("nested configure refused: %+v", r)
	}
	var out struct {
		MCP struct {
			Servers map[string]struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Enable  bool   `json:"enable"`
			} `json:"servers"`
		} `json:"mcp"`
		Literal map[string]json.RawMessage `json:"mcp.servers"`
	}
	if err := json.Unmarshal(mustRead(t, p), &out); err != nil {
		t.Fatal(err)
	}
	if out.Literal != nil {
		t.Fatalf("literal top-level mcp.servers key written: %v", out.Literal)
	}
	e, ok := out.MCP.Servers["context-mode"]
	if !ok || e.Type != "stdio" || e.Command != "/home/u/.local/bin/tokless" || !e.Enable {
		t.Fatalf("nested entry not refreshed to stdio: %+v", e)
	}
	if !a.has(req) {
		t.Fatal("has false after configure")
	}
	// Idempotent second configure: still owned, no foreign refusal.
	if r := a.configure(req); r.Err != nil {
		t.Fatalf("idempotent configure refused: %+v", r)
	}
	// Foreign nested entry refused.
	write(`{"mcp":{"servers":{"context-mode":{"type":"stdio","command":"/usr/bin/npx","args":[]}}}}`)
	before := mustRead(t, p)
	if r := a.configure(req); r.Err == nil || r.Changed || string(mustRead(t, p)) != string(before) {
		t.Fatalf("foreign nested configure not refused: %+v", r)
	}
	if r := a.remove(req); r.Err == nil {
		t.Fatalf("foreign nested remove not refused: %+v", r)
	}
	// Owned remove.
	write(`{"mcp":{"servers":{"context-mode":{"type":"stdio","command":"/home/u/.local/bin/tokless","args":["run-mcp","--context-mode","/bin/context-mode"]}}}}`)
	if r := a.remove(req); !r.Changed || r.Err != nil || a.has(req) {
		t.Fatalf("owned nested remove failed: %+v", r)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDshMcpAdapterOwnBlockRemoval(t *testing.T) {
	withMcpTest(t)
	p := filepath.Join(util.Home(), ".dsh", "cordis.patch.yml")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	foreign := "- insert:\n    - id: before\n      command: npx\n"
	if err := os.WriteFile(p, []byte(foreign), 0600); err != nil {
		t.Fatal(err)
	}
	a := dshMcpAdapter()
	req := core.McpRequest{ToolID: "probe", Command: []string{"tokless", "mcp"}}
	if r := a.configure(req); !r.Changed {
		t.Fatalf("configure: %+v", r)
	}
	if !a.has(req) {
		t.Fatal("has false after configure")
	}
	after := readFileForTest(t, p) + "\n- insert:\n    - id: after\n      command: npx\n"
	if err := os.WriteFile(p, []byte(after), 0600); err != nil {
		t.Fatal(err)
	}
	if r := a.remove(req); !r.Changed {
		t.Fatalf("remove: %+v", r)
	}
	got := readFileForTest(t, p)
	if !strings.HasPrefix(got, foreign) || !strings.Contains(got, "id: after") || strings.Contains(got, "mcp-probe") || a.has(req) {
		t.Fatalf("unexpected result: %q", got)
	}
	bad := "- insert:\n    - id: mcp-probe\n      name: '@deepseek-ai/dsh-mcp-client'\n      config:\n        serverName: probe\n        command: npx mine\n"
	if err := os.WriteFile(p, []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	if r := a.remove(req); r.Changed || r.Err == nil || readFileForTest(t, p) != bad {
		t.Fatal("foreign block removed")
	}
}

func readFileForTest(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// withMcpTest pins home and cwd to temp dirs so adapter writes stay contained.
func withMcpTest(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	t.Setenv("PI_CONFIG_DIR", filepath.Join(home, ".omp"))
	t.Setenv("GROK_HOME", "")
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() {
		util.SetHomeOverride("")
		_ = os.Chdir(cwd)
	})
}

func allMcpAgentIDs() []string {
	return []string{
		"claude", "opencode", "codex", "cursor", "antigravity", "copilot",
		"droid", "grok", "pi", "omp", "kilo", "cline",
	}
}

func TestMcpAdapterForCoversEveryAgent(t *testing.T) {
	for _, id := range allMcpAgentIDs() {
		if McpAdapterFor(id) == nil {
			t.Errorf("McpAdapterFor(%q) = nil", id)
		}
	}
	if McpAdapterFor("unknown") != nil {
		t.Error("McpAdapterFor(unknown) should be nil")
	}
}

func TestMcpAdaptersAssignedToManifests(t *testing.T) {
	manifests := []*core.AgentManifest{
		claude, opencode, codex, cursor, antigravity, copilot,
		droid, grok, pi, omp, kilo, cline,
	}
	for _, m := range manifests {
		if m.Mcp == nil {
			t.Errorf("%s manifest is missing its Mcp adapter", m.ID)
		}
	}
}

func TestMcpAdapterRoundTripAllAgents(t *testing.T) {
	withMcpTest(t)
	for _, id := range allMcpAgentIDs() {
		t.Run(id, func(t *testing.T) {
			a := McpAdapterFor(id)
			if a == nil {
				t.Fatal("no adapter")
			}
			cmd := McpCommand(id, "context-mode")
			reply := a.ConfigureMcp("context-mode", core.WithCommand(cmd))
			if reply.Err != nil {
				t.Fatalf("ConfigureMcp: %v", reply.Err)
			}
			if !a.HasMcp("context-mode", core.WithCommand(cmd)) {
				t.Fatalf("strict HasMcp false after configure (changed=%v file=%s)", reply.Changed, reply.File)
			}
			if !a.HasMcp("context-mode", core.WithAnyShape()) {
				t.Fatal("loose HasMcp false after configure")
			}
			if r := a.RemoveMcp("context-mode", core.WithCommand(cmd)); r.Err != nil {
				t.Fatalf("RemoveMcp: %v", r.Err)
			}
		})
	}
}

func TestClaudeMcpAdapterStrictRejectsForeign(t *testing.T) {
	withMcpTest(t)
	a := McpAdapterFor("claude")
	cmd := McpCommand("claude", "context-mode")
	if r := a.ConfigureMcp("context-mode", core.WithCommand(cmd)); r.Err != nil {
		t.Fatalf("ConfigureMcp: %v", r.Err)
	}
	if a.HasMcp("context-mode", core.WithCommand([]string{"/usr/bin/foreign-mcp", "--stdio"})) {
		t.Fatal("foreign command passed the strict gate")
	}
	if !a.HasMcp("context-mode", core.WithAnyShape()) {
		t.Fatal("loose presence gate should still pass")
	}
}

func TestCodexMcpAdapterStrictShape(t *testing.T) {
	withMcpTest(t)
	a := McpAdapterFor("codex")
	cmd := McpCommand("codex", "context-mode")
	if r := a.ConfigureMcp("context-mode", core.WithCommand(cmd)); r.Err != nil {
		t.Fatalf("ConfigureMcp: %v", r.Err)
	}
	if !a.HasMcp("context-mode", core.WithCommand(cmd)) {
		t.Fatal("bounded underscore block not recognized")
	}

	p := util.CodexPathsResolved().Config
	if err := util.WriteFile(p, "[mcp_servers.projectmem]\ncommand = \"pjm-mcp\"\nargs = []\n"); err != nil {
		t.Fatal(err)
	}
	pm := McpCommand("codex", "projectmem")
	if a.HasMcp("projectmem", core.WithCommand(pm)) {
		t.Fatal("foreign projectmem block passed the strict gate")
	}
	if !a.HasMcp("projectmem", core.WithAnyShape()) {
		t.Fatal("loose existence gate should accept a foreign block")
	}
}

func TestOpenCodeMcpAdapterAnyShape(t *testing.T) {
	withMcpTest(t)
	a := McpAdapterFor("opencode")

	p := util.OpenCodePathsResolved().Config
	if err := util.EnsureDir(filepath.Dir(p)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(p, `{"mcp":{"projectmem":{"type":"local","command":["/usr/bin/foreign"],"enabled":true}}}`); err != nil {
		t.Fatal(err)
	}
	pm := McpCommand("opencode", "projectmem")
	if a.HasMcp("projectmem", core.WithCommand(pm)) {
		t.Fatal("foreign command passed the strict gate")
	}
	if !a.HasMcp("projectmem", core.WithAnyShape()) {
		t.Fatal("loose shape gate should accept the v1 entry")
	}
}
