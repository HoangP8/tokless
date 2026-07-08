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

// findUnsupportedFlags mirrors upstream rtk's UNSUPPORTED_FIND_FLAGS list
// in src/cmds/system/find_cmd.rs.
var findUnsupportedFlags = map[string]bool{
	"-not": true, "!": true, "-or": true, "-o": true, "-and": true, "-a": true,
	"-exec": true, "-execdir": true, "-delete": true, "-print0": true,
	"-newer": true, "-perm": true, "-size": true, "-mtime": true, "-mmin": true,
	"-atime": true, "-amin": true, "-ctime": true, "-cmin": true, "-empty": true,
	"-link": true, "-regex": true, "-iregex": true,
}

// shellTokens splits a shell-like string into tokens, respecting single quotes,
// double quotes, and backslash escapes.
func shellTokens(s string) []string {
	var out []string
	var buf strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && !inSingle:
			if i+1 < len(s) {
				buf.WriteByte(s[i+1])
				i++
			}
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t' || c == '\n') && !inSingle && !inDouble:
			if buf.Len() > 0 {
				out = append(out, buf.String())
				buf.Reset()
			}
		default:
			buf.WriteByte(c)
		}
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}

// firstSegment returns the leading command segment of a shell line, split at
// the first &&, ||, ;, or | operator (outside quotes). Empty if none.
func firstSegment(line string) string {
	var seg strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && !inSingle:
			seg.WriteByte(c)
			if i+1 < len(line) {
				seg.WriteByte(line[i+1])
				i++
			}
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			seg.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			seg.WriteByte(c)
		case !inSingle && !inDouble && (c == '|' || c == '&' || c == ';'):
			return strings.TrimSpace(seg.String())
		default:
			seg.WriteByte(c)
		}
	}
	return strings.TrimSpace(seg.String())
}

func rtkUnsafeFind(cmdLine string) bool {
	toks := shellTokens(firstSegment(cmdLine))
	if len(toks) < 2 || toks[0] != "find" {
		return false
	}
	for _, t := range toks[1:] {
		if findUnsupportedFlags[t] {
			return true
		}
	}
	return false
}

func rtkRewrite(cmdLine string) (string, bool) {
	if cmdLine == "" {
		return "", false
	}
	if rtkUnsafeFind(cmdLine) {
		return "", false
	}
	rtkPath := util.ResolveRtkBin()
	if rtkPath == "" {
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
	if !strings.Contains(newCmd, "rtk ") {
		return "", false
	}
	newCmd = stripRtkAbsPath(newCmd)
	return newCmd, true
}

// stripRtkAbsPath converts an absolute rtk path prefix (Unix /usr/local/bin/rtk,
// Windows C:\Users\me\bin\rtk.exe, UNC \\server\share\rtk.exe) into the bare
// basename "rtk".
func stripRtkAbsPath(cmdLine string) string {
	if cmdLine == "" {
		return cmdLine
	}
	first := cmdLine[0]
	isAbs := first == '/' || first == '\\' || (len(cmdLine) >= 2 && cmdLine[1] == ':' && (cmdLine[0] >= 'A' && cmdLine[0] <= 'z'))
	if !isAbs {
		return cmdLine
	}
	idx := strings.IndexByte(cmdLine, ' ')
	if idx < 0 {
		return cmdLine
	}
	bin := cmdLine[:idx]
	tail := cmdLine[idx:]
	if li := strings.LastIndexAny(bin, "/\\"); li >= 0 {
		bin = bin[li+1:]
	}
	bin = strings.TrimSuffix(bin, ".exe")
	return bin + tail
}

// RunRtkHook handles the transparent command rewriting for Antigravity's PreToolUse hook.
func RunRtkHook() int {
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
	if req.ToolCall.Name != "run_command" {
		return 0
	}

	cmdLine, ok := req.ToolCall.Args["CommandLine"].(string)
	if !ok {
		return 0
	}

	trimmed := strings.TrimSpace(cmdLine)
	if strings.HasPrefix(trimmed, "rtk ") || trimmed == "rtk" {
		return 0
	}

	newCmd, changed := rtkRewrite(cmdLine)
	if !changed {
		return 0
	}

	req.ToolCall.Args["CommandLine"] = newCmd
	resp := struct {
		Decision  string                 `json:"decision"`
		Overwrite map[string]interface{} `json:"overwrite"`
	}{
		Decision:  "allow",
		Overwrite: req.ToolCall.Args,
	}
	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
	return 0
}

// RunRtkHookCodex handles transparent command rewriting for Codex and Claude Code.
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
		return 0
	}

	updated := map[string]string{"command": req.ToolInput.Command}
	if newCmd, changed := rtkRewrite(req.ToolInput.Command); changed {
		updated["command"] = newCmd
	}

	type hookOut struct {
		HookEventName      string            `json:"hookEventName"`
		PermissionDecision string            `json:"permissionDecision"`
		UpdatedInput       map[string]string `json:"updatedInput"`
	}
	resp := struct {
		HookSpecificOutput hookOut `json:"hookSpecificOutput"`
	}{
		HookSpecificOutput: hookOut{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
			UpdatedInput:       updated,
		},
	}

	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
	return 0
}

func RunRtkHookClaude() int {
	return RunRtkHookCodex()
}
