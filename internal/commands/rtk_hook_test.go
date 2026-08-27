package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestRunRtkHookGrokSkipsNonBash(t *testing.T) {
	out := runRtkHookInput(t, `{"hookEventName":"pre_tool_use","toolName":"read_file","toolInput":{"command":"git status"}}`, RunRtkHookGrok)
	if out != "" {
		t.Fatalf("want empty for non-bash tool, got %q", out)
	}
	out = runRtkHookInput(t, `{"hookEventName":"pre_tool_use","toolName":"bash","toolInput":{"path":"x"}}`, RunRtkHookGrok)
	if out != "" {
		t.Fatalf("want empty for bash without command, got %q", out)
	}
}

func TestRunRtkHookGrokAllowsAlreadyRewritten(t *testing.T) {
	out := runRtkHookInput(t, `{"hookEventName":"pre_tool_use","toolName":"bash","toolInput":{"command":"rtk git status"}}`, RunRtkHookGrok)
	if out != "" {
		t.Fatalf("want silent passthrough for already-rewritten command, got %q", out)
	}
}

func TestRunRtkHookGrokSkipsUnsafeFind(t *testing.T) {
	installFakeRtk(t, `printf 'rtk %s\n' "$2"`)
	out := runRtkHookInput(t, `{"toolName":"run_terminal_command","toolInput":{"command":"find . -name x -delete; git status"}}`, RunRtkHookGrok)
	if out != "" {
		t.Fatalf("want passthrough for unsafe find command, got %q", out)
	}
}

func TestRtkRewriteFindSafety(t *testing.T) {
	installFakeRtk(t, `printf 'rtk %s\n' "$2"`)
	for _, tc := range []struct {
		name string
		cmd  string
		want bool
	}{
		{"delete", `find . -name x -delete`, false},
		{"exec", `find . -name '*.go' -exec wc -l {} \;`, false},
		{"compound delete", `find . -delete; git status`, false},
		{"safe name", `find . -name '*.go' -type f`, true},
		{"quoted literal", `find . -name '*-delete'`, true},
		{"non-find", `echo "find . -delete"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := rtkRewrite(tc.cmd)
			if changed != tc.want {
				t.Fatalf("rtkRewrite(%q) changed=%v; want %v, output %q", tc.cmd, changed, tc.want, got)
			}
		})
	}
}

func TestRunRtkHookGrokRewrites(t *testing.T) {
	installFakeRtk(t, `case "$2" in
  rtk\ *) printf '%s\n' "$2" ;;
  *) printf 'rtk %s\n' "$2" ;;
esac`)
	for _, toolName := range []string{"bash", "run_terminal_cmd", "run_terminal_command"} {
		out := runRtkHookInput(t, `{"hookEventName":"pre_tool_use","toolName":"`+toolName+`","toolInput":{"command":"git status","cwd":"/tmp"}}`, RunRtkHookGrok)
		var resp struct {
			HookSpecificOutput struct {
				UpdatedInput map[string]any `json:"updatedInput"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			t.Fatalf("json: %v out=%q", err, out)
		}
		cmd, _ := resp.HookSpecificOutput.UpdatedInput["command"].(string)
		if !strings.Contains(cmd, "rtk") || cmd == "git status" {
			t.Fatalf("expected rtk rewrite for %s, got %q", toolName, cmd)
		}
		if resp.HookSpecificOutput.UpdatedInput["cwd"] != "/tmp" {
			t.Fatalf("unrelated toolInput keys must be preserved: %v", resp.HookSpecificOutput.UpdatedInput)
		}
	}
}

