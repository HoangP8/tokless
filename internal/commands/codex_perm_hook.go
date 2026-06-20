package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// codexPermAllowlist is the set of first-tokens that are auto-approved.
var codexPermAllowlist = map[string]bool{
	"rtk": true, "tokless": true, "git": true, "ls": true,
	"node": true, "npm": true, "npx": true,
	"context-mode": true, "codegraph": true,
	"cat": true, "head": true, "tail": true,
	"grep": true, "find": true, "pwd": true,
	"which": true, "echo": true, "true": true, "false": true,
}

// RunCodexPermHook handles Codex's PermissionRequest hook.
func RunCodexPermHook() int {
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
	if !codexPermAllow(req.ToolName, req.ToolInput.Command) {
		return 0
	}
	resp := struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      struct {
				Behavior string `json:"behavior"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}{}
	resp.HookSpecificOutput.HookEventName = "PermissionRequest"
	resp.HookSpecificOutput.Decision.Behavior = "allow"
	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
	return 0
}

func codexPermAllow(toolName, command string) bool {
	if toolName == "apply_patch" {
		return true
	}
	if toolName != "Bash" {
		return false
	}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return false
	}
	tok := cmd
	if sp := strings.IndexAny(tok, " \t"); sp >= 0 {
		tok = tok[:sp]
	}
	if idx := strings.LastIndexByte(tok, '/'); idx >= 0 {
		tok = tok[idx+1:]
	}
	if util.IsWin {
		if idx := strings.LastIndexByte(tok, '\\'); idx >= 0 {
			tok = tok[idx+1:]
		}
		tok = strings.TrimSuffix(strings.TrimSuffix(tok, ".exe"), ".cmd")
		tok = strings.TrimSuffix(tok, ".bat")
	}
	return codexPermAllowlist[tok]
}
