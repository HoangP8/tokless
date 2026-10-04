package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HoangP8/tokless/internal/tools"
)

func TestMcpChildEnvPassThrough(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/root", "TERM=xterm-256color", "NO_COLOR="}

	// Env passed through unchanged regardless of CLAUDECODE
	t.Run("claude_code", func(t *testing.T) {
		t.Setenv("CLAUDECODE", "1")
		got := mcpChildEnv(base)
		if len(got) != len(base) {
			t.Fatalf("env should be unchanged:\n got=%v\n want=%v", got, base)
		}
		for i := range got {
			if got[i] != base[i] {
				t.Fatalf("env[%d] changed: got %q, want %q", i, got[i], base[i])
			}
		}
	})

	t.Run("non_claude", func(t *testing.T) {
		got := mcpChildEnv(base)
		if len(got) != len(base) {
			t.Fatalf("env should be unchanged:\n got=%v\n want=%v", got, base)
		}
		for i := range got {
			if got[i] != base[i] {
				t.Fatalf("env[%d] changed: got %q, want %q", i, got[i], base[i])
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := mcpChildEnv(nil)
		if got != nil {
			t.Fatalf("nil env should return nil, got %v", got)
		}
	})
}

func TestNormalizeCmdBatchArgs(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    []string
	}{
		{"cmd", "cmd", []string{"/c", "C:/Users/user/AppData/Roaming/npm/codegraph.CMD", "serve"}, []string{"/c", `C:\Users\user\AppData\Roaming\npm\codegraph.CMD`, "serve"}},
		{"cmd exe upper C", `C:\Windows\System32\cmd.exe`, []string{"/C", "C:/tools/codegraph.bat"}, []string{"/C", `C:\tools\codegraph.bat`}},
		{"non cmd", "powershell", []string{"/c", "C:/tools/codegraph.cmd"}, []string{"/c", "C:/tools/codegraph.cmd"}},
		{"non batch", "cmd", []string{"/c", "echo", "C:/keep/slashes"}, []string{"/c", "echo", "C:/keep/slashes"}},
		{"short", "cmd", []string{"/c"}, []string{"/c"}},
		{"npx cmd", "cmd", []string{"/c", "C:/Users/user/AppData/Roaming/npm/npx.cmd", "serve"}, []string{"/c", `C:\Users\user\AppData\Roaming\npm\npx.cmd`, "serve"}},
		{"npx bat", "cmd", []string{"/c", "C:/Users/user/AppData/Roaming/npm/npx.bat", "serve"}, []string{"/c", `C:\Users\user\AppData\Roaming\npm\npx.bat`, "serve"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.args...)
			got := normalizedCmdBatchArgs(tt.command, tt.args, true)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("args = %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(tt.args, original) {
				t.Fatalf("input mutated: got %q, want %q", tt.args, original)
			}
		})
	}
}

func TestNormalizedCmdBatchArgsNonWindows(t *testing.T) {
	for _, args := range [][]string{
		{"/c", "C:/tools/codegraph.cmd"},
		{"/c", "C:/Users/user/AppData/Roaming/npm/npx.cmd", "serve"},
		{"/c", "C:/Users/user/AppData/Roaming/npm/npx.bat", "serve"},
	} {
		if got := normalizedCmdBatchArgs("cmd.exe", args, false); !reflect.DeepEqual(got, args) {
			t.Fatalf("non-Windows args changed: %q", got)
		}
	}
}