func TestRunRtkHookGrokAcceptsSnakeCasePayload(t *testing.T) {
	installFakeRtk(t, `printf 'rtk %s\n' "$2"`)
	out := runRtkHookInput(t, `{"hook_event_name":"pre_tool_use","tool_name":"run_terminal_command","tool_input":{"command":"git status","cwd":"/tmp"}}`, RunRtkHookGrok)
	var resp struct {
		HookSpecificOutput struct {
			UpdatedInput map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if got, _ := resp.HookSpecificOutput.UpdatedInput["command"].(string); !strings.Contains(got, "rtk") || got == "git status" {
		t.Fatalf("expected rtk rewrite, got %q", got)
	}
}

func TestRunRtkHookGrokPassthroughCases(t *testing.T) {
	installFakeRtk(t, `case "$2" in
  rtk\ *) printf '%s\n' "$2" ;;
  *) printf 'rtk %s\n' "$2" ;;
esac`)
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"malformed JSON", "{"},
		{"missing command", `{"toolName":"bash","toolInput":{"cwd":"/tmp"}}`},
		{"non-shell", `{"toolName":"read_file","toolInput":{"command":"git status"}}`},
		{"already rewritten", `{"toolName":"bash","toolInput":{"command":"rtk git status"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := runRtkHookInput(t, tc.payload, RunRtkHookGrok); out != "" {
				t.Fatalf("expected passthrough, got %q", out)
			}
		})
	}
}

func TestRunRtkHookGrokRtkFailurePassesThrough(t *testing.T) {
	installFakeRtk(t, `exit 1`)
	out := runRtkHookInput(t, `{"toolName":"bash","toolInput":{"command":"git status"}}`, RunRtkHookGrok)
	if out != "" {
		t.Fatalf("expected passthrough after rtk failure, got %q", out)
	}
}

func installFakeRtk(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rtk")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'rtk 1.0.0\\n'; exit 0; fi\nif [ \"$1\" != \"rewrite\" ]; then exit 1; fi\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runRtkHookInput(t *testing.T, payload string, hook func() int) string {
	t.Helper()
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	go func() { _, _ = io.WriteString(wIn, payload); _ = wIn.Close() }()
	if code := hook(); code != 0 {
		t.Fatalf("hook exit code %d", code)
	}
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()
	return strings.TrimSpace(buf.String())
}

func TestRunRtkHookAgySkipsNonRtk(t *testing.T) {
	out := runRtkHookInput(t, `{"toolCall":{"name":"run_command","args":{"CommandLine":"true"}}}`, RunRtkHook)
	if out != "" {
		t.Fatalf("want empty for non-rtk, got %q", out)
	}
}

func TestRunRtkHookAgyAllowsAlreadyRewritten(t *testing.T) {
	out := runRtkHookInput(t, `{"toolCall":{"name":"run_command","args":{"CommandLine":"rtk git status"}}}`, RunRtkHook)
	var resp struct {
		Decision  string         `json:"decision"`
		AllowTool bool           `json:"allowTool"`
		Overwrite map[string]any `json:"overwrite"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if resp.Decision != "allow" {
		t.Fatalf("decision=%q want allow", resp.Decision)
	}
	if !resp.AllowTool {
		t.Fatal("allowTool=false")
	}
	if resp.Overwrite["CommandLine"] != "rtk git status" {
		t.Fatalf("CommandLine=%v", resp.Overwrite["CommandLine"])
	}
}

func TestRunRtkHookAgyRewritesWhenPossible(t *testing.T) {
	if util.ResolveRtkBin() == "" {
		t.Skip("rtk not installed")
	}
	out := runRtkHookInput(t, `{"toolCall":{"name":"run_command","args":{"CommandLine":"git status"}}}`, RunRtkHook)
	var resp struct {
		Decision  string         `json:"decision"`
		AllowTool bool           `json:"allowTool"`
		Overwrite map[string]any `json:"overwrite"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if resp.Decision != "allow" {
		t.Fatalf("decision=%q want allow", resp.Decision)
	}
	if !resp.AllowTool {
		t.Fatal("allowTool=false")
	}
	cmd, _ := resp.Overwrite["CommandLine"].(string)
	if !strings.Contains(cmd, "rtk") {
		t.Fatalf("expected rtk rewrite, got %q", cmd)
	}
}

func TestRunRtkHookAgySegmentRewrite(t *testing.T) {
	if util.ResolveRtkBin() == "" {
		t.Skip("rtk not installed")
	}
	out := runRtkHookInput(t, `{"toolCall":{"name":"run_command","args":{"CommandLine":"git status && echo hi"}}}`, RunRtkHook)
	var resp struct {
		Decision  string         `json:"decision"`
		AllowTool bool           `json:"allowTool"`
		Overwrite map[string]any `json:"overwrite"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if resp.Decision != "allow" {
		t.Fatalf("decision=%q want allow", resp.Decision)
	}
	if !resp.AllowTool {
		t.Fatal("allowTool=false")
	}
	cmd, _ := resp.Overwrite["CommandLine"].(string)
	if !strings.Contains(cmd, "rtk") {
		t.Fatalf("expected rtk in chain rewrite, got %q", cmd)
	}
}

func TestRunRtkHookAgyIgnoresNonRunCommand(t *testing.T) {
	out := runRtkHookInput(t, `{"toolCall":{"name":"view_file","args":{"AbsolutePath":"/tmp/x"}}}`, RunRtkHook)
	if out != "" {
		t.Fatalf("want empty for non-run_command, got %q", out)
	}
}

func TestRunRtkHookCodexPreservesToolInputFields(t *testing.T) {
	for _, toolName := range []string{"Bash", "bash"} {
		t.Run(toolName, func(t *testing.T) {
			out := runRtkHookInput(t, `{"tool_name":"`+toolName+`","tool_input":{"command":"true","timeout":15,"description":"keep me"}}`, RunRtkHookCodex)
			var resp struct {
				HookSpecificOutput struct {
					UpdatedInput map[string]any `json:"updatedInput"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal([]byte(out), &resp); err != nil {
				t.Fatalf("bad JSON %q: %v", out, err)
			}
			if got := resp.HookSpecificOutput.UpdatedInput["timeout"]; got != float64(15) {
				t.Errorf("timeout=%v; want 15", got)
			}
			if got := resp.HookSpecificOutput.UpdatedInput["description"]; got != "keep me" {
				t.Errorf("description=%v; want preserved field", got)
			}
		})
	}
}

func TestRunRtkHookDroidPreservesToolInputFields(t *testing.T) {
	out := runRtkHookInput(t, `{"tool_name":"Execute","tool_input":{"command":"true","timeout":15,"description":"keep me"}}`, RunRtkHookDroid)
	var resp struct {
		HookSpecificOutput struct {
			UpdatedInput map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("bad JSON %q: %v", out, err)
	}
	if got := resp.HookSpecificOutput.UpdatedInput["timeout"]; got != float64(15) {
		t.Errorf("timeout=%v; want 15", got)
	}
	if got := resp.HookSpecificOutput.UpdatedInput["description"]; got != "keep me" {
		t.Errorf("description=%v; want preserved field", got)
	}
}

func TestRunRtkHookCursorPreservesToolInputFields(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable fixture")
	}
	binDir := t.TempDir()
	rtk := filepath.Join(binDir, "rtk")
	if err := os.WriteFile(rtk, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 1.0.0; exit 0; fi\necho 'rtk ls'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out := runRtkHookInput(t, `{"tool_name":"Shell","tool_input":{"command":"ls","timeout":15,"description":"keep me"}}`, RunRtkHookCursor)
	var resp struct {
		Permission   string         `json:"permission"`
		UpdatedInput map[string]any `json:"updated_input"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("bad JSON %q: %v", out, err)
	}
	if resp.Permission != "allow" || resp.UpdatedInput["command"] != "rtk ls" {
		t.Fatalf("response = %+v", resp)
	}
	if resp.UpdatedInput["timeout"] != float64(15) || resp.UpdatedInput["description"] != "keep me" {
		t.Fatalf("tool input fields not preserved: %+v", resp.UpdatedInput)
	}
}

// TestRtkRewriteHook is an integration check that exercises rtkRewrite against
// the actual installed rtk binary. Each case asserts the user-visible behavior:
// (1) bad input → empty string + false (passthrough, no broken command emitted)
// (2) good input → rewritten string + true (rtk prefix applied)
func TestRtkRewriteHook(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed; integration test skipped")
	}
	tests := []struct {
		name      string
		input     string
		wantPass  bool // true: must emit a rewritten rtk command
		wantEmpty bool // true: must return empty (passthrough)
	}{
		{"find -not", `find . -name "*.go" -not -path "*/.*"`, false, true},
		{"find -exec", `find . -name "*.go" -exec wc -l {} \;`, false, true},
		{"find -size", `find . -size +1M`, false, true},
		{"find -delete", `find . -name x -delete`, false, true},
		{"compound semicolon", `find . -name x -delete; git status`, false, true},
		{"compound cd", `cd /tmp && git status`, true, false},

		// Sanity: clean input must still rewrite.
		{"clean find", `find . -name "*.go" -type f`, true, false},
		{"clean find -maxdepth", `find . -name "*.go" -maxdepth 3`, true, false},
		{"git status", `git status`, true, false},
		{"cargo test", `cargo test`, true, false},
		{"git log", `git log --oneline -10`, true, false},

		// Quoted literals: git with -not in arg must still rewrite.
		{"git grep literal", `git log --grep=-not`, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := rtkRewrite(tc.input)
			if tc.wantEmpty && (changed || got != "") {
				t.Errorf("rtkRewrite(%q) = (%q, %v); want passthrough (empty, false)", tc.input, got, changed)
			}
			if tc.wantPass && !changed {
				t.Errorf("rtkRewrite(%q) = (%q, %v); want rewrite (non-empty, true)", tc.input, got, changed)
			}
			if tc.wantPass && got == tc.input {
				t.Errorf("rtkRewrite(%q) returned input unchanged; expected rtk-prefixed rewrite", tc.input)
			}
			if tc.wantPass && !containsRtkPrefix(got) {
				t.Errorf("rtkRewrite(%q) = %q; missing 'rtk ' prefix", tc.input, got)
			}
		})
	}
}

func containsRtkPrefix(s string) bool {
	for i := 0; i+4 <= len(s); i++ {
		if s[i:i+4] == "rtk " {
			return true
		}
	}
	return false
}

// TestRtkRewritePipesPassthrough pins upstream parity: rtk rewrite rejects
// piped lines (exit 1), so hooks must pass them through untouched.
func TestRtkRewritePipesPassthrough(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed; integration test skipped")
	}
	for _, cmd := range []string{`git status | head -5`, `git status | grep main | wc -l`} {
		got, changed := rtkRewrite(cmd)
		if changed || got != "" {
			t.Fatalf("pipe %q: want passthrough, got (%q, %v)", cmd, got, changed)
		}
	}
}

// TestRunRtkRewriteContract pins the CLI contract used by generated agent
// extensions: exit 0 + rewritten command on stdout when changed, exit 1 and
// no output when unchanged.
func TestRunRtkRewriteContract(t *testing.T) {
	installFakeRtk(t, `case "$2" in
  rtk\ *) printf '%s\n' "$2" ;;
  *) printf 'rtk %s\n' "$2" ;;
esac`)
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"tokless", "rtk-rewrite", "--", "git", "status"}
	if code := RunRtkRewrite(); code != 0 {
		t.Fatalf("changed: want exit 0, got %d", code)
	}
	os.Args = []string{"tokless", "rtk-rewrite", "--", "rtk", "git", "status"}
	if code := RunRtkRewrite(); code != 1 {
		t.Fatalf("unchanged: want exit 1, got %d", code)
	}
}

func utilHaveRtk() bool {
	return util.ResolveRtkBin() != ""
}

func TestCommandFromToolArgs(t *testing.T) {
	raw, _ := json.Marshal(`{"command":"git status"}`)
	if got := commandFromToolArgs(raw); got != "git status" {
		t.Errorf("string toolArgs: got %q", got)
	}
	raw, _ = json.Marshal(map[string]string{"command": "ls -la"})
	if got := commandFromToolArgs(raw); got != "ls -la" {
		t.Errorf("object toolArgs: got %q", got)
	}
}

func TestRunRtkHookCopilot(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}

	payload := `{"timestamp":1,"cwd":"/tmp","toolName":"bash","toolArgs":"{\"command\":\"git status\"}"}`
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()

	code := RunRtkHookCopilot()
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()

	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("expected rewrite JSON, got empty")
	}
	var resp struct {
		PermissionDecision string            `json:"permissionDecision"`
		ModifiedArgs       map[string]string `json:"modifiedArgs"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("bad JSON %q: %v", out, err)
	}
	if resp.PermissionDecision != "allow" {
		t.Errorf("permissionDecision=%q", resp.PermissionDecision)
	}
	cmd := resp.ModifiedArgs["command"]
	if !strings.HasPrefix(cmd, "rtk ") {
		t.Errorf("modified command missing rtk prefix: %q", cmd)
	}
	if !strings.Contains(cmd, "git") {
		t.Errorf("modified command missing git: %q", cmd)
	}

	// non-shell tool → no-op
	payload2 := `{"toolName":"read","toolArgs":"{\"path\":\"x\"}"}`
	rIn2, wIn2, _ := os.Pipe()
	rOut2, wOut2, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn2, wOut2
	go func() {
		_, _ = io.WriteString(wIn2, payload2)
		_ = wIn2.Close()
	}()
	code2 := RunRtkHookCopilot()
	_ = wOut2.Close()
	var buf2 bytes.Buffer
	_, _ = io.Copy(&buf2, rOut2)
	_ = rIn2.Close()
	if code2 != 0 || strings.TrimSpace(buf2.String()) != "" {
		t.Errorf("non-shell should no-op; code=%d out=%q", code2, buf2.String())
	}

	// VS Code Chat shape (runTerminalCommand + updatedInput)
	payload3 := `{"tool_name":"runTerminalCommand","tool_input":{"command":"git status"}}`
	rIn3, wIn3, _ := os.Pipe()
	rOut3, wOut3, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn3, wOut3
	go func() {
		_, _ = io.WriteString(wIn3, payload3)
		_ = wIn3.Close()
	}()
	code3 := RunRtkHookCopilot()
	_ = wOut3.Close()
	var buf3 bytes.Buffer
	_, _ = io.Copy(&buf3, rOut3)
	_ = rIn3.Close()
	if code3 != 0 {
		t.Fatalf("vscode exit code %d", code3)
	}
	out3 := strings.TrimSpace(buf3.String())
	if out3 == "" {
		t.Fatal("vscode: expected rewrite JSON, got empty")
	}
	var vscode struct {
		HookSpecificOutput struct {
			UpdatedInput map[string]string `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out3), &vscode); err != nil {
		t.Fatalf("vscode bad JSON %q: %v", out3, err)
	}
	if !strings.HasPrefix(vscode.HookSpecificOutput.UpdatedInput["command"], "rtk ") {
		t.Errorf("vscode missing rtk rewrite: %q", out3)
	}

	// Pure already-rtk → allow; may surface same command in modifiedArgs.
	payload4 := `{"toolName":"bash","toolArgs":"{\"command\":\"rtk git log --oneline\"}"}`
	rIn4, wIn4, _ := os.Pipe()
	rOut4, wOut4, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn4, wOut4
	go func() {
		_, _ = io.WriteString(wIn4, payload4)
		_ = wIn4.Close()
	}()
	code4 := RunRtkHookCopilot()
	_ = wOut4.Close()
	var buf4 bytes.Buffer
	_, _ = io.Copy(&buf4, rOut4)
	_ = rIn4.Close()
	if code4 != 0 {
		t.Fatalf("already-rtk exit %d", code4)
	}
	out4 := strings.TrimSpace(buf4.String())
	var already struct {
		PermissionDecision string            `json:"permissionDecision"`
		ModifiedArgs       map[string]string `json:"modifiedArgs"`
	}
	if err := json.Unmarshal([]byte(out4), &already); err != nil {
		t.Fatalf("already-rtk bad JSON %q: %v", out4, err)
	}
	if already.PermissionDecision != "allow" {
		t.Errorf("already-rtk must allow, got %q", already.PermissionDecision)
	}
	if c := already.ModifiedArgs["command"]; c != "" && c != "rtk git log --oneline" {
		t.Errorf("already-rtk must not re-write command, got %q", c)
	}

	// Mixed: model-native rtk + bare git → must still rewrite bare half.
	payloadMixed := `{"toolName":"bash","toolArgs":"{\"command\":\"rtk git log --oneline && git status\"}"}`
	rInM, wInM, _ := os.Pipe()
	rOutM, wOutM, _ := os.Pipe()
	os.Stdin, os.Stdout = rInM, wOutM
	go func() {
		_, _ = io.WriteString(wInM, payloadMixed)
		_ = wInM.Close()
	}()
	codeM := RunRtkHookCopilot()
	_ = wOutM.Close()
	var bufM bytes.Buffer
	_, _ = io.Copy(&bufM, rOutM)
	_ = rInM.Close()
	if codeM != 0 {
		t.Fatalf("mixed-rtk exit %d", codeM)
	}
	outM := strings.TrimSpace(bufM.String())
	var mixed struct {
		PermissionDecision string            `json:"permissionDecision"`
		ModifiedArgs       map[string]string `json:"modifiedArgs"`
	}
	if err := json.Unmarshal([]byte(outM), &mixed); err != nil {
		t.Fatalf("mixed-rtk bad JSON %q: %v", outM, err)
	}
	if mixed.PermissionDecision != "allow" {
		t.Errorf("mixed-rtk must allow, got %q", mixed.PermissionDecision)
	}
	mc := mixed.ModifiedArgs["command"]
	if !strings.Contains(mc, "rtk git status") {
		t.Errorf("mixed-rtk must rewrite bare git status half, got %q", mc)
	}
	if strings.Contains(mc, "&& git status") {
		t.Errorf("mixed-rtk left bare git status: %q", mc)
	}

	// Non-rtk, non-rewritable shell → no-op.
	payload5 := `{"toolName":"bash","toolArgs":"{\"command\":\"true\"}"}`
	rIn5, wIn5, _ := os.Pipe()
	rOut5, wOut5, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn5, wOut5
	go func() {
		_, _ = io.WriteString(wIn5, payload5)
		_ = wIn5.Close()
	}()
	code5 := RunRtkHookCopilot()
	_ = wOut5.Close()
	var buf5 bytes.Buffer
	_, _ = io.Copy(&buf5, rOut5)
	_ = rIn5.Close()
	if code5 != 0 || strings.TrimSpace(buf5.String()) != "" {
		t.Errorf("non-rtk non-rewrite should no-op; code=%d out=%q", code5, buf5.String())
	}
}

