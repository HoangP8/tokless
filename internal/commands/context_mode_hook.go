package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

func contextModeRoutingFile() string {
	return filepath.Join(util.Home(), ".gemini", "config", "tokless", "context-mode-routing.md")
}

// RunContextModePreInvocationAgy injects context-mode routing instruction once
// at session start (invocationNum == 1).
func RunContextModePreInvocationAgy() int {
	raw, ok := util.ReadFileSafe(contextModeRoutingFile())
	if !ok {
		return 0
	}
	if !isFirstInvocation() {
		return 0
	}
	resp := map[string]interface{}{
		"injectSteps": []map[string]interface{}{
			{
				"ephemeralMessage": map[string]interface{}{
					"content": raw,
				},
			},
		},
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return 0
	}
	fmt.Println(string(out))
	return 0
}

func isFirstInvocation() bool {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return false
	}
	var req struct {
		InvocationNum int `json:"invocationNum"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return false
	}
	return req.InvocationNum <= 1
}

// Redirect messages mirror context-mode's Gemini CLI routing engine verbatim.
const (
	webFetchMsg = "context-mode: WebFetch redirected. " +
		"Call mcp__context-mode__ctx_fetch_and_index(url, source) to fetch + index the page, " +
		"then mcp__context-mode__ctx_search(queries) to query the indexed content — " +
		"the raw page bytes stay in storage instead of entering your conversation. " +
		"Or call mcp__context-mode__ctx_execute(language, code) when you want to derive " +
		"your answer in one round trip (parse, extract, count) without persisting the response. " +
		"Both have full network access. Retry on transient DNS errors (EAI_AGAIN, ETIMEDOUT, ENETUNREACH)."

	curlWgetMsg = "context-mode: curl/wget redirected. " +
		"Call mcp__context-mode__ctx_execute(language, code) to fetch the URL, " +
		"derive your answer in code, and print only the result — " +
		"the raw HTTP body stays in the sandbox instead of entering your conversation. " +
		"Or call mcp__context-mode__ctx_fetch_and_index(url, source) when you want to query " +
		"the response later via mcp__context-mode__ctx_search. " +
		"Both have full network access. Retry on transient DNS errors (EAI_AGAIN, ETIMEDOUT, ENETUNREACH)."

	inlineHttpMsg = "context-mode: Inline HTTP redirected. " +
		"Call mcp__context-mode__ctx_execute(language, code) to fetch, " +
		"derive your answer in code, and console.log() only the result — " +
		"the raw response body stays in the sandbox instead of entering your conversation. " +
		"Full network access. Retry on transient DNS errors (EAI_AGAIN, ETIMEDOUT, ENETUNREACH)."
)

// RunContextModePreToolUseAgy redirects raw tools to context-mode equivalents
// using per-tool messages from upstream context-mode routing engine.
func RunContextModePreToolUseAgy() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return 0
	}

	var req struct {
		ToolCall struct {
			Name string                 `json:"name"`
			Args map[string]interface{} `json:"args"`
		} `json:"toolCall"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return 0
	}

	switch req.ToolCall.Name {
	case "read_url_content", "web_fetch":
		overwrite := copyArgs(req.ToolCall.Args)
		overwrite["url"] = "data:text/plain;charset=utf-8," + webFetchMsg
		emitModify(overwrite)

	case "run_command", "run_shell_command":
		cmd, _ := req.ToolCall.Args["CommandLine"].(string)
		if cmd == "" {
			return 0
		}
		msg := classifyShellRedirect(cmd)
		if msg == "" {
			return 0
		}
		overwrite := copyArgs(req.ToolCall.Args)
		escaped := strings.ReplaceAll(msg, "'", "'\\''")
		overwrite["CommandLine"] = "echo '" + escaped + "'"
		emitModify(overwrite)
	}
	return 0
}

func classifyShellRedirect(cmd string) string {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	if strings.Contains(lower, "curl ") || strings.Contains(lower, "wget ") ||
		strings.HasPrefix(lower, "curl\n") || strings.HasPrefix(lower, "wget\n") {
		return curlWgetMsg
	}
	triggers := []string{"fetch(", "requests.get", "requests.post",
		"http.get", "http.request", "urllib", "httpx.get", "httpx.post"}
	for _, t := range triggers {
		if strings.Contains(lower, t) {
			return inlineHttpMsg
		}
	}
	return ""
}

func copyArgs(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for k, v := range args {
		out[k] = v
	}
	return out
}

func emitModify(overwrite map[string]interface{}) {
	resp := map[string]interface{}{
		"decision":  "modify",
		"overwrite": overwrite,
	}
	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
}
