package agents

import (
	"os"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// claudeMcpRepairHome isolates HOME and cwd (project-local settings land in a temp dir).
func claudeMcpRepairHome(t *testing.T) string {
	t.Helper()
	claudeProxyTestHome(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return util.ClaudeCodePaths().GlobalJSON
}

func claudeMcpEntry(t *testing.T, toolID string) *util.OrderedMap {
	t.Helper()
	raw, _ := util.ReadFileSafe(util.ClaudeCodePaths().GlobalJSON)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("claude config unparseable: %q", raw)
	}
	servers, _ := mapChild(cfg, "mcpServers")
	em, _ := servers.Get(toolID)
	entry, _ := em.(*util.OrderedMap)
	if entry == nil {
		t.Fatalf("mcpServers.%s missing: %s", toolID, raw)
	}
	return entry
}

func seedClaudeMcp(t *testing.T, path, entry string) {
	t.Helper()
	if err := util.WriteFile(path, `{"mcpServers":{"context-mode":`+entry+`}}`); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeMcpRepairsStaleContextModeTarget(t *testing.T) {
	path := claudeMcpRepairHome(t)
	want := util.McpSpawnForAgent("claude", "context-mode")
	seedClaudeMcp(t, path, `{"type":"stdio","command":`+jsonStr(want.Command)+`,"args":["run-mcp","--context-mode","--ctx-agent","claude-code","/old/mise/installs/node/26.12.0/bin/context-mode"],"env":{"KEEP":"1"},"note":"mine"}`)

	changed, _ := ConfigureClaudeMcp("context-mode")
	if !changed {
		t.Fatal("stale target must be repaired")
	}
	entry := claudeMcpEntry(t, "context-mode")
	cmd, _ := entry.Get("command")
	args, _ := entry.Get("args")
	if cmd != want.Command || jsonStr(args) != jsonStr(toAnySlice(want.Args)) {
		t.Fatalf("entry not repaired: command=%v args=%v want=%v", cmd, args, want)
	}
	typ, _ := entry.Get("type")
	_, hasEnv := entry.Get("env")
	_, hasNote := entry.Get("note")
	if typ != "stdio" || !hasEnv || !hasNote {
		t.Fatalf("user keys lost: type=%v env=%v note=%v", typ, hasEnv, hasNote)
	}
	if again, _ := ConfigureClaudeMcp("context-mode"); again {
		t.Fatal("repaired entry must be idempotent despite env")
	}
	if reason := ClaudeMcpRefusal("context-mode"); reason != "" {
		t.Fatalf("unexpected refusal: %s", reason)
	}
}

func TestClaudeMcpRepairsMovedToklessBinary(t *testing.T) {
	path := claudeMcpRepairHome(t)
	want := util.McpSpawnForAgent("claude", "context-mode")
	seedClaudeMcp(t, path, `{"type":"stdio","command":"/moved/elsewhere/tokless","args":["run-mcp","--context-mode","/old/bin/context-mode"]}`)

	if changed, _ := ConfigureClaudeMcp("context-mode"); !changed {
		t.Fatal("moved tokless binary must be repaired")
	}
	entry := claudeMcpEntry(t, "context-mode")
	if cmd, _ := entry.Get("command"); cmd != want.Command {
		t.Fatalf("command not repaired: %v want %v", cmd, want.Command)
	}
}

func TestClaudeMcpRefusesForeignEntry(t *testing.T) {
	for _, entry := range []string{
		`{"type":"stdio","command":"my-wrapper","args":["run-mcp","--context-mode","/x/context-mode"]}`,
		`{"type":"stdio","command":"/bin/tokless","args":["run-mcp","--context-mode","/x/context-mode","--extra"]}`,
		`{"type":"http","command":"/bin/tokless","args":["run-mcp","--context-mode","/x/context-mode"]}`,
		`{"command":"context-mode","args":[]}`,
	} {
		path := claudeMcpRepairHome(t)
		seedClaudeMcp(t, path, entry)
		before, _ := util.ReadFileSafe(path)
		if changed, _ := ConfigureClaudeMcp("context-mode"); changed {
			t.Fatalf("foreign entry rewritten: %s", entry)
		}
		if after, _ := util.ReadFileSafe(path); after != before {
			t.Fatalf("foreign entry file changed:\n%s", after)
		}
		reason := ClaudeMcpRefusal("context-mode")
		if reason == "" || strings.Contains(reason, "--extra") || strings.Contains(reason, "/x/") {
			t.Fatalf("bad refusal reason %q for %s", reason, entry)
		}
	}
}

func TestClaudeMcpRefusalCoversUnwritableConfig(t *testing.T) {
	path := claudeMcpRepairHome(t)
	for _, raw := range []string{"{\n// c\n\"mcpServers\":{}}", "{not json", `{"mcpServers":[]}`} {
		if err := util.WriteFile(path, raw); err != nil {
			t.Fatal(err)
		}
		if ClaudeMcpRefusal("context-mode") == "" {
			t.Fatalf("expected refusal for %q", raw)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if reason := ClaudeMcpRefusal("context-mode"); reason != "" {
		t.Fatalf("fresh home refused: %s", reason)
	}
}

func TestRemoveClaudeMcpRemovesDriftedOwnedKeepsForeign(t *testing.T) {
	path := claudeMcpRepairHome(t)
	seedClaudeMcp(t, path, `{"type":"stdio","command":"/moved/tokless","args":["run-mcp","--context-mode","--ctx-agent","claude-code","/old/bin/context-mode"]}`)
	if !RemoveClaudeMcp("context-mode") {
		t.Fatal("drifted owned entry must be removed")
	}
	if raw, _ := util.ReadFileSafe(path); strings.Contains(raw, `"context-mode"`) {
		t.Fatalf("entry still present: %s", raw)
	}

	foreign := `{"type":"stdio","command":"my-wrapper","args":["run-mcp","--context-mode","/x/context-mode"]}`
	seedClaudeMcp(t, path, foreign)
	before, _ := util.ReadFileSafe(path)
	if RemoveClaudeMcp("context-mode") {
		t.Fatal("foreign entry must not be removed")
	}
	if after, _ := util.ReadFileSafe(path); after != before {
		t.Fatalf("foreign file changed: %s", after)
	}
}

func TestClaudeMcpCodegraphExactOnly(t *testing.T) {
	path := claudeMcpRepairHome(t)
	if changed, _ := ConfigureClaudeMcp("codegraph"); !changed {
		t.Fatal("fresh codegraph entry not written")
	}
	if reason := ClaudeMcpRefusal("codegraph"); reason != "" {
		t.Fatalf("exact codegraph entry refused: %s", reason)
	}
	// Drifted codegraph is never owned-by-shape, even for a shape kiloExpectedCommand accepts.
	for _, agent := range []string{"claude", "kilo"} {
		if err := util.WriteFile(path, `{"mcpServers":{"codegraph":{"type":"stdio","command":"/moved/tokless","args":["run-mcp","--agent","`+agent+`","codegraph","serve","--mcp"]}}}`); err != nil {
			t.Fatal(err)
		}
		before, _ := util.ReadFileSafe(path)
		if ClaudeMcpOwned("codegraph", claudeMcpEntry(t, "codegraph")) {
			t.Fatalf("codegraph (--agent %s) must not be owned by shape", agent)
		}
		if changed, _ := ConfigureClaudeMcp("codegraph"); changed {
			t.Fatalf("drifted codegraph (--agent %s) must not be rewritten", agent)
		}
		if after, _ := util.ReadFileSafe(path); after != before {
			t.Fatalf("codegraph file changed: %s", after)
		}
	}
}

func TestClaudeMcpRepairsProjectmemDrift(t *testing.T) {
	path := claudeMcpRepairHome(t)
	want := util.McpSpawnForAgent("claude", "projectmem")
	if err := util.WriteFile(path, `{"mcpServers":{"projectmem":{"type":"stdio","command":"/moved/tokless","args":["run-mcp","--tool","projectmem","/old/bin/pjm-mcp"]}}}`); err != nil {
		t.Fatal(err)
	}
	if !ClaudeMcpOwned("projectmem", claudeMcpEntry(t, "projectmem")) {
		t.Skip("projectmem spawn shape is not owned on this platform")
	}
	if changed, _ := ConfigureClaudeMcp("projectmem"); !changed {
		t.Fatal("projectmem drift must be repaired")
	}
	args, _ := claudeMcpEntry(t, "projectmem").Get("args")
	if jsonStr(args) != jsonStr(toAnySlice(want.Args)) {
		t.Fatalf("projectmem args = %v want %v", args, want.Args)
	}
}

func TestClaudeMcpRefusalNamesShapeAndRemedy(t *testing.T) {
	path := claudeMcpRepairHome(t)
	seedClaudeMcp(t, path, `{"type":"stdio","command":"my-wrapper","args":["secret-token"]}`)
	reason := ClaudeMcpRefusal("context-mode")
	for _, part := range []string{"not a tokless-managed entry", "command=my-wrapper", "claude mcp remove context-mode", "then `tokless`"} {
		if !strings.Contains(reason, part) {
			t.Fatalf("reason %q missing %q", reason, part)
		}
	}
	if strings.Contains(reason, "secret-token") {
		t.Fatalf("reason leaks args: %q", reason)
	}
	seedClaudeMcp(t, path, `{"type":"stdio","command":"/bin/tokless","args":["run-mcp","--context-mode","/x/context-mode","--extra"]}`)
	if reason := ClaudeMcpRefusal("context-mode"); !strings.Contains(reason, "tokless command with an unrecognised shape") || !strings.Contains(reason, "command=tokless") {
		t.Fatalf("tokless-shaped reason = %q", reason)
	}
}

func TestClaudeMcpNullConfigIsRefusedNotPanic(t *testing.T) {
	path := claudeMcpRepairHome(t)
	if err := util.WriteFile(path, "null\n"); err != nil {
		t.Fatal(err)
	}
	if ClaudeMcpRefusal("context-mode") == "" {
		t.Fatal("null config must be refused")
	}
	if changed, _ := ConfigureClaudeMcp("context-mode"); changed {
		t.Fatal("null config must not be rewritten")
	}
	if raw, _ := util.ReadFileSafe(path); raw != "null\n" {
		t.Fatalf("null config changed: %q", raw)
	}
}