func TestIsNodeShebangScript(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"env node", "#!/usr/bin/env node\n", true},
		{"usr bin node", "#!/usr/bin/node\n", true},
		{"env -S node", "#!/usr/bin/env -S node\n", true},
		{"sh shebang", "#!/bin/sh\n", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "script")
			if err := os.WriteFile(f, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := isNodeShebangScript(f); got != tt.want {
				t.Fatalf("isNodeShebangScript(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestResolveMcpCommandDoesNotMutateArgv(t *testing.T) {
	argv := []string{"cmd", "/c", "C:/tools/codegraph.cmd", "serve"}
	original := append([]string(nil), argv...)
	_, _ = resolveMcpCommand("cmd", argv)
	if !reflect.DeepEqual(argv, original) {
		t.Fatalf("argv mutated: got %q, want %q", argv, original)
	}
}

func TestResolveMcpCommandShortArgvNoPanic(t *testing.T) {
	_, args := resolveMcpCommand("cmd", []string{"cmd"})
	if len(args) != 0 {
		t.Fatalf("expected empty args, got %v", args)
	}
}

func TestBoundedContextModeProxyFiltersToolsAndForwardsCalls(t *testing.T) {
	dir := t.TempDir()
	upstream := filepath.Join(dir, "upstream.go")
	source := `package main
import ("bufio"; "encoding/json"; "os")
func main() {
 s := bufio.NewScanner(os.Stdin)
 for s.Scan() {
  var r map[string]json.RawMessage; json.Unmarshal(s.Bytes(), &r)
  var method string; json.Unmarshal(r["method"], &method)
  if method == "tools/list" { os.Stdout.Write([]byte(` + "`" + `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"ctx_search"},{"name":"hidden"},{"name":"ctx_execute"},{"name":"ctx_batch_execute"},{"name":"ctx_execute_file"},{"name":"ctx_index"},{"name":"ctx_fetch_and_index"}]}}` + "`" + `+"\n")) } else if method == "tools/call" { os.Stdout.Write([]byte(` + "`" + `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"forwarded"}]}}` + "`" + `+"\n")) }
 }
}`
	if err := os.WriteFile(upstream, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"ctx_search\"}}\n")
	var output bytes.Buffer
	if code := runMcpProxyIO("", "go", []string{"go", "run", upstream}, nil, input, &output, io.Discard, "context-mode"); code != 0 {
		t.Fatalf("proxy exit code = %d", code)
	}
	var responses []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var response map[string]any
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 2 {
		t.Fatalf("responses = %d, want 2: %s", len(responses), output.String())
	}
	result := responses[0]["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != len(contextModeTools) {
		t.Fatalf("tools = %v, want %v", tools, contextModeTools)
	}
	for i, tool := range tools {
		if got := tool.(map[string]any)["name"]; got != contextModeTools[i] {
			t.Fatalf("tool %d = %q, want %q", i, got, contextModeTools[i])
		}
	}
	content := responses[1]["result"].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "forwarded" {
		t.Fatalf("allowed tools/call was not forwarded: %s", output.String())
	}
}

func TestBoundedPolicyFiltersAndRejectsHiddenTools(t *testing.T) {
	allowed := mcpToolPolicies["context-mode"]
	ids := &mcpRequestIDs{}
	var upstream, output bytes.Buffer
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ctx_execute"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ctx_hidden_tool"}}`,
	}, "\n") + "\n"
	if err := scanMcpInput(strings.NewReader(input), &upstream, &output, ids, allowed); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(upstream.String(), "tools/call"); got != 1 || strings.Contains(upstream.String(), "ctx_hidden_tool") {
		t.Fatalf("hidden call reached upstream: %q", upstream.String())
	}
	if !strings.Contains(output.String(), `"id":3`) || !strings.Contains(output.String(), `"code":-32601`) || !strings.Contains(output.String(), `tool \"ctx_hidden_tool\" is not available`) {
		t.Fatalf("unexpected denied response: %q", output.String())
	}
	output.Reset()
	if err := scanMcpInput(strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"ctx_hidden_tool"}}`+"\n"), &upstream, &output, ids, allowed); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("denied notification produced response: %q", output.String())
	}

	output.Reset()
	response := `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"ctx_execute"},{"name":"ctx_hidden_tool"},{"name":"ctx_index"}]}}` + "\n"
	if err := scanMcpOutput(strings.NewReader(response), &output, ids, allowed, nil); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "ctx_hidden_tool") || !strings.Contains(got, "ctx_execute") || !strings.Contains(got, "ctx_index") {
		t.Fatalf("list not correctly filtered: %q", got)
	}
}

func TestUnboundedProxyPassesThroughTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh fixture")
	}
	script := filepath.Join(t.TempDir(), "upstream")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nread line\nprintf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[{\"name\":\"headroom_stats\"}]}}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := runMcpProxyIO("", script, []string{script}, nil, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"), &output, io.Discard, ""); code != 0 {
		t.Fatalf("proxy exit code = %d", code)
	}
	if !strings.Contains(output.String(), "headroom_stats") {
		t.Fatalf("unbounded proxy filtered upstream tools: %q", output.String())
	}
}

func TestMcpProxyAfterStartRunsOnceOnlyAfterSuccessfulStart(t *testing.T) {
	successPath := "sh"
	successArgv := []string{"sh", "-c", "exit 0"}
	if runtime.GOOS == "windows" {
		successPath = "cmd"
		successArgv = []string{"cmd", "/c", "exit 0"}
	}
	tests := []struct {
		name    string
		tool    string
		path    string
		argv    []string
		wantRun bool
	}{
		{"unbounded success", "", successPath, successArgv, true},
		{"unbounded failure", "", filepath.Join(t.TempDir(), "missing"), []string{"missing"}, false},
		{"bounded success", "context-mode", successPath, successArgv, true},
		{"bounded failure", "context-mode", filepath.Join(t.TempDir(), "missing"), []string{"missing"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			code := runMcpProxyIOAfterStart("", tt.path, tt.argv, nil, strings.NewReader(""), io.Discard, io.Discard, tt.tool, func() {
				calls++
			})
			if tt.wantRun && code != 0 {
				t.Fatalf("proxy exit code = %d, want 0", code)
			}
			if !tt.wantRun && code == 0 {
				t.Fatal("proxy unexpectedly succeeded")
			}
			wantCalls := 0
			if tt.wantRun {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("afterStart calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestBoundedProxyOutputDoesNotDeadlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh fixture")
	}
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	var output bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runMcpProxyIO("", "sh", []string{"sh", "-c", "printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}'"}, nil, inputReader, &output, io.Discard, "context-mode")
	}()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("proxy exit code = %d", code)
		}
		if got := strings.TrimSpace(output.String()); got != `{"jsonrpc":"2.0","id":1,"result":{}}` {
			t.Fatalf("output = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bounded proxy deadlocked while forwarding child output")
	}
}

func TestScanMcpInputSupportsLargeNDJSONMessage(t *testing.T) {
	message := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ctx_search","arguments":{"query":"` + strings.Repeat("x", 70<<10) + `"}}}`
	var upstream, output bytes.Buffer
	if err := scanMcpInput(strings.NewReader(message+"\n"), &upstream, &output, &mcpRequestIDs{}, contextModeTools); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(upstream.String()); got != message {
		t.Fatalf("upstream message changed or truncated: got %d bytes, want %d", len(got), len(message))
	}
}

