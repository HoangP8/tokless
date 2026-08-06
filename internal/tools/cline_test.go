package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func clineToolProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return root
}

func clineTestEnv(t *testing.T) util.ClinePaths {
	t.Helper()
	t.Setenv("CLINE_DIR", filepath.Join(t.TempDir(), "cline"))
	t.Setenv("CLINE_DATA_DIR", "")
	t.Setenv("CLINE_MCP_SETTINGS_PATH", "")
	t.Setenv("TOKLESS_TEST", "1")
	return util.ClinePathsResolved()
}

func TestClineContextAndCodegraphWireVerify(t *testing.T) {
	clineToolProject(t)
	p := clineTestEnv(t)
	binDir := t.TempDir()
	kiloExecutableFixture(t, binDir, "codegraph", "codegraph")
	t.Setenv("PATH", binDir)

	ctx := contextMode.WireFor["cline"]
	if ctx == nil {
		t.Fatal("cline missing from context-mode WireFor")
	}
	if ok, err := ctx(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("context Cline wire = %v, %v", ok, err)
	}
	spawn := util.PickMcpSpawn("context-mode")
	expected := append([]string{spawn.Command}, spawn.Args...)
	joined := strings.Join(expected, " ")
	for _, want := range []string{"run-mcp", "--context-mode"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("cline context argv not bounded via context-mode proxy: %#v", expected)
		}
	}
	if !HasOwner("cline", "context-mode") {
		t.Fatal("context owner block missing from cline instructions")
	}

	cg := codegraph.WireFor["cline"]
	if cg == nil {
		t.Fatal("cline missing from codegraph WireFor")
	}
	if ok, err := cg(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("codegraph Cline wire = %v, %v", ok, err)
	}
	expectedCG := util.WrapAutoIndex("cline", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	cgArgv := append([]string{expectedCG.Command}, expectedCG.Args...)
	cgJoined := strings.Join(cgArgv, " ")
	for _, want := range []string{"run-mcp", "--agent", "cline", "codegraph", "serve", "--mcp"} {
		if !strings.Contains(cgJoined, want) {
			t.Fatalf("cline codegraph argv missing %q: %#v", want, cgArgv)
		}
	}
	if !codegraphVerify("cline") || !agents.ClineMcpMatches("codegraph", cgArgv) {
		t.Fatal("codegraph Cline exact verify failed")
	}

	instructions, err := os.ReadFile(agents.ClineInstructionsPath())
	if err != nil || strings.Contains(string(instructions), "run-mcp") {
		t.Fatal("MCP command leaked into cline instructions")
	}
	if !strings.Contains(string(instructions), "## Context Tools (context-mode)") || !strings.Contains(string(instructions), "## Code Index (codegraph)") {
		t.Fatalf("Cline AGENTS.md missing owners: %s", instructions)
	}
	if raw, _ := util.ReadFileSafe(p.McpConfig); !strings.Contains(raw, `"context-mode"`) || !strings.Contains(raw, `"codegraph"`) {
		t.Fatalf("Cline MCP settings entries missing: %s", raw)
	}

	// Unwire removes entries and ownership.
	if ok, err := contextMode.UnwireFor["cline"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("context Cline unwire = %v, %v", ok, err)
	}
	if ok, err := codegraph.UnwireFor["cline"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("codegraph Cline unwire = %v, %v", ok, err)
	}
	raw, _ := util.ReadFileSafe(p.McpConfig)
	if strings.Contains(raw, `"context-mode"`) || strings.Contains(raw, `"codegraph"`) {
		t.Fatalf("Cline MCP entries survived unwire: %s", raw)
	}
	if HasOwner("cline", "context-mode") || HasOwner("cline", "codegraph") {
		t.Fatal("owner blocks survived unwire")
	}
}

