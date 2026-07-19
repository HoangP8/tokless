package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestWindowsBashHostilePath(t *testing.T) {
	orig := util.IsWin
	defer func() { util.IsWin = orig }()

	util.IsWin = true
	hostile := []string{
		`C:\Users\user\AppData\Local\Programs\tokless\tokless.exe`,
		`D:\tokless.exe`,
		`\\server\share\tokless.exe`,
	}
	for _, p := range hostile {
		if !windowsBashHostilePath(p) {
			t.Errorf("expected hostile: %q", p)
		}
	}
	safe := []string{
		"C:/Users/user/AppData/Local/Programs/tokless/tokless.exe",
		"tokless",
		"/usr/local/bin/tokless",
		"",
	}
	for _, p := range safe {
		if windowsBashHostilePath(p) {
			t.Errorf("expected safe: %q", p)
		}
	}

	util.IsWin = false
	if windowsBashHostilePath(`C:\Users\user\tokless.exe`) {
		t.Error("non-Windows must not flag backslash paths")
	}
}

func TestProbeHookCommandBashHostile(t *testing.T) {
	orig := util.IsWin
	defer func() { util.IsWin = orig }()
	util.IsWin = true

	cmd := `C:\Users\user\AppData\Local\Programs\tokless\tokless.exe rtk-hook claude`
	got := probeHookCommand(cmd)
	if got == "" || !strings.Contains(got, "Git Bash") {
		t.Fatalf("expected Git Bash warning, got %q", got)
	}
}

func TestProbeHookCommandForwardSlashOK(t *testing.T) {
	orig := util.IsWin
	defer func() { util.IsWin = orig }()
	util.IsWin = true

	// Write a fake tokless that exits 0 on any args.
	dir := t.TempDir()
	fake := filepath.Join(dir, "tokless")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}
	writeExit0(t, fake)

	// Use forward-slash form (issue #21 fix).
	slashy := filepath.ToSlash(fake)
	cmd := slashy + " rtk-hook claude"
	if detail := probeHookCommand(cmd); detail != "" {
		t.Fatalf("forward-slash hook should pass, got %q", detail)
	}
}

func TestProbeHookCommandMissingBinary(t *testing.T) {
	orig := util.IsWin
	defer func() { util.IsWin = orig }()
	util.IsWin = false

	detail := probeHookCommand("/no/such/tokless-binary-xyz rtk-hook claude")
	if detail == "" || !strings.Contains(detail, "not found") {
		t.Fatalf("expected not found, got %q", detail)
	}
}