func TestScanMcpInputRejectsHiddenToolLocally(t *testing.T) {
	request := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"hidden"}}`
	var upstream, output bytes.Buffer
	if err := scanMcpInput(strings.NewReader(request+"\n"), &upstream, &output, &mcpRequestIDs{}, contextModeTools); err != nil {
		t.Fatal(err)
	}
	if upstream.Len() != 0 {
		t.Fatalf("hidden tool forwarded: %q", upstream.String())
	}
	var response struct {
		ID    int `json:"id"`
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != 7 || response.Error.Code != -32601 {
		t.Fatalf("unexpected local error: %s", output.String())
	}
}

func TestScanMcpInputRejectsBatchFailClosed(t *testing.T) {
	batch := `[{"jsonrpc":"2.0","id":1,"method":"tools/list"},{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hidden"}}]`
	var upstream, output bytes.Buffer
	if err := scanMcpInput(strings.NewReader(batch+"\n"), &upstream, &output, &mcpRequestIDs{}, contextModeTools); err != nil {
		t.Fatal(err)
	}
	if upstream.Len() != 0 {
		t.Fatalf("batch reached upstream: %q", upstream.String())
	}
	var response struct {
		ID     any `json:"id"`
		Result any `json:"result"`
		Error  struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != nil || response.Result != nil || response.Error.Code != -32600 {
		t.Fatalf("unexpected batch error: %s", output.String())
	}
}

func TestToolsListIsAlwaysShapeFiltered(t *testing.T) {
	allowed := contextModeTools
	ids := &mcpRequestIDs{}
	var upstream, discarded bytes.Buffer
	input := "{\"jsonrpc\":\"2.0\",\"id\": 1,\"method\":\"tools/list\"}\n"
	if err := scanMcpInput(strings.NewReader(input), &upstream, &discarded, ids, allowed); err != nil {
		t.Fatal(err)
	}
	response := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"hidden"}]}}`)
	if !filterMcpTools(response) {
		t.Fatal("tools/list-shaped response was not detected for filtering")
	}
	filtered := filterMcpToolsResponse(response, allowed)
	if bytes.Contains(filtered, []byte(`"name":"hidden"`)) {
		t.Fatal("hidden tool survived shape-based filtering")
	}
	// Replayed / unrecorded id: still shape-filtered (fail-closed to allowlist).
	responseNullID := []byte(`{"jsonrpc":"2.0","id":null,"result":{"tools":[{"name":"hidden"},{"name":"ctx_search"}]}}`)
	if !filterMcpTools(responseNullID) {
		t.Fatal("null-id tools/list-shaped response was not detected for filtering")
	}
	filteredNull := filterMcpToolsResponse(responseNullID, allowed)
	if bytes.Contains(filteredNull, []byte(`"name":"hidden"`)) || !bytes.Contains(filteredNull, []byte(`"name":"ctx_search"`)) {
		t.Fatalf("null-id tools/list not filtered to allowlist: %s", filteredNull)
	}
}

func TestMCPBothInitializeResponsesStayRewritten(t *testing.T) {
	ids := &mcpRequestIDs{}
	var upstream, discarded bytes.Buffer
	input := `{"jsonrpc":"2.0","id":"a","method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":"b","method":"initialize","params":{}}` + "\n"
	if err := scanMcpInput(strings.NewReader(input), &upstream, &discarded, ids, projectmemTools); err != nil {
		t.Fatal(err)
	}
	responses := `{"jsonrpc":"2.0","id":"a","result":{"protocolVersion":"2024-11-05","instructions":"upstream text"}}` + "\n" +
		`{"jsonrpc":"2.0","id":"b","result":{"protocolVersion":"2024-11-05","instructions":"upstream text"}}` + "\n"
	var output bytes.Buffer
	if err := scanMcpOutput(strings.NewReader(responses), &output, ids, projectmemTools, func() string { return "rewritten instructions" }); err != nil {
		t.Fatal(err)
	}
	raw := output.String()
	if strings.Contains(raw, "upstream text") || strings.Count(raw, "rewritten instructions") != 2 {
		t.Fatalf("pipelined initialize responses not both rewritten: %s", raw)
	}
}

func TestToolsListResponseIsShapeFiltered(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{"numeric normalization", `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"hidden"},{"name":"ctx_search"}]}}`},
		{"escaped string", `{"jsonrpc":"2.0","id":"\u0061","result":{"tools":[{"name":"hidden"},{"name":"ctx_search"}]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !filterMcpTools([]byte(tt.response)) {
				t.Fatal("tools/list-shaped response was not detected for filtering")
			}
			filtered := filterMcpToolsResponse([]byte(tt.response), contextModeTools)
			if bytes.Contains(filtered, []byte(`"name":"hidden"`)) {
				t.Fatalf("hidden tool survived filtering for %s", tt.name)
			}
		})
	}
}