func TestClineRtkHookWireUnwire(t *testing.T) {
	clineToolProject(t)
	p := clineTestEnv(t)
	binDir := t.TempDir()
	kiloExecutableFixture(t, binDir, "tokless", "rtk-rewrite")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	hookPath := filepath.Join(p.HooksDir, "PreToolUse")
	if util.IsWin {
		hookPath = filepath.Join(p.HooksDir, "PreToolUse.ps1")
	}
	foreign := "#!/bin/sh\necho foreign\n"
	if err := util.WriteFile(hookPath, foreign); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := rtk.WireFor["cline"](core.RunOpts{}); err != nil || ok {
		t.Fatalf("Cline RTK foreign wire = %v, %v; want false, nil", ok, err)
	}
	preserved, err := os.ReadFile(hookPath)
	if err != nil || string(preserved) != string(original) {
		t.Fatalf("foreign hook changed: %q, %v", preserved, err)
	}
	if _, err := os.Stat(hookPath + ".foreign-backup"); !os.IsNotExist(err) {
		t.Fatalf("foreign hook backup unexpectedly created: %v", err)
	}

	if err := os.Remove(hookPath); err != nil {
		t.Fatal(err)
	}
	if ok, err := rtk.WireFor["cline"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Cline RTK managed wire = %v, %v", ok, err)
	}
	managed, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := rtk.WireFor["cline"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Cline RTK idempotent wire = %v, %v", ok, err)
	}
	rewired, err := os.ReadFile(hookPath)
	if err != nil || string(rewired) != string(managed) {
		t.Fatalf("managed hook changed on idempotent wire: %q, %v", rewired, err)
	}
	if ok, err := rtk.UnwireFor["cline"](core.RunOpts{}); err != nil || !ok || clineRtkVerify() {
		t.Fatalf("Cline RTK unwire = %v, %v", ok, err)
	}
}

// buildClineSimBinary builds the real tokless binary into a dir with a space in the path.
func buildClineSimBinary(t *testing.T) string {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "tokless bin with spaces")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "tokless")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", bin, "./cmd/tokless")
	build.Dir = root
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GOCACHE=") {
			env = append(env, e)
		}
	}
	build.Env = append(env, "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("go build tokless: %v\n%s", err, out)
	}
	return bin
}