func TestIsAlreadyRtk(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"rtk git status", true},
		{"rtk", true},
		{"  rtk ls  ", true},
		{"/usr/local/bin/rtk git status", true},
		{"git status", false},
		{"echo rtk", false},
		{"rtk git log && git status", false},
	}
	for _, tc := range cases {
		if got := isAlreadyRtk(tc.in); got != tc.want {
			t.Errorf("isAlreadyRtk(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestCommandUsesRtk(t *testing.T) {
	if !commandUsesRtk("rtk git status") {
		t.Error("expected true for pure rtk")
	}
	if !commandUsesRtk("cd /tmp && rtk git log") {
		t.Error("expected true for rtk mid-chain")
	}
	if commandUsesRtk("git status && ls") {
		t.Error("expected false for no rtk")
	}
}

func TestRunRtkHookCopilotPreservesDescription(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	payload := `{"toolName":"bash","toolArgs":"{\"command\":\"git status\",\"description\":\"Check git status\"}"}`
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()
	code := RunRtkHookCopilot()
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var resp struct {
		PermissionDecision string         `json:"permissionDecision"`
		ModifiedArgs       map[string]any `json:"modifiedArgs"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v out=%q", err, buf.String())
	}
	if resp.PermissionDecision != "allow" {
		t.Fatalf("want allow, got %q", resp.PermissionDecision)
	}
	cmd, _ := resp.ModifiedArgs["command"].(string)
	if !strings.HasPrefix(cmd, "rtk ") {
		t.Fatalf("want rtk rewrite, got %q", cmd)
	}
	desc, _ := resp.ModifiedArgs["description"].(string)
	if strings.HasPrefix(desc, "rtk") {
		t.Fatalf("description must not be rtk-stamped, got %#v", resp.ModifiedArgs)
	}
}

func TestRunRtkHookCopilotPostQuiet(t *testing.T) {
	payload := `{
		"toolName":"bash",
		"toolArgs":"{\"command\":\"rtk git log --oneline -2\",\"description\":\"rtk · log\"}",
		"toolResult":{"resultType":"success","textResultForLlm":"abc\ndef"}
	}`
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdin, os.Stdout, os.Stderr = rIn, wOut, wErr
	defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()
	code := RunRtkHookCopilot()
	_ = wOut.Close()
	_ = wErr.Close()
	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, rOut)
	_, _ = io.Copy(&errBuf, rErr)
	_ = rIn.Close()
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.TrimSpace(outBuf.String()) != "" {
		t.Fatalf("postToolUse must not emit stdout, got %q", outBuf.String())
	}
	if !strings.Contains(errBuf.String(), "rtk git log") {
		t.Fatalf("stderr missing rtk trace: %q", errBuf.String())
	}
}

func TestRunRtkHookCopilotPostSkipsElided(t *testing.T) {
	payload := `{
		"toolName":"bash",
		"toolArgs":"{\"command\":\"rtk git status\"}",
		"toolResult":{"resultType":"success","textResultForLlm":"[copilot:elided textResultForLlm (100 bytes)]"}
	}`
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdin, os.Stdout, os.Stderr = rIn, wOut, wErr
	defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()
	code := RunRtkHookCopilot()
	_ = wOut.Close()
	_ = wErr.Close()
	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, rOut)
	_, _ = io.Copy(&errBuf, rErr)
	_ = rIn.Close()
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.TrimSpace(outBuf.String()) != "" {
		t.Fatalf("postToolUse must not emit stdout, got %q", outBuf.String())
	}
	if !strings.Contains(errBuf.String(), "rtk git status") {
		t.Fatalf("stderr missing rtk trace: %q", errBuf.String())
	}
}

func TestRunRtkHookCopilotDualFireAlreadyRtk(t *testing.T) {
	payload := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rtk git status"}}`
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()
	code := RunRtkHookCopilot()
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, `"permissionDecision":"allow"`) && !strings.Contains(out, `"permissionDecision": "allow"`) {
		t.Fatalf("dual-fire already-rtk must allow, got %q", out)
	}
	if strings.Contains(out, "rtk rtk ") {
		t.Fatalf("dual-fire must not double-prefix: %q", out)
	}
}

func TestRunRtkHookDroid(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}

	payload := `{"tool_name":"Execute","tool_input":{"command":"git status"}}`
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()

	code := RunRtkHookDroid()
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()

	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("expected rewrite JSON, got empty")
	}
	var resp struct {
		HookSpecificOutput struct {
			HookEventName      string            `json:"hookEventName"`
			PermissionDecision string            `json:"permissionDecision"`
			UpdatedInput       map[string]string `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("bad JSON %q: %v", out, err)
	}
	if resp.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("permissionDecision=%q", resp.HookSpecificOutput.PermissionDecision)
	}
	if resp.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName=%q", resp.HookSpecificOutput.HookEventName)
	}
	cmd := resp.HookSpecificOutput.UpdatedInput["command"]
	if cmd == "" {
		t.Fatal("updatedInput[command] is empty — key not detected")
	}
	if !strings.HasPrefix(cmd, "rtk ") {
		t.Errorf("rewrite missing rtk prefix: %q", cmd)
	}
	if !strings.Contains(cmd, "git") {
		t.Errorf("rewrite missing git: %q", cmd)
	}
	t.Logf("OK: command lowercase rewrite: %q", cmd)
}

func TestRunRtkHookDroidPascalCase(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}

	payload := `{"tool_name":"Execute","tool_input":{"CommandLine":"git log --oneline -5"}}`
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()

	code := RunRtkHookDroid()
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()

	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := strings.TrimSpace(buf.String())
	var resp struct {
		HookSpecificOutput struct {
			PermissionDecision string            `json:"permissionDecision"`
			UpdatedInput       map[string]string `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("bad JSON %q: %v", out, err)
	}
	cmd := resp.HookSpecificOutput.UpdatedInput["CommandLine"]
	if cmd == "" {
		t.Fatal("updatedInput[CommandLine] is empty — key not detected")
	}
	if !strings.HasPrefix(cmd, "rtk ") {
		t.Errorf("rewrite missing rtk prefix: %q", cmd)
	}
	t.Logf("OK PascalCase rewrite: %q", cmd)
}

func TestRunRtkHookDroidNonExecuteNoOp(t *testing.T) {
	payload := `{"tool_name":"Edit","tool_input":{"file_path":"/tmp/x.go","content":"foo"}}`
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()

	code := RunRtkHookDroid()
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()

	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("non-Execute must produce empty output, got %q", buf.String())
	}
}