func TestServerRequestDoesNotConsumeToolsListID(t *testing.T) {
	ids := &mcpRequestIDs{}
	var upstream, discarded bytes.Buffer
	if err := scanMcpInput(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"), &upstream, &discarded, ids, contextModeTools); err != nil {
		t.Fatal(err)
	}
	serverRequest := []byte(`{"jsonrpc":"2.0","id":1,"method":"notifications/request"}`)
	if filterMcpTools(serverRequest) {
		t.Fatal("server request must not be treated as tools/list response")
	}
	response := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"hidden"},{"name":"ctx_search"}]}}`)
	if !filterMcpTools(response) {
		t.Fatal("tools/list-shaped response was not detected for filtering")
	}
	var output bytes.Buffer
	if err := scanMcpOutput(strings.NewReader(string(response)+"\n"), &output, ids, contextModeTools, nil); err != nil {
		t.Fatal(err)
	}
	filtered := output.Bytes()
	if bytes.Contains(filtered, []byte(`"name":"hidden"`)) || !bytes.Contains(filtered, []byte(`"name":"ctx_search"`)) {
		t.Fatalf("hidden tool list was not filtered: %s", filtered)
	}
}

func TestMCPContentLengthFraming(t *testing.T) {
	message := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ctx_search"}}`)
	input := fmt.Sprintf("Content-Length: %d\r\nX-Test: yes\r\n\r\n%s", len(message), message)
	var upstream, output bytes.Buffer
	if err := scanMcpInput(strings.NewReader(input), &upstream, &output, &mcpRequestIDs{}, contextModeTools); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(message), message)
	if upstream.String() != want {
		t.Fatalf("framing = %q, want %q", upstream.String(), want)
	}
}

type mcpBlockingWriter struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	started chan struct{}
	release chan struct{}
	writes  int
}

