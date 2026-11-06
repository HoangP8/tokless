package commands

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

func TestNodeInstallNoticeSkipped(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	ctxTools := []*core.ToolManifest{{ID: "context-mode"}}
	if minNode, needs := ctxNodeShortfall(ctxTools, true); needs || minNode != "" {
		t.Fatalf("test mode must report no shortfall: %q %v", minNode, needs)
	}
	if nodeInstallNotice(ctxTools, true, true) || nodeInstallNotice(ctxTools, true, false) {
		t.Fatal("must not allow a Node install")
	}
}

func ctxAgentEnv(t *testing.T, opencode bool) {
	t.Helper()
	agents.Register()
	tools.Register()
	withSelHome(t)
	bin := t.TempDir()
	if opencode {
		if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte("#!/bin/sh\necho 1.19.5\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
}

func TestContextModeNeedsNode(t *testing.T) {
	for _, tc := range []struct {
		name, agent, bin string
		cliHarness       bool
	}{
		{"OpenCode only", "opencode", "opencode", false},
		{"CLI harness", "claude", "claude", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctxAgentEnv(t, tc.agent == "opencode")
			seedAgentInstalled(t, tc.agent)
			if tc.agent != "opencode" {
				bin := filepath.SplitList(os.Getenv("PATH"))[0]
				if err := os.WriteFile(filepath.Join(bin, tc.bin), []byte("#!/bin/sh\necho 2.0.0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if !core.GetAgent(tc.agent).Detect().Installed || tools.CtxNeedsGlobalCLI() != tc.cliHarness {
				t.Skip("environment detects a different agent set")
			}
			ctx := core.GetTool("context-mode")
			if got := toolNeedsNode(ctx); got != tc.cliHarness {
				t.Fatalf("toolNeedsNode = %v, want %v", got, tc.cliHarness)
			}
			if !toolNeedsNode(core.GetTool("codegraph")) {
				t.Fatal("other npm tools must still need Node")
			}
			needsInstall, _, _ := updateToolFlow("context-mode", util.VersionInfo{Latest: strPtr("2.0.2")}, nil)
			if needsInstall != tc.cliHarness {
				t.Fatalf("needsInstall = %v, want %v", needsInstall, tc.cliHarness)
			}
			if _, needs := ctxNodeShortfall([]*core.ToolManifest{ctx}, true); !tc.cliHarness && needs {
				t.Fatal("no Node shortfall expected for OpenCode-only")
			}
		})
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	return <-done
}

func openCodeUpdateEnv(t *testing.T) string {
	t.Helper()
	ctxAgentEnv(t, true)
	t.Setenv("TOKLESS_TEST", "1")
	seedAgentInstalled(t, "opencode")
	if tools.CtxNeedsGlobalCLI() {
		t.Skip("another non-OpenCode agent is detected in this environment")
	}
	latest, err := util.ContextModeLatestVersion()
	if err != nil {
		t.Fatal(err)
	}
	util.SetQuiet(false)
	return latest
}

func dryRunUpdate(t *testing.T) string {
	return captureStdout(t, func() { RunUpdate(InitOptions{DryRun: true}) })
}

func TestRunUpdateDryRunRepinsStaleOpenCodePlugin(t *testing.T) {
	latest := openCodeUpdateEnv(t)
	tools.WriteOwner("opencode", "context-mode")
	write := func(spec string) {
		body := `{"plugin":["` + spec + `"],"permission":{"ctx_resume":"deny","ctx_stats":"deny","ctx_doctor":"deny","ctx_upgrade":"deny","ctx_purge":"deny"}}`
		if err := util.WriteFile(util.OpenCodePathsResolved().Config, body); err != nil {
			t.Fatal(err)
		}
	}
	write("context-mode@0.0.1")
	if out := dryRunUpdate(t); !strings.Contains(out, "re-pin OpenCode plugin") || !strings.Contains(out, "Would upgrade:") || !strings.Contains(out, "context-mode, ") {
		t.Fatalf("stale pin must be reported as a change:\n%s", out)
	}
	write("context-mode@" + latest)
	out := dryRunUpdate(t)
	if strings.Contains(out, "re-pin OpenCode plugin") || strings.Contains(out, "context-mode, ") || !regexp.MustCompile(`context-mode +plugin +\S+ v`+regexp.QuoteMeta(latest)+` +\(up to date\)`).MatchString(out) {
		t.Fatalf("current pin must be up to date:\n%s", out)
	}
}

func TestRunUpdateDryRunReportsMissingHideBlock(t *testing.T) {
	latest := openCodeUpdateEnv(t)
	if err := util.WriteFile(util.OpenCodePathsResolved().Config, `{"plugin":["context-mode@`+latest+`"]}`); err != nil {
		t.Fatal(err)
	}
	if out := dryRunUpdate(t); strings.Contains(out, "hide ctx tools") {
		t.Fatalf("user-owned plugin must not be reported:\n%s", out)
	}
	tools.WriteOwner("opencode", "context-mode")
	if out := dryRunUpdate(t); !strings.Contains(out, "hide ctx tools") || strings.Contains(out, "re-pin OpenCode plugin") {
		t.Fatalf("missing deny block must be reported as hide ctx tools:\n%s", out)
	}
}

func TestConfiguredContextModeArgvAddsServerFor2x(t *testing.T) {
	old := contextModeProbeMajor
	t.Cleanup(func() { contextModeProbeMajor = old })
	for _, tc := range []struct {
		major int
		in    []string
		want  []string
	}{
		{2, []string{"context-mode"}, []string{"context-mode", "server", "--agent", "claude-code"}},
		{2, []string{"context-mode", "server"}, []string{"context-mode", "server", "--agent", "claude-code"}},
		{2, []string{"context-mode", "server", "--agent", "x"}, []string{"context-mode", "server", "--agent", "x"}},
		{1, []string{"context-mode"}, []string{"context-mode"}},
		{0, []string{"context-mode"}, []string{"context-mode"}},
	} {
		contextModeProbeMajor = func([]string) int { return tc.major }
		if got := configuredContextModeArgv(append([]string(nil), tc.in...), "claude", "", ""); strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Fatalf("major %d: got %v want %v", tc.major, got, tc.want)
		}
	}
}

// The probe runs the whole configured command, so wrappers like `sh script` report their version.
func TestContextModeProbeMajorUsesFullArgv(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "ver.sh")
	// Prints 2.x only when called as `sh ver.sh context-mode --version`.
	body := "#!/bin/sh\n[ \"$1\" = context-mode ] && [ \"$2\" = --version ] && echo 2.0.1 || echo 1.0.0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := contextModeProbeMajor([]string{"sh", script, "context-mode"}); got != 2 {
		t.Fatalf("major = %d, want 2", got)
	}
	if got := contextModeProbeMajor([]string{"sh", script}); got != 1 {
		t.Fatalf("major = %d, want 1", got)
	}
	if got := contextModeProbeMajor([]string{filepath.Join(dir, "missing")}); got != 0 {
		t.Fatalf("missing binary major = %d, want 0", got)
	}
}

func TestContextModeAgentID(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	for in, want := range map[string]string{
		"claude": "claude-code", "copilot": "copilot-cli", "codex": "codex", "zcode": "zcode",
		"opencode": "opencode", "cursor": "vscode", "pi": "gemini", "kiro": "gemini", "": "gemini",
	} {
		if got := contextModeAgentID(in); got != want {
			t.Errorf("contextModeAgentID(%q) = %q, want %q", in, got, want)
		}
	}
	t.Setenv("CLAUDE_PROJECT_DIR", "/p")
	if got := contextModeAgentID(""); got != "claude-code" {
		t.Errorf("empty agent under Claude Code = %q, want claude-code", got)
	}
}

func TestRunUpdateMigratesLegacyOpenCodeMcp(t *testing.T) {
	openCodeUpdateEnv(t)
	tools.WriteOwner("opencode", "context-mode")
	cfgPath := util.OpenCodePathsResolved().Config
	spawn := util.PickMcpSpawn("context-mode")
	body := `{"mcp":{"context-mode":{"type":"local","command":["` + spawn.Command + `","run-mcp","--context-mode"],"enabled":true}}}`
	if err := util.WriteFile(cfgPath, body); err != nil {
		t.Fatal(err)
	}
	if out := dryRunUpdate(t); !strings.Contains(out, "migrate to plugin") || !strings.Contains(out, "Would upgrade:") {
		t.Fatalf("legacy MCP shape must be reported:\n%s", out)
	}
	resyncWiring([]*core.ToolManifest{ctxToolForTest(t)})
	raw, _ := util.ReadFileSafe(cfgPath)
	if strings.Contains(raw, "run-mcp") || !strings.Contains(raw, "context-mode@") || !strings.Contains(raw, "ctx_resume") {
		t.Fatalf("resync must swap MCP for plugin + deny block:\n%s", raw)
	}
	if out := dryRunUpdate(t); strings.Contains(out, "migrate to plugin") || !regexp.MustCompile(`context-mode +plugin .*\(up to date\)`).MatchString(out) {
		t.Fatalf("second pass must be clean:\n%s", out)
	}
}

// A tokless MCP entry without an AGENTS.md marker migrates; user content survives.
func TestRunUpdateMigratesUnmarkedOpenCodeMcp(t *testing.T) {
	const tokless = `{"type":"local","command":["/opt/tokless","run-mcp","--context-mode","/npm/context-mode"],"enabled":true}`
	const user = `{"type":"local","command":["context-mode"],"enabled":true}`
	const notes = "# my own notes\nkeep me\n"
	for _, tc := range []struct {
		name    string
		v2      bool
		body    string
		migrate bool
		keep    []string
	}{
		{"v1 root entry", false, `{"model":"a/b","permissions":["x"],"plugins":["u@1"],"mcp":{"context7":{"type":"remote","url":"https://c7"},"context-mode":` + tokless + `}}`, true,
			[]string{`"model"`, `"permissions"`, `"u@1"`, `"context7"`}},
		{"v2 servers entry", true, `{"permissions":["x"],"plugin":[],"plugins":["u@1"],"mcp":{"servers":{"context7":{"type":"remote","url":"https://c7"},"context-mode":` + tokless + `}}}`, true,
			[]string{`"permissions"`, `"plugin": []`, `"u@1"`, `"context7"`}},
		{"v1 plugin plus duplicate entry", false, `{"plugin":["context-mode@1.0.169"],"model":"a/b","mcp":{"context7":{"type":"remote","url":"https://c7"},"context-mode":` + tokless + `}}`, true,
			[]string{`"model"`, `"context7"`}},
		{"v2 plugin plus duplicate entry", true, `{"plugins":["u@1","context-mode@1.0.169"],"plugin":[],"mcp":{"servers":{"context-mode":` + tokless + `}}}`, true,
			[]string{`"plugin": []`, `"u@1"`}},
		{"v1 user entry", false, `{"mcp":{"context-mode":` + user + `}}`, false, []string{`"command": ["context-mode"]`}},
		{"v2 user entry", true, `{"mcp":{"servers":{"context-mode":` + user + `}}}`, false, []string{`"command": ["context-mode"]`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctxAgentEnv(t, false)
			t.Setenv("TOKLESS_TEST", "1")
			bin, ver := "opencode", "1.19.5"
			if tc.v2 {
				bin, ver = "opencode2", "2.0.6"
				t.Setenv("OPENCODE_CONFIG_DIR_V2", filepath.Join(util.Home(), ".config", "opencode2"))
			}
			fake := filepath.Join(filepath.SplitList(os.Getenv("PATH"))[0], bin)
			if err := os.WriteFile(fake, []byte("#!/bin/sh\necho "+ver+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			agentID := "opencode"
			paths := util.OpenCodePathsResolved()
			if tc.v2 {
				agentID, paths = "opencode2", util.OpenCodePathsV2()
			}
			seedAgentInstalled(t, agentID)
			if tools.CtxNeedsGlobalCLI() || !core.GetAgent(agentID).Detect().Installed {
				t.Skip("environment detects a different agent set")
			}
			write(t, paths.Config, tc.body)
			write(t, paths.Instructions, notes)
			if tools.HasOwner(agentID, "context-mode") {
				t.Fatal("test needs a config without an ownership marker")
			}

			out := dryRunUpdate(t)
			if got := strings.Contains(out, "migrate to plugin"); got != tc.migrate {
				t.Fatalf("migrate flagged = %v, want %v:\n%s", got, tc.migrate, out)
			}
			resyncWiring([]*core.ToolManifest{ctxToolForTest(t)})
			raw, _ := util.ReadFileSafe(paths.Config)
			if !tc.migrate {
				if want := tc.body; raw != want {
					t.Fatalf("non-tokless entry must stay untouched:\n%s", raw)
				}
				return
			}
			if strings.Contains(raw, "run-mcp") || !strings.Contains(raw, "context-mode@") {
				t.Fatalf("resync must swap the MCP entry for the plugin:\n%s", raw)
			}
			for _, k := range append([]string{"ctx_resume", "ctx_stats", "ctx_doctor", "ctx_upgrade", "ctx_purge"}, tc.keep...) {
				if !strings.Contains(raw, k) {
					t.Fatalf("missing %s after migration:\n%s", k, raw)
				}
			}
			if notesRaw, _ := util.ReadFileSafe(paths.Instructions); !strings.Contains(notesRaw, "keep me") {
				t.Fatalf("hand-written AGENTS.md content lost:\n%s", notesRaw)
			}
			if out := dryRunUpdate(t); strings.Contains(out, "migrate to plugin") {
				t.Fatalf("second pass must be clean:\n%s", out)
			}
		})
	}
}

// A refused (commented) config must not also print a rollback failure.
func TestRunInitRefusedOpenCodeConfigSkipsRollback(t *testing.T) {
	openCodeUpdateEnv(t)
	t.Setenv("TOKLESS_INSTALLER_RUN", "1")
	cfgPath := util.OpenCodePathsResolved().Config
	body := "{\n  // mine\n  \"mcp\": {\"context-mode\": {\"type\":\"local\",\"command\":[\"/opt/tokless\",\"run-mcp\",\"--context-mode\"]}}\n}\n"
	write(t, cfgPath, body)
	var code int
	out := captureStdout(t, func() {
		code = RunInit(InitOptions{Agents: []string{"opencode"}, Tools: []string{"context-mode"}})
	})
	if code == 0 || !strings.Contains(out, "not wired") {
		t.Fatalf("refused config must fail the wire (code=%d):\n%s", code, out)
	}
	if strings.Contains(out, "rollback failed") {
		t.Fatalf("refusal wrote nothing; no rollback expected:\n%s", out)
	}
	if raw, _ := util.ReadFileSafe(cfgPath); raw != body {
		t.Fatalf("config was modified:\n%s", raw)
	}
}