func TestRunRtkHookDroidUnsupportedPassthrough(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}

	payload := `{"tool_name":"Execute","tool_input":{"command":"echo hello"}}`
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	go func() {
		_, _ = io.WriteString(wIn, payload)
		_ = wIn.Close()
	}()

	code := RunRtkHookDroid()
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()

	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	var resp struct {
		HookSpecificOutput struct {
			PermissionDecision string            `json:"permissionDecision"`
			UpdatedInput       map[string]string `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	cmd := resp.HookSpecificOutput.UpdatedInput["command"]
	if strings.Contains(cmd, "rtk ") {
		t.Errorf("unsupported command must not be rewritten, got %q", cmd)
	}
	if resp.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("must allow unsupported command, got %q", resp.HookSpecificOutput.PermissionDecision)
	}
	t.Logf("OK passthrough: %q", cmd)
}

func runRtkHookClinePayload(t *testing.T, payload string) (int, string) {
	t.Helper()
	oldIn, oldOut := os.Stdin, os.Stdout
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	os.Stdin, os.Stdout = rIn, wOut
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	go func() { _, _ = io.WriteString(wIn, payload); _ = wIn.Close() }()
	code := RunRtkHookCline()
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	_ = rIn.Close()
	return code, strings.TrimSpace(buf.String())
}

func TestRunRtkHookCline(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	payload := `{"hookName":"tool_call","tool_call":{"id":"1","name":"execute_command","input":{"command":"git status"}}}`
	code, out := runRtkHookClinePayload(t, payload)
	if code != 0 || out == "" || out == "{}" {
		t.Fatalf("expected overrideInput JSON, code=%d out=%q", code, out)
	}
	var resp struct {
		OverrideInput map[string]string `json:"overrideInput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.OverrideInput["command"], "rtk ") {
		t.Fatalf("Cline command not rewritten: %q", out)
	}
}

