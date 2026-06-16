package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// extractNudge removes `echo ` and surrounding matching quotes from a nudge command.
func extractNudge(cmd string) string {
	s := strings.TrimSpace(cmd)
	if strings.HasPrefix(s, "echo ") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "echo "))
		if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) || (strings.HasPrefix(s, `'`) && strings.HasSuffix(s, `'`)) {
			s = s[1 : len(s)-1]
			s = strings.ReplaceAll(s, `\"`, `"`)
		}
	}
	return s
}

// RunContextModeHookAgy handles mapping Antigravity's PreToolUse to context-mode's gemini-cli beforetool.
func RunContextModeHookAgy() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return 0 // fail-open
	}

	var req struct {
		ToolCall struct {
			Name string                 `json:"name"`
			Args map[string]interface{} `json:"args"`
		} `json:"toolCall"`
		WorkspacePaths []string `json:"workspacePaths"`
		ConversationID string   `json:"conversationId"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return 0
	}

	agyName := req.ToolCall.Name
	canonicalMap := map[string]string{
		"run_command":          "Bash",
		"view_file":            "Read",
		"read_file":            "Read",
		"edit_file":            "Edit",
		"replace_file_content": "Edit",
		"write_file":           "Write",
		"glob":                 "Glob",
		"search_file_content":  "Grep",
		"grep_search":          "Grep",
		"web_fetch":            "WebFetch",
	}

	var canonical string
	if mapped, ok := canonicalMap[agyName]; ok {
		canonical = mapped
	} else if strings.HasPrefix(agyName, "mcp__") {
		canonical = agyName // Passthrough MCP tools
	} else {
		return 0 // Unknown tool, passthrough
	}

	argMap := map[string]string{
		"CommandLine": "command",
		"Cwd":         "cwd",
		"path":        "file_path",
		"url":         "url",
	}

	remappedArgs := make(map[string]interface{})
	for k, v := range req.ToolCall.Args {
		if newKey, ok := argMap[k]; ok {
			remappedArgs[newKey] = v
		} else {
			remappedArgs[k] = v
		}
	}

	projectDir := ""
	if len(req.WorkspacePaths) > 0 && req.WorkspacePaths[0] != "" {
		projectDir = req.WorkspacePaths[0]
	} else {
		projectDir, _ = os.Getwd()
	}

	ctxModeStdin := map[string]interface{}{
		"tool_name":  canonical,
		"tool_input": remappedArgs,
		"cwd":        projectDir,
	}

	ctxModeJSON, err := json.Marshal(ctxModeStdin)
	if err != nil {
		return 0
	}

	scriptPath := ""
	for i := 1; i < len(os.Args)-1; i++ {
		if os.Args[i] == "--script" {
			scriptPath = os.Args[i+1]
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if scriptPath != "" {
		cmd = exec.CommandContext(ctx, "node", scriptPath)
	} else {
		cmd = exec.CommandContext(ctx, "context-mode", "hook", "gemini-cli", "beforetool")
	}

	cmd.Env = append(os.Environ(),
		"CONTEXT_MODE_PLATFORM=antigravity",
		"GEMINI_PROJECT_DIR="+projectDir,
		"CLAUDE_PROJECT_DIR="+projectDir,
		"CLAUDE_SESSION_ID="+req.ConversationID,
	)
	cmd.Dir = projectDir
	cmd.Stdin = bytes.NewReader(ctxModeJSON)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run() // Ignore errors, fail-open on output parse

	outStr := strings.TrimSpace(stdout.String())
	if outStr == "" {
		return 0 // passthrough/allow
	}

	var ctxModeResp map[string]interface{}
	if err := json.Unmarshal([]byte(outStr), &ctxModeResp); err != nil {
		return 0
	}

	// 1. Explicit Deny
	if decision, ok := ctxModeResp["decision"].(string); ok && decision == "deny" {
		reason := "Request denied by context-mode"
		if r, ok := ctxModeResp["reason"].(string); ok {
			reason = r
		}
		emitAgyDeny(reason)
		return 0
	}

	// 2. Modify (nudge) -> convert to Deny
	if hso, ok := ctxModeResp["hookSpecificOutput"].(map[string]interface{}); ok {
		if ti, ok := hso["tool_input"].(map[string]interface{}); ok {
			if command, ok := ti["command"].(string); ok && strings.HasPrefix(strings.TrimSpace(command), "echo ") {
				emitAgyDeny(extractNudge(command))
				return 0
			}
		}
		// 3. AdditionalContext -> passthrough to avoid over-blocking
		if _, ok := hso["additionalContext"]; ok {
			return 0
		}
	}

	return 0
}

func emitAgyDeny(reason string) {
	resp := map[string]interface{}{
		"decision": "deny",
		"reason":   reason,
	}
	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
}