func (w *mcpBlockingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	first := w.writes == 1
	w.mu.Unlock()
	if first {
		close(w.started)
		<-w.release
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Write(p)
}

func (w *mcpBlockingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func TestMCPContentLengthFramesAreAtomic(t *testing.T) {
	writer := &mcpBlockingWriter{started: make(chan struct{}), release: make(chan struct{})}
	locked := &mcpLockedWriter{writer: writer}
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	first := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601}}`)
	second := []byte(`{"jsonrpc":"2.0","id":2,"result":{}}`)
	go func() { firstDone <- writeMCPMessage(locked, mcpContentLength, first) }()
	<-writer.started
	go func() { secondDone <- writeMCPMessage(locked, mcpContentLength, second) }()
	close(writer.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("Content-Length: %d\r\n\r\n%sContent-Length: %d\r\n\r\n%s", len(first), first, len(second), second)
	if got := writer.String(); got != want {
		t.Fatalf("interleaved Content-Length frames:\n got %q\nwant %q", got, want)
	}
}

func TestBoundedContextModeProxyReturnsWhenUpstreamCloses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh fixture")
	}
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	done := make(chan int, 1)
	go func() {
		done <- runMcpProxyIO("", "sh", []string{"sh", "-c", "exit 0"}, nil, inputReader, io.Discard, io.Discard, "context-mode")
	}()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("proxy exit code = %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("proxy waited for client stdin after upstream closed")
	}
}

func TestProjectmemPolicyExposesWriterToolsOnly(t *testing.T) {
	allowed := mcpToolPolicies["projectmem"]
	want := map[string]bool{
		"log_issue": true, "record_attempt": true, "record_fix": true,
		"add_decision": true, "add_note": true,
	}
	if len(allowed) != len(want) {
		t.Fatalf("policy = %v, want %d tools", allowed, len(want))
	}
	for _, name := range allowed {
		if !want[name] {
			t.Fatalf("unexpected tool in policy: %s", name)
		}
	}
	for _, hidden := range []string{
		"get_project_map", "get_context", "precheck_file", "get_instructions",
		"get_plan", "get_issue", "get_score", "get_global_gotchas",
		"list_projects", "current_project", "search_events", "get_summary",
	} {
		if isAllowedMcpTool(hidden, allowed) {
			t.Fatalf("reader tool leaked into policy: %s", hidden)
		}
	}
}

func TestProjectmemInitializeInstructionsRewrittenAndHiddenCallsDenied(t *testing.T) {
	dir := t.TempDir()
	upstream := filepath.Join(dir, "upstream.go")
	source := `package main
import ("bufio"; "encoding/json"; "os")
func main() {
 s := bufio.NewScanner(os.Stdin)
 for s.Scan() {
  var r map[string]json.RawMessage; json.Unmarshal(s.Bytes(), &r)
  var method string; json.Unmarshal(r["method"], &method)
  switch method {
  case "initialize":
   os.Stdout.Write([]byte(` + "`" + `{"jsonrpc":"2.0","id":"init-1","result":{"protocolVersion":"2024-11-05","instructions":"call get_instructions() now"}}` + "`" + `+"\n"))
  case "tools/list":
	os.Stdout.Write([]byte(` + "`" + `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_project_map"},{"name":"log_issue"},{"name":"record_attempt"},{"name":"record_fix"},{"name":"add_decision"},{"name":"add_note"},{"name":"search_events"},{"name":"get_summary"}]}}` + "`" + `+"\n"))
  case "tools/call":
   os.Stdout.Write([]byte(` + "`" + `{"jsonrpc":"2.0","id":` + "`" + `+string(r["id"])+` + "`" + `,"result":{"content":[{"type":"text","text":"forwarded"}]}}` + "`" + `+"\n"))
  }
 }
}`
	if err := os.WriteFile(upstream, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBufferString(strings.Join([]string{
		`{"jsonrpc":"2.0","id":"init-1","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_project_map"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"add_note"}}`,
	}, "\n") + "\n")
	var output bytes.Buffer
	if code := runMcpProxyIO("", "go", []string{"go", "run", upstream}, nil, input, &output, io.Discard, "projectmem"); code != 0 {
		t.Fatalf("proxy exit code = %d", code)
	}
	raw := output.String()
	if !strings.Contains(raw, "projectmem writer tools: log_issue") || strings.Contains(raw, "call get_instructions() now") {
		t.Fatalf("initialize instructions not rewritten: %s", raw)
	}
	var responses []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var response map[string]any
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 4 {
		t.Fatalf("responses = %d, want 4: %s", len(responses), raw)
	}
	byID := map[string]map[string]any{}
	for _, response := range responses {
		byID[fmt.Sprint(response["id"])] = response
	}
	tools := byID["2"]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != len(projectmemTools) {
		t.Fatalf("tools = %v, want %v", tools, projectmemTools)
	}
	errObj := byID["3"]["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 || !strings.Contains(errObj["message"].(string), "get_project_map") {
		t.Fatalf("hidden call not denied: %s", raw)
	}
	content := byID["4"]["result"].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "forwarded" {
		t.Fatalf("allowed tools/call was not forwarded: %s", raw)
	}
}

func TestRewriteMcpInstructionsSkipsNullAndNonInitialize(t *testing.T) {
	if _, ok := rewriteMcpInstructions([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`), "x"); ok {
		t.Fatal("null result must not be rewritten")
	}
	nonInit := []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"x"}]}}`)
	if _, ok := rewriteMcpInstructions(nonInit, "x"); ok {
		t.Fatal("non-initialize result must not be rewritten")
	}
}

func TestContextModeInitializeInstructionsForcedEmpty(t *testing.T) {
	allowed := mcpToolPolicies["context-mode"]
	ids := &mcpRequestIDs{}
	var upstream, discarded bytes.Buffer
	input := `{"jsonrpc":"2.0","id":"init-ctx","method":"initialize","params":{}}` + "\n"
	if err := scanMcpInput(strings.NewReader(input), &upstream, &discarded, ids, allowed); err != nil {
		t.Fatal(err)
	}
	upstreamInstructions := "upstream tool hints about ctx_purge"
	response := `{"jsonrpc":"2.0","id":"init-ctx","result":{"protocolVersion":"2024-11-05","instructions":"` + upstreamInstructions + `"}}` + "\n"
	instructions := mcpInitializeInstructions("context-mode")
	if instructions == nil {
		t.Fatal("context-mode should have an instructions func")
	}
	if instructions() != "" {
		t.Fatalf("context-mode instructions func returned %q, want empty", instructions())
	}
	var output bytes.Buffer
	if err := scanMcpOutput(strings.NewReader(response), &output, ids, allowed, instructions); err != nil {
		t.Fatal(err)
	}
	raw := output.String()
	if strings.Contains(raw, upstreamInstructions) {
		t.Fatalf("upstream instructions leaked into context-mode output: %s", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &decoded); err != nil {
		t.Fatal(err)
	}
	result := decoded["result"].(map[string]any)
	if result["instructions"] != "" {
		t.Fatalf("instructions = %q, want empty string", result["instructions"])
	}
}

func TestProjectmemInitializeInstructionsRewritten(t *testing.T) {
	allowed := mcpToolPolicies["projectmem"]
	ids := &mcpRequestIDs{}
	var upstream, discarded bytes.Buffer
	input := `{"jsonrpc":"2.0","id":"init-pm","method":"initialize","params":{}}` + "\n"
	if err := scanMcpInput(strings.NewReader(input), &upstream, &discarded, ids, allowed); err != nil {
		t.Fatal(err)
	}
	response := `{"jsonrpc":"2.0","id":"init-pm","result":{"protocolVersion":"2024-11-05","instructions":"upstream text about get_instructions"}}` + "\n"
	instructions := mcpInitializeInstructions("projectmem")
	if instructions == nil {
		t.Fatal("projectmem should have an instructions func")
	}
	var output bytes.Buffer
	if err := scanMcpOutput(strings.NewReader(response), &output, ids, allowed, instructions); err != nil {
		t.Fatal(err)
	}
	raw := output.String()
	if strings.Contains(raw, "upstream text about") {
		t.Fatalf("upstream instructions not rewritten: %s", raw)
	}
	if !strings.Contains(raw, tools.ProjectmemMcpInstructions) {
		t.Fatalf("expected projectmem instructions in output: %s", raw)
	}
}

func TestScanMcpOutputDropsBatchArray(t *testing.T) {
	allowed := mcpToolPolicies["context-mode"]
	ids := &mcpRequestIDs{}
	batchLine := `[{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"hidden"}]}}]`
	nextLine := `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ctx_search"}]}}` + "\n"
	input := batchLine + "\n" + nextLine
	var output bytes.Buffer
	if err := scanMcpOutput(strings.NewReader(input), &output, ids, allowed, nil); err != nil {
		t.Fatal(err)
	}
	raw := output.String()
	if strings.Contains(raw, batchLine) {
		t.Fatalf("batch array was forwarded: %s", raw)
	}
	if !strings.Contains(raw, "ctx_search") {
		t.Fatalf("next single-object message did not come through: %s", raw)
	}
}