func TestRunRtkHookClineCurrentPreToolUseCompatibility(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	_, out := runRtkHookClinePayload(t, `{"preToolUse":{"toolName":"execute_command","parameters":{"command":"git diff"}}}`)
	if !strings.Contains(out, `"command":"rtk git diff"`) {
		t.Fatalf("current CLI preToolUse compatibility not rewritten: %q", out)
	}
}

func TestRunRtkHookClineLegacyHookInputWrapperNoOp(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	_, out := runRtkHookClinePayload(t, `{"hookInput":{"tool_call":{"name":"execute_command","input":{"command":"git diff"}}}}`)
	if out != "{}" {
		t.Fatalf("legacy hookInput wrapper must no-op: %q", out)
	}
}

func TestRunRtkHookClineStructuredCommandNoOp(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	_, out := runRtkHookClinePayload(t, `{"tool_call":{"name":"run_commands","input":{"commands":[{"command":"git","args":["status","--short"]}]}}}`)
	if out != "{}" {
		t.Fatalf("structured command must pass through unchanged: %q", out)
	}
}

func TestRunRtkHookClinePreservesRtkRewrite(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	_, out := runRtkHookClinePayload(t, `{"workspaceRoots":["/home/hoangp8/tokless"],"tool_call":{"name":"run_commands","input":{"commands":["git -C /home/hoangp8/tokless diff","git -C /tmp diff"]}}}`)
	var resp struct {
		OverrideInput map[string]any `json:"overrideInput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	commands := resp.OverrideInput["commands"].([]any)
	if commands[0] != "rtk git -C /home/hoangp8/tokless diff" {
		t.Fatalf("RTK rewrite changed by Cline adapter: %q", out)
	}
	if !strings.Contains(commands[1].(string), "git -C /tmp") {
		t.Fatalf("different -C path changed: %q", out)
	}
}

func TestRunRtkHookClineCommandsStringArray(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}

	payload := `{"tool_call":{"name":"run_commands","input":{"commands":["git status --short","git diff"]}}}`
	_, out := runRtkHookClinePayload(t, payload)
	var resp struct {
		OverrideInput map[string]any `json:"overrideInput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	commands, ok := resp.OverrideInput["commands"].([]any)
	if !ok || len(commands) != 2 {
		t.Fatalf("commands array not returned: %q", out)
	}
	for i, raw := range commands {
		command, ok := raw.(string)
		if !ok || !strings.HasPrefix(command, "rtk ") {
			t.Fatalf("command %d not rewritten: %q", i, out)
		}
	}
}

func TestRunRtkHookClineCommandsArrayNoOp(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "already rtk",
			payload: `{"tool_call":{"name":"run_commands","input":{"commands":["rtk git status"]}}}`,
		},
		{
			name:    "non shell",
			payload: `{"tool_call":{"name":"read_file","input":{"commands":[{"command":"git status"}]}}}`,
		},
		{
			name:    "non command item",
			payload: `{"tool_call":{"name":"run_commands","input":{"commands":[{"label":"missing command","value":true}]}}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, out := runRtkHookClinePayload(t, tc.payload)
			if out != "{}" {
				t.Fatalf("got %q; want {}", out)
			}
		})
	}
}

func TestRunRtkHookClineUnsupportedCommandPassthrough(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	unsupported := []string{
		"npm test",
		"echo hello",
		"cd /tmp",
		"sleep 1",
		"mkdir -p /tmp/x",
		"rm -rf /tmp/x",
		"export FOO=1",
		"apt-get update",
		"node --version",
		"undefinedcommandxyz",
	}
	for _, cmd := range unsupported {
		t.Run(cmd, func(t *testing.T) {
			payload := `{"tool_call":{"name":"execute_command","input":{"command":` +
				strconv.Quote(cmd) + `}}}`
			_, out := runRtkHookClinePayload(t, payload)
			if out != "{}" {
				t.Fatalf("unsupported command %q: got %q; want {}", cmd, out)
			}
		})
	}
}

func TestRunRtkHookClineCommandsArrayUnsupportedPreserved(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	payload := `{"tool_call":{"name":"run_commands","input":{"commands":["npm test","echo hello","sleep 1"]}}}`
	_, out := runRtkHookClinePayload(t, payload)
	if out != "{}" {
		t.Fatalf("all-unsupported array: got %q; want {}", out)
	}

	payload = `{"tool_call":{"name":"run_commands","input":{"commands":["git status","npm test","git diff"]}}}`
	_, out = runRtkHookClinePayload(t, payload)
	var resp struct {
		OverrideInput map[string]any `json:"overrideInput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("mixed array: bad JSON %q: %v", out, err)
	}
	commands, ok := resp.OverrideInput["commands"].([]any)
	if !ok || len(commands) != 3 {
		t.Fatalf("mixed array: commands missing: %q", out)
	}
	if commands[0] != "rtk git status" || commands[1] != "npm test" || commands[2] != "rtk git diff" {
		t.Fatalf("mixed array: want [rtk git status, npm test, rtk git diff], got %q", commands)
	}
}
