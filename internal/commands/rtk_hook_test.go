package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestShellTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   \t\n", nil},
		{"simple", "git status", []string{"git", "status"}},
		{"double quotes", `find . -name "*.go"`, []string{"find", ".", "-name", "*.go"}},
		{"single quotes", `find . -name '*.go'`, []string{"find", ".", "-name", "*.go"}},
		{"mixed quotes", `find . -name "*.go" -path 'foo bar'`, []string{"find", ".", "-name", "*.go", "-path", "foo bar"}},
		{"backslash escape", `find . -name \*go`, []string{"find", ".", "-name", "*go"}},
		{"literal -not in double quotes", `echo "use -not to filter"`, []string{"echo", "use -not to filter"}},
		{"literal ! in single quotes", `grep '!=foo'`, []string{"grep", "!=foo"}},
		{"tabs and newlines", "find\t.\n -name\t*.go", []string{"find", ".", "-name", "*.go"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shellTokens(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("shellTokens(%q) = %v (len %d); want %v (len %d)", tc.input, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("shellTokens(%q)[%d] = %q; want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestFirstSegment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"single", "git status", "git status"},
		{"and-chain", "git add . && git commit", "git add ."},
		{"or-chain", "git status || echo fail", "git status"},
		{"semicolon", "find . -name foo ; ls", "find . -name foo"},
		{"pipe", "find . -name foo | head", "find . -name foo"},
		{"double pipe", "find . -name foo || head", "find . -name foo"},
		{"quoted pipe", `echo "a | b" && ls`, `echo "a | b"`},
		{"leading whitespace", "  git status", "git status"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := firstSegment(tc.input)
			if got != tc.want {
				t.Errorf("firstSegment(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRtkUnsafeFind(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Bug repro — must return true (passthrough).
		{"find with -not", `find . -name "*.go" -not -path "*/.*"`, true},
		{"find with -exec", `find . -name "*.go" -exec wc -l {} \;`, true},
		{"find with -size", `find . -size +1M`, true},
		{"find with -perm", `find . -perm 644`, true},
		{"find with -print0", `find . -print0`, true},
		{"find with -delete", `find . -name x -delete`, true},
		{"find with -or", `find . -name x -o -name y`, true},
		{"find with -a", `find . -name x -a -type f`, true},
		{"find with !", `find . ! -name "*.test.go"`, true},
		{"find with -regex", `find . -regex ".*\.go"`, true},
		{"find with -mtime", `find . -mtime -1`, true},

		// Compound: bad first segment → unsafe.
		{"bad find then git", `find . -delete; git status`, true},
		{"bad find then git (and)", `find . -exec rm {} \; && git status`, true},

		// Safe — must return false (rewrite allowed).
		{"bare find", `find .`, false},
		{"find -name only", `find . -name "*.go"`, false},
		{"find -name -type", `find . -name "*.go" -type f`, false},
		{"find -name -maxdepth", `find . -name "*.go" -maxdepth 3`, false},
		{"find -iname", `find . -iname "Makefile"`, false},
		{"find -type d", `find . -type d -name node_modules`, false},

		// Quoted literals must not false-positive.
		{"literal -not in filename", `find . -name "*-not-suffix"`, false},
		{"literal ! in single-quoted arg", `find . -name '!=foo'`, false},
		{"echo with -not literal", `echo "use -not to filter"`, false},

		// Non-find commands — never flag.
		{"git status", `git status`, false},
		{"cargo test", `cargo test`, false},
		{"empty", ``, false},
		{"whitespace only", `   `, false},
		{"bash with find inside string", `bash -c 'find . -name "*.go" -not -path x'`, false},

		// Compound: clean find first segment → safe (rtk handles rest).
		{"clean find and git", `find . -name foo && git status`, false},
		{"clean find ; git", `find . -type d; git status`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rtkUnsafeFind(tc.input)
			if got != tc.want {
				t.Errorf("rtkUnsafeFind(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
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
		// User's bug case: must passthrough (not emit broken rtk find).
		{"user bug find -not", `find . -name "*.go" -not -path "*/.*"`, false, true},
		{"find -exec", `find . -name "*.go" -exec wc -l {} \;`, false, true},
		{"find -size", `find . -size +1M`, false, true},
		{"find -delete", `find . -name x -delete`, false, true},
		{"find bare", `find . -name x -delete; git status`, false, true},

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

func TestStripRtkAbsPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"relative unchanged", "rtk git status", "rtk git status"},
		{"unix abs", "/usr/local/bin/rtk git status", "rtk git status"},
		{"unix home abs", "/home/user/.local/bin/rtk git diff", "rtk git diff"},
		{"windows drive", `C:\Users\me\bin\rtk.exe git status`, "rtk git status"},
		{"windows backslash abs", `\Users\me\bin\rtk git status`, "rtk git status"},
		{"unc path", `\\server\share\rtk.exe git diff`, "rtk git diff"},
		{"no space abs no strip", "/usr/local/bin/rtk", "/usr/local/bin/rtk"},
		{"trailing space preserved", "/x/rtk git status ", "rtk git status "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripRtkAbsPath(tc.input)
			if got != tc.want {
				t.Errorf("stripRtkAbsPath(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

// utilHaveRtk returns true if the rtk binary is available on this system.
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

func TestCopilotRtkDecideMixed(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	newCmd, changed, approve := copilotRtkDecide("rtk git log --oneline && git status")
	if !approve {
		t.Fatal("must approve")
	}
	if !changed {
		t.Fatal("must rewrite bare half")
	}
	if strings.Contains(newCmd, "&& git status") && !strings.Contains(newCmd, "&& rtk git status") {
		t.Fatalf("bare git status remained: %q", newCmd)
	}
	_, changed2, approve2 := copilotRtkDecide("rtk git status")
	if !approve2 || changed2 {
		t.Fatalf("pure rtk: approve=%v changed=%v", approve2, changed2)
	}
	_, ch3, ap3 := copilotRtkDecide("git remote -v")
	if ch3 || ap3 {
		t.Fatalf("git remote is not rewrite-supported; must leave bare: changed=%v approve=%v", ch3, ap3)
	}
	new4, changed4, approve4 := copilotRtkDecide("ls -la && git remote -v")
	if !approve4 || !changed4 {
		t.Fatalf("compound: expect rewrite ls half: approve=%v changed=%v", approve4, changed4)
	}
	if !strings.Contains(new4, "rtk ls") && !strings.Contains(new4, "rtk ls -la") {
		if !strings.HasPrefix(strings.TrimSpace(new4), "rtk ") {
			t.Fatalf("expected rtk ls half: %q", new4)
		}
	}
	if strings.Contains(new4, "rtk git remote") {
		t.Fatalf("must not force-prefix unsupported git remote: %q", new4)
	}
	partial := "git remote -v && rtk git branch -a"
	new5, changed5, approve5 := copilotRtkDecide(partial)
	if !approve5 {
		t.Fatal("partial with rtk segment must approve")
	}
	if changed5 && strings.Contains(new5, "rtk git remote") {
		t.Fatalf("must not invent rtk git remote: %q", new5)
	}
}

func TestRtkRewriteBySegmentPreservesOpSpacing(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed")
	}
	in := "git status && rtk git log --oneline"
	out, ok := rtkRewrite(in)
	if !ok {
		t.Fatal("expected rewrite")
	}
	if !strings.Contains(out, "rtk git status") {
		t.Fatalf("bare half not rewritten: %q", out)
	}
	if strings.Contains(out, "&&rtk") || strings.Contains(out, "rtk&&") {
		t.Fatalf("operator spacing crushed: %q", out)
	}
	_, ok2 := rtkRewrite(out)
	if ok2 {
		t.Fatalf("second pass must no-op, got rewrite of %q", out)
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