func TestScrubDescriptionRemovesHiddenToolNames(t *testing.T) {
	upstream := `{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"ctx_execute","description":"Execute a command. See ctx_purge for cleanup."},
		{"name":"ctx_search","description":"Search with ctx_purge context."},
		{"name":"ctx_purge","description":"Purge cache."}
	]}}`
	allowed := []string{"ctx_execute", "ctx_search", "ctx_index", "ctx_execute_file", "ctx_batch_execute", "ctx_fetch_and_index"}
	filtered := filterMcpToolsResponse([]byte(upstream), allowed)
	raw := string(filtered)
	if strings.Contains(raw, "ctx_purge") {
		t.Fatalf("hidden tool name leaked into output: %s", raw)
	}
	if !strings.Contains(raw, "Execute a command.") {
		t.Fatalf("kept tool description truncated: %s", raw)
	}
}

func TestScrubPrefixProtectionDoesNotMatchLongerName(t *testing.T) {
	upstream := `{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"ctx_execute","description":"Call ctx_execute_file internally."},
		{"name":"ctx_execute_file","description":"Internal helper."}
	]}}`
	allowed := []string{"ctx_execute"}
	filtered := filterMcpToolsResponse([]byte(upstream), allowed)
	raw := string(filtered)
	if strings.Contains(raw, "ctx_execute_file") {
		t.Fatalf("prefix protection failed: ctx_execute_file leaked into ctx_execute output: %s", raw)
	}
	if !strings.Contains(raw, "Call the memory system internally") {
		t.Fatalf("ctx_execute_file in description not scrubbed: %s", raw)
	}
	var parsed map[string]any
	if err := json.Unmarshal(filtered, &parsed); err != nil {
		t.Fatal(err)
	}
	result := parsed["result"].(map[string]any)
	tools := result["tools"].([]any)
	if got := tools[0].(map[string]any)["name"]; got != "ctx_execute" {
		t.Fatalf("kept tool name corrupted: got %v", got)
	}
}