// runClineHookScript executes the generated hook like Cline's SDK does: script on path, payload JSON on stdin, control JSON out.
func runClineHookScript(t *testing.T, hookPath, payload string) string {
	t.Helper()
	cmd := exec.Command("sh", hookPath)
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook script %s failed: %v\n%s", hookPath, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fakeRtk writes a hermetic rtk shim so hook rewriting is deterministic
// without depending on a real installed rtk binary (CI runners have none).
// It must satisfy BinaryHealthy (--version → exit 0, output with a dot) and
// implement `rtk rewrite` for the exact commands these tests send.
func fakeRtk(t *testing.T, dir string) {
	t.Helper()
	body := `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'rtk 1.0\n'; exit 0; fi
if [ "$1" = "rewrite" ]; then
  shift
  case "$*" in
    "git status") printf 'rtk git status'; exit 0;;
    "git diff") printf 'rtk git diff'; exit 0;;
    "cd /tmp && git status") printf 'cd /tmp && rtk git status'; exit 0;;
  esac
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "rtk"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestClineRtkHookEndToEnd(t *testing.T) {
	if util.IsWin {
		t.Skip("sh simulation only; ps1 covered by content test")
	}
	bin := buildClineSimBinary(t)
	fakeRtk(t, filepath.Dir(bin))
	// The hook script embeds this binary's quoted path; ensure tokless on PATH
	// is the one we built so the simulation exercises current code.
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	clineToolProject(t)
	p := clineTestEnv(t)
	if ok, err := rtk.WireFor["cline"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Cline RTK wire = %v, %v", ok, err)
	}
	hookPath := filepath.Join(p.HooksDir, "PreToolUse")
	raw, ok := util.ReadFileSafe(hookPath)
	if !ok {
		t.Fatal("hook not written")
	}
	if !strings.Contains(raw, "'"+bin+"'") {
		t.Fatalf("hook must embed quoted abs binary path %q, got:\n%s", bin, raw)
	}
	if strings.Contains(raw, "exec tokless ") && !strings.Contains(raw, "exec '") {
		t.Fatal("hook fell back to bare tokless instead of quoted path")
	}
	if !clineRtkVerify() {
		t.Fatal("verify failed after wire")
	}

	// Cline SDK payload: tool_call name + input.
	payload := `{"hookName":"tool_call","tool_call":{"id":"1","name":"execute_command","input":{"command":"git status","cwd":"/tmp"}}}`
	out := runClineHookScript(t, hookPath, payload)
	if !strings.HasPrefix(out, `{"overrideInput":`) {
		t.Fatalf("expected overrideInput response, got %q", out)
	}
	if !strings.Contains(out, `"command":"rtk git status"`) || !strings.Contains(out, `"cwd":"/tmp"`) {
		t.Fatalf("command not rewritten or fields lost: %q", out)
	}

	// Legacy preToolUse shape still works.
	legacy := `{"hookName":"tool_call","preToolUse":{"toolName":"execute_command","parameters":{"command":"git diff"}}}`
	if out := runClineHookScript(t, hookPath, legacy); !strings.Contains(out, `"command":"rtk git diff"`) {
		t.Fatalf("legacy shape not rewritten: %q", out)
	}

	// Commands-array shape preserved.
	arr := `{"hookName":"tool_call","tool_call":{"id":"2","name":"run_commands","input":{"cwd":"/tmp","timeout":30,"commands":[{"command":"cd /tmp && git status","label":"display only","extra":"one"}]}}}`
	out = runClineHookScript(t, hookPath, arr)
	var response struct {
		OverrideInput struct {
			Commands []any `json:"commands"`
		} `json:"overrideInput"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatal(err)
	}
	command, ok := response.OverrideInput.Commands[0].(string)
	if !ok || command != "cd /tmp && rtk git status" {
		t.Fatalf("commands array not normalized and rewritten: %q", out)
	}

	// Non-shell tool passthrough.
	if out := runClineHookScript(t, hookPath, `{"hookName":"tool_call","tool_call":{"id":"3","name":"read_file","input":{"filePath":"x"}}}`); out != "{}" {
		t.Fatalf("non-shell tool expected {}, got %q", out)
	}
}

func TestClineRtkHookWindowsShimContent(t *testing.T) {
	old := util.IsWin
	util.IsWin = true
	defer func() { util.IsWin = old }()
	exe := `C:\Program Files\tokless\tokless.exe`
	script := clineRtkHookScript(exe)
	if strings.Contains(script, ".ps1") || strings.Contains(script, "pwsh") ||
		strings.Contains(script, "[Console]::") {
		t.Fatalf("windows hook must not depend on PowerShell, got:\n%s", script)
	}
	if !strings.Contains(script, strconv.Quote(exe)) {
		t.Fatalf("windows shim must embed JSON-quoted abs path, got:\n%s", script)
	}
	if !strings.Contains(script, `stdio: "inherit"`) {
		t.Fatalf("windows shim must pass stdio through, got:\n%s", script)
	}
	if !strings.Contains(script, "rtk-hook") || !strings.Contains(script, `"cline"`) ||
		!strings.Contains(script, clineRtkMarker) {
		t.Fatalf("windows shim missing marker/args:\n%s", script)
	}
	if want := filepath.Base(clineRtkHookPath()); want != "PreToolUse.cjs" {
		t.Fatalf("windows hook filename = %q, want PreToolUse.cjs", want)
	}
}

// TestClineRtkHookWindowsShimRuns executes the generated Windows shim with the
// real node runtime, proving it forwards stdin and emits Cline's control JSON.
func TestClineRtkHookWindowsShimRuns(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	bin := buildClineSimBinary(t)
	fakeRtk(t, filepath.Dir(bin))

	old := util.IsWin
	util.IsWin = true
	script := clineRtkHookScript(bin)
	util.IsWin = old

	shim := filepath.Join(t.TempDir(), "PreToolUse.cjs")
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, shim)
	cmd.Stdin = strings.NewReader(`{"hookName":"tool_call","tool_call":{"id":"1","name":"execute_command","input":{"command":"git status"}}}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node shim failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); !strings.Contains(got, `"overrideInput"`) ||
		!strings.Contains(got, `"command":"rtk git status"`) {
		t.Fatalf("node shim output = %q", got)
	}
}