func TestProbeHookCommandIgnoresUserWrapper(t *testing.T) {
	if detail := probeHookCommand("my-wrapper rtk-hook claude"); detail != "" {
		if !strings.Contains(detail, "not found") && !strings.Contains(detail, "PATH") {
			t.Fatalf("unexpected: %q", detail)
		}
	}
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "custom-wrapper")
	if runtime.GOOS == "windows" {
		wrapper += ".bat"
		if err := os.WriteFile(wrapper, []byte("@echo off\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		writeExit0(t, wrapper)
	}
	if detail := probeHookCommand(wrapper + " rtk-hook claude"); detail != "" {
		t.Fatalf("existing user wrapper should pass existence: %q", detail)
	}
}

func TestIsToklessManagedHook(t *testing.T) {
	yes := [][]string{
		{"tokless", "rtk-hook", "claude"},
		{"C:/bin/tokless.exe", "rtk-hook", "codex"},
		{"tokless", "codex-perm", "codex"},
		{"tokless", "index", "--auto", "droid"},
		{"tokless", "agy-hook", "codegraph-index"},
		{"tokless", "copilot-hook", "codegraph-index"},
	}
	for _, f := range yes {
		if !isToklessManagedHook(f) {
			t.Errorf("expected managed: %v", f)
		}
	}
	no := [][]string{
		{"custom-wrapper", "rtk-hook", "claude"},
		{"tokless", "doctor"},
		{"rtk", "hook", "claude"},
		{"tokless"},
	}
	for _, f := range no {
		if isToklessManagedHook(f) {
			t.Errorf("expected not managed: %v", f)
		}
	}
}

func TestUnwrapRunMcp(t *testing.T) {
	cmd, args := unwrapRunMcp(`/tokless`, []string{"run-mcp", "--agent", "opencode", "cmd", "/c", `C:\a\codegraph.CMD`, "serve", "--mcp"})
	if cmd != "cmd" || len(args) != 4 || args[0] != "/c" {
		t.Fatalf("unwrap = %q %v", cmd, args)
	}
	cmd, args = unwrapRunMcp("codegraph", []string{"serve", "--mcp"})
	if cmd != "codegraph" || len(args) != 2 {
		t.Fatalf("passthrough = %q %v", cmd, args)
	}
}

func TestNormalizedCmdBatchArgsUsedByProbe(t *testing.T) {
	got := normalizedCmdBatchArgs("cmd", []string{"/c", "C:/Users/user/AppData/Roaming/npm/codegraph.CMD", "serve"}, true)
	want := `C:\Users\user\AppData\Roaming\npm\codegraph.CMD`
	if got[1] != want {
		t.Fatalf("normalize = %q, want %q", got[1], want)
	}
}

func TestProbeMcpSpawnMissingOuter(t *testing.T) {
	detail := probeMcpSpawn("/no/such/tokless-xyz", []string{"run-mcp", "--agent", "opencode", "codegraph", "serve", "--mcp"})
	if detail == "" || !strings.Contains(detail, "not found") {
		t.Fatalf("expected outer missing, got %q", detail)
	}
}

func TestProbeMcpSpawnCmdBatchMissing(t *testing.T) {
	if _, err := exec.LookPath("cmd"); err != nil {
		if _, err2 := exec.LookPath("cmd.exe"); err2 != nil {
			t.Skip("cmd not available")
		}
	}
	detail := probeMcpSpawn("cmd", []string{"/c", "/no/such/dir/codegraph.CMD", "serve", "--mcp"})
	if detail == "" || !strings.Contains(detail, "not found") {
		t.Fatalf("expected batch missing, got %q", detail)
	}
}

func TestProbeMcpSpawnCmdBatchLive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd batch live probe is Windows-oriented")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "AppData", "Roaming", "npm")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(sub, "codegraph.CMD")
	if err := os.WriteFile(batch, []byte("@echo 1.2.3\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fwd := filepath.ToSlash(batch)
	tokless := filepath.Join(dir, "tokless.exe")
	writeExit0(t, tokless)

	detail := probeMcpSpawn(tokless, []string{"run-mcp", "--agent", "opencode", "cmd", "/c", fwd, "serve", "--mcp"})
	if detail != "" {
		t.Fatalf("normalized batch probe should pass, got %q", detail)
	}
}

func TestProbeMcpSpawnCodegraphDirect(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codegraph")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	writeExit0(t, bin)
	if detail := probeMcpSpawn(bin, []string{"serve", "--mcp"}); detail != "" {
		t.Fatalf("direct codegraph should pass, got %q", detail)
	}
}

func TestMcpFromCommandArgsFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "claude.json")
	body := `{
  "mcpServers": {
    "codegraph": {
      "type": "stdio",
      "command": "/opt/tokless",
      "args": ["run-mcp", "--agent", "claude", "codegraph", "serve", "--mcp"]
    }
  }
}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	spawns := mcpFromCommandArgsFile(p, "mcpServers", "codegraph")
	if len(spawns) != 1 || spawns[0].Command != "/opt/tokless" || len(spawns[0].Args) != 6 {
		t.Fatalf("spawns = %+v", spawns)
	}
}

func TestMcpFromCommandArrayFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "opencode.json")
	body := `{
  "mcp": {
    "codegraph": {
      "type": "local",
      "command": ["C:/tokless.exe", "run-mcp", "--agent", "opencode", "cmd", "/c", "C:/npm/codegraph.CMD", "serve", "--mcp"],
      "enabled": true
    }
  }
}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	spawns := mcpFromCommandArrayFile(p, "mcp", "codegraph")
	if len(spawns) != 1 || spawns[0].Command != "C:/tokless.exe" || len(spawns[0].Args) != 8 {
		t.Fatalf("spawns = %+v", spawns)
	}
	inner, args := unwrapRunMcp(spawns[0].Command, spawns[0].Args)
	if inner != "cmd" || len(args) < 2 || args[1] != "C:/npm/codegraph.CMD" {
		t.Fatalf("inner = %q %v", inner, args)
	}
}