func TestScrubProjectmemDescriptionRemovesHiddenReaderNames(t *testing.T) {
	upstream := `{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"log_issue","description":"Log a bug. Use list_projects to find context, then call search_events."},
		{"name":"list_projects","description":"List all projects."},
		{"name":"pre_check_file","description":"Check a file."},
		{"name":"search_events","description":"Search event log."}
	]}}`
	allowed := mcpToolPolicies["projectmem"]
	filtered := filterMcpToolsResponse([]byte(upstream), allowed)
	raw := string(filtered)
	if strings.Contains(raw, "list_projects") || strings.Contains(raw, "search_events") || strings.Contains(raw, "pre_check_file") {
		t.Fatalf("hidden reader tool names leaked into projectmem output: %s", raw)
	}
	var parsed map[string]any
	if err := json.Unmarshal(filtered, &parsed); err != nil {
		t.Fatal(err)
	}
	result := parsed["result"].(map[string]any)
	tools := result["tools"].([]any)
	if got := tools[0].(map[string]any)["name"]; got != "log_issue" {
		t.Fatalf("kept tool name corrupted: got %v", got)
	}
	if !strings.Contains(raw, "Log a bug.") {
		t.Fatalf("log_issue description truncated: %s", raw)
	}
}

func TestScrubEmptyHiddenSetPassesThroughUnchanged(t *testing.T) {
	upstream := `{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"ctx_search","description":"A tool"},
		{"name":"ctx_execute","description":"Another tool"}
	]}}`
	allowed := mcpToolPolicies["context-mode"]
	filtered := filterMcpToolsResponse([]byte(upstream), allowed)
	if !bytes.Equal(filtered, filterMcpToolsResponse([]byte(upstream), allowed)) {
		t.Fatal("non-deterministic output")
	}
}

func TestBoundedContextModeProxyContentLengthEndToEnd(t *testing.T) {
	dir := t.TempDir()
	upstream := filepath.Join(dir, "upstream.go")
	source := `package main
import ("bufio"; "encoding/json"; "fmt"; "io"; "os"; "strconv"; "strings")
func writeCL(body string) { fmt.Printf("Content-Length: %d\r\n\r\n%s", len(body), body) }
func main() {
	r := bufio.NewReader(os.Stdin)
	for {
		line, err := r.ReadString('\n')
		if err != nil { return }
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "Content-Length:") { continue }
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
		if err != nil { return }
		for {
			header, err := r.ReadString('\n')
			if err != nil { return }
			if strings.TrimRight(header, "\r\n") == "" { break }
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil { return }
		var req map[string]json.RawMessage
		if json.Unmarshal(body, &req) != nil { continue }
		var method string; json.Unmarshal(req["method"], &method)
		id := string(req["id"])
		switch method {
		case "initialize":
			writeCL("{\"jsonrpc\":\"2.0\",\"id\":" + id + ",\"result\":{\"protocolVersion\":\"2024-11-05\",\"instructions\":\"upstream hints\"}}")
		case "tools/list":
			writeCL("{\"jsonrpc\":\"2.0\",\"id\":" + id + ",\"result\":{\"tools\":[{\"name\":\"ctx_search\"},{\"name\":\"hidden_tool\"},{\"name\":\"ctx_execute\"}]}}")
		case "tools/call":
			writeCL("{\"jsonrpc\":\"2.0\",\"id\":" + id + ",\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"forwarded\"}]}}")
		}
	}
}`
	if err := os.WriteFile(upstream, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	frame := func(body string) string {
		return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	input := frame(`{"jsonrpc":"2.0","id":"init-1","method":"initialize","params":{}}`) +
		frame(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`) +
		frame(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ctx_search"}}`) +
		frame(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"hidden_tool"}}`)
	var output bytes.Buffer
	if code := runMcpProxyIO("", "go", []string{"go", "run", upstream}, nil, strings.NewReader(input), &output, io.Discard, "context-mode"); code != 0 {
		t.Fatalf("proxy exit code = %d", code)
	}
	reader := bufio.NewReader(bytes.NewReader(output.Bytes()))
	byID := map[string]map[string]any{}
	frames := 0
	for {
		message, framing, err := readMCPMessage(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read framing: %v", err)
		}
		if framing != mcpContentLength {
			t.Fatalf("response %d was not Content-Length framed", frames)
		}
		frames++
		var response map[string]any
		if err := json.Unmarshal(message, &response); err != nil {
			t.Fatalf("decode response %q: %v", message, err)
		}
		byID[fmt.Sprint(response["id"])] = response
	}
	if frames != 4 {
		t.Fatalf("frames = %d, want 4: %s", frames, output.String())
	}
	tools := byID["2"]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v, want 2", tools)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	if !names["ctx_search"] || !names["ctx_execute"] || names["hidden_tool"] {
		t.Fatalf("tools not filtered to allowlist: %v", tools)
	}
	content := byID["3"]["result"].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "forwarded" {
		t.Fatalf("allowed tools/call was not forwarded: %s", output.String())
	}
	errObj := byID["4"]["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Fatalf("hidden tools/call was not denied: %s", output.String())
	}
}

func TestShapeGateFiltersToolsDespiteMethodKey(t *testing.T) {
	allowed := mcpToolPolicies["context-mode"]
	line := `{"jsonrpc":"2.0","id":1,"method":null,"result":{"tools":[{"name":"ctx_purge"},{"name":"ctx_search"}]}}`
	if !filterMcpTools([]byte(line)) {
		t.Fatal("tools/list-shaped response with a method key was not filtered")
	}
	var output bytes.Buffer
	if err := scanMcpOutput(strings.NewReader(line+"\n"), &output, &mcpRequestIDs{}, allowed, nil); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "ctx_purge") || !strings.Contains(got, "ctx_search") {
		t.Fatalf("method-key bypass leaked hidden tools: %s", got)
	}
}

func TestScanMcpInputDropsMalformedLines(t *testing.T) {
	allowed := mcpToolPolicies["context-mode"]
	tests := []struct {
		name      string
		line      string
		errorCode int
	}{
		{"trailing batch", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hidden"}}[{"method":"tools/call","params":{"name":"hidden"}}]`, 0},
		{"trailing garbage", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hidden"}}trailing`, 0},
		{"json null", `null`, 0},
		{"recoverable id parse error", `{"jsonrpc":"2.0","id":9,"method":123}`, -32700},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var upstream, output bytes.Buffer
			if err := scanMcpInput(strings.NewReader(tt.line+"\n"), &upstream, &output, &mcpRequestIDs{}, allowed); err != nil {
				t.Fatal(err)
			}
			if upstream.Len() != 0 {
				t.Fatalf("malformed input reached upstream: %q", upstream.String())
			}
			if tt.errorCode != 0 && !strings.Contains(output.String(), fmt.Sprintf(`"code":%d`, tt.errorCode)) {
				t.Fatalf("expected local error %d, got %q", tt.errorCode, output.String())
			}
		})
	}
}

