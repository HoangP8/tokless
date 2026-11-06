package tools

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

var ctxFlagPairs = regexp.MustCompile(`"--ctx-agent",\s*"[^"]*",?\s*|"--workspace",\s*"\$\{workspaceFolder\}",?\s*`)

// downgradeCtxFlags rewrites every file under root to the pre-host-flag shape.
func downgradeCtxFlags(t *testing.T, root string) {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil || !strings.Contains(string(raw), "--ctx-agent") {
			return nil
		}
		if werr := os.WriteFile(p, ctxFlagPairs.ReplaceAll(raw, nil), 0o600); werr != nil {
			t.Fatal(werr)
		}
		n++
		return nil
	})
	if n == 0 {
		t.Fatal("no file carried --ctx-agent")
	}
}

func wantCtxAgent(t *testing.T, path, id string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if id == "gemini" { // default id: no flag, entry keeps the pre-flag shape
		if strings.Contains(string(raw), "--ctx-agent") {
			t.Fatalf("%s must not carry --ctx-agent:\n%s", path, raw)
		}
		return
	}
	if !regexp.MustCompile(`"--ctx-agent",\s*"` + regexp.QuoteMeta(id) + `"`).Match(raw) {
		t.Fatalf("%s lacks --ctx-agent %s:\n%s", path, id, raw)
	}
}

// Each harness entry carries its upstream context-mode id, and an entry written
// before the flag existed reads as stale and is rewritten in place.
func TestContextModeWiresHostAgentID(t *testing.T) {
	type wiring struct {
		agent  string
		paths  func(tmp string) map[string]string // file -> expected upstream id
		verify func() bool
		wire   func() bool
	}
	ctxWire := func(agent string) func() bool {
		return func() bool { ok, _ := contextMode.WireFor[agent](core.RunOpts{}); return ok }
	}
	ctxVerify := func(agent string) func() bool { return func() bool { return *contextMode.VerifyFor[agent]() } }
	all := []wiring{
		{"claude", func(string) map[string]string {
			return map[string]string{util.ClaudeCodePaths().GlobalJSON: "claude-code"}
		}, ctxVerifyClaude, func() bool { ok, _ := ctxWireClaude(core.RunOpts{}); return ok }},
		{"codex", func(string) map[string]string { return map[string]string{util.CodexPathsResolved().Config: "codex"} }, func() bool { return mcpHas("codex", "context-mode", false) }, func() bool { ok, _ := ctxWireCodex(core.RunOpts{}); return ok }},
		{"droid", func(tmp string) map[string]string {
			return map[string]string{filepath.Join(tmp, ".factory", "mcp.json"): "gemini"}
		}, ctxVerifyDroid, func() bool { ok, _ := ctxWireDroid(core.RunOpts{}); return ok }},
		{"antigravity", func(string) map[string]string {
			return map[string]string{util.AntigravityPathsResolved().McpConfigCLI: "gemini"}
		}, ctxVerifyAntigravity, func() bool { ok, _ := ctxWireAntigravity(core.RunOpts{}); return ok }},
		{"copilot", func(tmp string) map[string]string {
			return map[string]string{util.CopilotPathsResolved().McpConfig: "copilot-cli", filepath.Join(tmp, ".vscode", "mcp.json"): "vscode"}
		}, ctxVerifyCopilot, func() bool { ok, _ := ctxWireCopilot(core.RunOpts{}); return ok }},
		{"cursor", func(string) map[string]string { return map[string]string{util.CursorGlobalMcpPath(): "vscode"} }, ctxVerify("cursor"), ctxWire("cursor")},
		{"grok", func(tmp string) map[string]string {
			return map[string]string{filepath.Join(tmp, ".grok", "config.toml"): "gemini"}
		}, ctxVerifyGrok, func() bool { ok, _ := ctxWireGrok(core.RunOpts{}); return ok }},
		{"omp", func(string) map[string]string {
			return map[string]string{filepath.Join(agents.OmpAgentDirResolved(), "mcp.json"): "gemini"}
		}, ctxVerify("omp"), ctxWire("omp")},
		{"kilo", func(string) map[string]string { return map[string]string{util.KiloPathsResolved().Config: "gemini"} }, ctxVerify("kilo"), func() bool { ok, _ := ctxWireKilo(core.RunOpts{}); return ok }},
		{"cline", func(string) map[string]string {
			return map[string]string{util.ClinePathsResolved().McpConfig: "gemini"}
		}, ctxVerify("cline"), func() bool { ok, _ := ctxWireCline(core.RunOpts{}); return ok }},
	}
	for _, tc := range all {
		tc := tc
		t.Run(tc.agent, func(t *testing.T) {
			tmp := t.TempDir()
			chdirTemp(t)
			util.SetHomeOverride(tmp)
			t.Setenv("HOME", tmp)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
			t.Setenv("TOKLESS_TEST", "1")
			t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(tmp, "pi"))
			agents.SetIdeProjectRoot(tmp)
			t.Cleanup(func() {
				util.SetHomeOverride("")
				agents.SetIdeProjectRoot("")
			})
			if !tc.wire() || !tc.verify() {
				t.Fatalf("%s: fresh wire failed verify", tc.agent)
			}
			for p, id := range tc.paths(tmp) {
				wantCtxAgent(t, p, id)
			}
			flagged := false
			for _, id := range tc.paths(tmp) {
				flagged = flagged || id != "gemini"
			}
			if !flagged { // gemini-mapped: nothing to migrate, verify stays true
				if !tc.verify() {
					t.Fatalf("%s: unflagged entry must verify", tc.agent)
				}
				return
			}
			downgradeCtxFlags(t, tmp)
			if tc.verify() {
				t.Fatalf("%s: entry without --ctx-agent must not verify", tc.agent)
			}
			if !tc.wire() || !tc.verify() {
				t.Fatalf("%s: old entry was not rewritten", tc.agent)
			}
			for p, id := range tc.paths(tmp) {
				wantCtxAgent(t, p, id)
			}
		})
	}
}

func TestContextModePiKeepsPreFlagShape(t *testing.T) {
	tmp := t.TempDir()
	chdirTemp(t)
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("TOKLESS_TEST", "1")
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(tmp, "pi"))
	t.Cleanup(func() { util.SetHomeOverride("") })
	if c, _ := agents.ConfigurePiMcp("context-mode"); !c || !ctxVerifyPi() {
		t.Fatal("pi wire failed verify")
	}
	wantCtxAgent(t, filepath.Join(tmp, "pi", "mcp.json"), "gemini")
}
