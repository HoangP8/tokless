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
	"github.com/HoangP8/tokless/internal/util"
)

func rtkRewrite(cmdLine string) (string, bool) {
	if cmdLine == "" {
		return "", false
	}
	rtkPath := util.ResolveRtkBin()
	if rtkPath == "" {
		fmt.Fprintln(os.Stderr, "[rtk] binary not found — command unchanged")
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, rtkPath, "rewrite", cmdLine)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	_ = cmd.Run()

	newCmd := strings.TrimSpace(stdout.String())
	if newCmd == "" || newCmd == cmdLine {
		return "", false
	}
	if !strings.HasPrefix(newCmd, "rtk ") {
		return "", false
	}
	return newCmd, true
}

// RunRtkHook handles the transparent command rewriting for Antigravity's PreToolUse hook.
func RunRtkHook() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return 0 // recover -> exit 0, no output
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
	if req.ToolCall.Name != "run_command" {
		return 0
	}

	cmdLine, ok := req.ToolCall.Args["CommandLine"].(string)
	if !ok {
		return 0
	}

	newCmd, changed := rtkRewrite(cmdLine)
	if !changed {
		return 0
	}
	req.ToolCall.Args["CommandLine"] = newCmd

	var resp struct {
		Decision  string                 `json:"decision"`
		Overwrite map[string]interface{} `json:"overwrite"`
	}
	resp.Decision = "modify"
	resp.Overwrite = req.ToolCall.Args

	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
	return 0
}

// RunRtkHookCodex handles transparent command rewriting for Codex's PreToolUse hook. 
func RunRtkHookCodex() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return 0
	}

	var req struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return 0
	}
	if req.ToolName != "Bash" {
		return 0 // only rewrite shell commands
	}

	newCmd, changed := rtkRewrite(req.ToolInput.Command)
	if !changed {
		return 0
	}

	type hookOut struct {
		HookEventName    string            `json:"hookEventName"`
		PermissionDecision string          `json:"permissionDecision"`
		UpdatedInput     map[string]string `json:"updatedInput"`
	}
	resp := struct {
		HookSpecificOutput hookOut `json:"hookSpecificOutput"`
	}{
		HookSpecificOutput: hookOut{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
			UpdatedInput:       map[string]string{"command": newCmd},
		},
	}

	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
	return 0
}