func TestScanMcpOutputDropsMalformedLines(t *testing.T) {
	allowed := mcpToolPolicies["context-mode"]
	malformed := `{}{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"ctx_purge"}]}}`
	valid := `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"ok"}]}}`
	var output bytes.Buffer
	if err := scanMcpOutput(strings.NewReader(malformed+"\n"+valid+"\n"), &output, &mcpRequestIDs{}, allowed, nil); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "ctx_purge") || strings.Contains(got, malformed) {
		t.Fatalf("malformed output was forwarded: %s", got)
	}
	if !strings.Contains(got, `"id":2`) {
		t.Fatalf("valid message after malformed line was dropped: %s", got)
	}
}

func TestScrubHiddenNamesInSchemaKeys(t *testing.T) {
	upstream := `{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"ctx_search","inputSchema":{"type":"object","properties":{"ctx_purge":{"type":"string"}}}}
	]}}`
	allowed := mcpToolPolicies["context-mode"]
	filtered := filterMcpToolsResponse([]byte(upstream), allowed)
	raw := string(filtered)
	if strings.Contains(raw, "ctx_purge") {
		t.Fatalf("hidden name survived as a schema key: %s", raw)
	}
	if !strings.Contains(raw, "ctx_search") {
		t.Fatalf("kept tool name missing: %s", raw)
	}
}

func TestScrubHiddenNamesAbsentFromResponse(t *testing.T) {
	// ctx_purge is referenced in prose but is not in this response's tool list.
	upstream := `{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"ctx_search","description":"See ctx_purge for cleanup."}
	]}}`
	allowed := mcpToolPolicies["context-mode"]
	filtered := filterMcpToolsResponse([]byte(upstream), allowed)
	if strings.Contains(string(filtered), "ctx_purge") {
		t.Fatalf("static-universe tool name leaked: %s", filtered)
	}
}

func TestScrubStaticUniverseProjectmem(t *testing.T) {
	upstream := `{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"log_issue","description":"Run precheck_file first."}
	]}}`
	allowed := mcpToolPolicies["projectmem"]
	filtered := filterMcpToolsResponse([]byte(upstream), allowed)
	if strings.Contains(string(filtered), "precheck_file") {
		t.Fatalf("projectmem static-universe name leaked: %s", filtered)
	}
}
