package commands

import (
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
	args := []string{"/c", "C:/tools/codegraph.cmd"}
	if got := normalizedCmdBatchArgs("cmd.exe", args, false); !reflect.DeepEqual(got, args) {
		t.Fatalf("non-Windows args changed: %q", got)
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

func TestHeadroomPolicyFiltersAndRejectsHiddenTools(t *testing.T) {
	allowed := mcpToolPolicies["headroom"]
	ids := &mcpRequestIDs{ids: map[string]bool{}}
	var upstream, output bytes.Buffer
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"headroom_compress"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"headroom_stats"}}`,
	}, "\n") + "\n"
	if err := scanMcpInput(strings.NewReader(input), &upstream, &output, ids, allowed); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(upstream.String(), "tools/call"); got != 1 || strings.Contains(upstream.String(), "headroom_stats") {
		t.Fatalf("hidden Headroom call reached upstream: %q", upstream.String())
	}
	if !strings.Contains(output.String(), `"id":3`) || !strings.Contains(output.String(), `"code":-32601`) || !strings.Contains(output.String(), `tool \"headroom_stats\" is not available`) {
		t.Fatalf("unexpected denied response: %q", output.String())
	}
	output.Reset()
	if err := scanMcpInput(strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"headroom_stats"}}`+"\n"), &upstream, &output, ids, allowed); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("denied notification produced response: %q", output.String())
	}

	output.Reset()
	response := `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"headroom_compress"},{"name":"headroom_stats"},{"name":"headroom_retrieve"},{"name":"headroom_read"}]}}` + "\n"
	if err := scanMcpOutput(strings.NewReader(response), &output, ids, allowed); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "headroom_stats") || strings.Contains(got, "headroom_read") || !strings.Contains(got, "headroom_compress") || !strings.Contains(got, "headroom_retrieve") {
		t.Fatalf("Headroom list not correctly filtered: %q", got)
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
	if err := scanMcpInput(strings.NewReader(message+"\n"), &upstream, &output, &mcpRequestIDs{ids: map[string]bool{}}, contextModeTools); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(upstream.String()); got != message {
		t.Fatalf("upstream message changed or truncated: got %d bytes, want %d", len(got), len(message))
	}
}

func TestScanMcpInputRejectsHiddenToolLocally(t *testing.T) {
	request := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"hidden"}}`
	var upstream, output bytes.Buffer
	if err := scanMcpInput(strings.NewReader(request+"\n"), &upstream, &output, &mcpRequestIDs{ids: map[string]bool{}}, contextModeTools); err != nil {
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
	if err := scanMcpInput(strings.NewReader(batch+"\n"), &upstream, &output, &mcpRequestIDs{ids: map[string]bool{}}, contextModeTools); err != nil {
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

func TestToolsListIDIsCanonicalAndRemovedAfterResponse(t *testing.T) {
	ids := &mcpRequestIDs{ids: map[string]bool{}}
	var upstream, discarded bytes.Buffer
	input := "{\"jsonrpc\":\"2.0\",\"id\": 1,\"method\":\"tools/list\"}\n"
	if err := scanMcpInput(strings.NewReader(input), &upstream, &discarded, ids, contextModeTools); err != nil {
		t.Fatal(err)
	}
	response := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"hidden"}]}}`)
	if !filterMcpTools(response, ids) {
		t.Fatal("equivalent request/response IDs did not correlate")
	}
	if filterMcpTools(response, ids) {
		t.Fatal("request ID was reused after correlated response")
	}
}

func TestToolsListIDsCorrelateSemanticJSONValues(t *testing.T) {
	tests := []struct {
		name     string
		request  string
		response string
	}{
		{"numeric normalization", `1.0`, `1`},
		{"escaped string", `"\u0061"`, `"a"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := &mcpRequestIDs{ids: map[string]bool{}}
			input := `{"jsonrpc":"2.0","id":` + tt.request + `,"method":"tools/list"}` + "\n"
			var upstream, discarded bytes.Buffer
			if err := scanMcpInput(strings.NewReader(input), &upstream, &discarded, ids, contextModeTools); err != nil {
				t.Fatal(err)
			}
			response := []byte(`{"jsonrpc":"2.0","id":` + tt.response + `,"result":{"tools":[{"name":"hidden"}]}}`)
			if !filterMcpTools(response, ids) {
				t.Fatalf("IDs did not correlate: request %s, response %s", tt.request, tt.response)
			}
		})
	}
}

func TestServerRequestDoesNotConsumeToolsListID(t *testing.T) {
	ids := &mcpRequestIDs{ids: map[string]bool{}}
	var upstream, discarded bytes.Buffer
	if err := scanMcpInput(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"), &upstream, &discarded, ids, contextModeTools); err != nil {
		t.Fatal(err)
	}
	serverRequest := []byte(`{"jsonrpc":"2.0","id":1,"method":"notifications/request"}`)
	if filterMcpTools(serverRequest, ids) {
		t.Fatal("server request consumed pending tools/list ID")
	}
	response := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"hidden"},{"name":"ctx_search"}]}}`)
	var output bytes.Buffer
	if err := scanMcpOutput(strings.NewReader(string(response)+"\n"), &output, ids, contextModeTools); err != nil {
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
	if err := scanMcpInput(strings.NewReader(input), &upstream, &output, &mcpRequestIDs{ids: map[string]bool{}}, contextModeTools); err != nil {
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