func TestMcpFromCodexToml(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.toml")
	body := `
[mcp_servers.other]
command = "x"

[mcp_servers.codegraph]
command = "C:\\tokless.exe"
args = ["run-mcp", "--agent", "codex", "cmd", "/c", "C:\\npm\\codegraph.CMD", "serve", "--mcp"]
enabled = true
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	spawns := mcpFromCodexToml("codegraph")
	if len(spawns) != 1 {
		t.Fatalf("spawns = %+v", spawns)
	}
	if spawns[0].Command != `C:\tokless.exe` {
		t.Fatalf("command = %q", spawns[0].Command)
	}
	if len(spawns[0].Args) != 8 || spawns[0].Args[0] != "run-mcp" {
		t.Fatalf("args = %v", spawns[0].Args)
	}
}

func TestExtractJSONCommands(t *testing.T) {
	raw := `{"hooks":{"PreToolUse":[{"hooks":[{"command":"C:\\bin\\tokless.exe rtk-hook claude"},{"command":"echo hi"}]}]}}`
	got := extractJSONCommands(raw)
	if len(got) != 2 || !strings.Contains(got[0], "rtk-hook") {
		t.Fatalf("got %v", got)
	}
}

func TestManagedHookCommandsClaude(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	cp := util.ClaudeCodePaths()
	if err := os.MkdirAll(cp.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "hooks": {
    "PreToolUse": [
      {"matcher":"Bash","hooks":[
        {"type":"command","command":"C:/tokless.exe rtk-hook claude"},
        {"type":"command","command":"echo user"}
      ]}
    ]
  }
}`
	if err := os.WriteFile(cp.Settings, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds := managedHookCommands("claude")
	if len(cmds) != 1 || !strings.Contains(cmds[0], "rtk-hook claude") {
		t.Fatalf("cmds = %v", cmds)
	}
}

func TestProbeAgentRuntimeClaudeBashHostile(t *testing.T) {
	orig := util.IsWin
	defer func() { util.IsWin = orig }()
	util.IsWin = true

	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	cp := util.ClaudeCodePaths()
	if err := os.MkdirAll(cp.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"C:\\Users\\user\\AppData\\Local\\Programs\\tokless\\tokless.exe rtk-hook claude"}]}]}}`
	if err := os.WriteFile(cp.Settings, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := probeAgentRuntime("claude")
	if len(issues) == 0 {
		t.Fatal("expected runtime issue for bash-hostile hook")
	}
	if issues[0].kind != "hook" || !strings.Contains(issues[0].detail, "Git Bash") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestProbeAgentRuntimeOpenCodeMcpMissingBatch(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	binDir := t.TempDir()
	tokless := filepath.Join(binDir, "tokless")
	if runtime.GOOS == "windows" {
		tokless += ".exe"
	}
	writeExit0(t, tokless)

	op := util.OpenCodePathsResolved()
	if err := os.MkdirAll(op.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "mcp": {
    "codegraph": {
      "type": "local",
      "command": ["` + filepath.ToSlash(tokless) + `", "run-mcp", "--agent", "opencode", "cmd", "/c", "C:/no/such/Roaming/npm/codegraph.CMD", "serve", "--mcp"],
      "enabled": true
    }
  }
}`
	if err := os.WriteFile(op.Config, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := exec.LookPath("cmd"); err != nil {
		if _, err2 := exec.LookPath("cmd.exe"); err2 != nil {
		}
	}

	issues := probeAgentRuntime("opencode")
	if len(issues) == 0 {
		t.Fatal("expected mcp runtime issue")
	}
	found := false
	for _, is := range issues {
		if is.kind == "mcp" && (strings.Contains(is.detail, "not found") || strings.Contains(is.detail, "broken") || strings.Contains(is.detail, "not runnable")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestDoctorSummaryRuntime(t *testing.T) {
	s := formatRuntimeIssues([]runtimeIssue{
		{kind: "hook", detail: "backslash path"},
		{kind: "mcp", detail: "batch not found"},
		{kind: "hook", detail: "backslash path"}, // dedupe
	})
	if !strings.Contains(s, "backslash path") || !strings.Contains(s, "batch not found") {
		t.Fatalf("format = %q", s)
	}
	if strings.Count(s, "backslash") != 1 {
		t.Fatalf("dedupe failed: %q", s)
	}
}

func TestDoctorReportMarksRuntimeUnwired(t *testing.T) {
	orig := util.IsWin
	defer func() { util.IsWin = orig }()
	util.IsWin = true

	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	cp := util.ClaudeCodePaths()
	if err := os.MkdirAll(cp.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"C:\\Users\\u\\tokless.exe rtk-hook claude"}]}]}}`
	if err := os.WriteFile(cp.Settings, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	issues := probeAgentRuntime("claude")
	r := agentReport{
		label:     "Claude Code",
		installed: true,
		wired:     len(issues) == 0,
		runtime:   issues,
	}
	if r.wired {
		t.Fatal("should not be wired with runtime issues")
	}
	if len(r.runtime) == 0 {
		t.Fatal("expected runtime issues")
	}
}

func TestLiveHookEmptyStdinExit0(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "tokless")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}
	writeExit0(t, fake)
	if detail := liveHook([]string{fake, "rtk-hook", "claude"}); detail != "" {
		t.Fatalf("liveHook: %q", detail)
	}
}

func TestLooksLikeCodegraph(t *testing.T) {
	if !looksLikeCodegraph(`C:\npm\codegraph.CMD`, []string{"serve", "--mcp"}) {
		t.Fatal("expected codegraph.CMD match")
	}
	if !looksLikeCodegraph("cmd", []string{"/c", "C:/x/codegraph.cmd", "serve"}) {
		t.Fatal("expected arg match")
	}
	if looksLikeCodegraph("context-mode", nil) {
		t.Fatal("context-mode is not codegraph")
	}
}

func TestPathCandidates(t *testing.T) {
	c := pathCandidates(`C:\Users\a\tokless.exe`)
	if len(c) < 2 {
		t.Fatalf("candidates = %v", c)
	}
	found := false
	for _, x := range c {
		if strings.Contains(x, "/") {
			found = true
		}
	}
	if !found && runtime.GOOS != "windows" {
		_ = found
	}
}

// writeExit0 writes a tiny executable that exits 0.
func writeExit0(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if strings.HasSuffix(strings.ToLower(path), ".exe") {
			src := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "build", "-o", path, src)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build fake: %v: %s", err, out)
			}
			return
		}
		if err := os.WriteFile(path, []byte("@echo off\r\nexit /b 0\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
