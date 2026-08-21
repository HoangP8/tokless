package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	for _, part := range shellParts(cmdLine) {
		if part.isOp {
			continue
		}
		toks := shellTokens(part.text)
		if len(toks) < 2 || toks[0] != "find" {
			continue
		}
		for _, t := range toks[1:] {
			if findUnsupportedFlags[t] {
				return true
			}
		}
	}
	return false
}

// rtkRewrite runs `rtk rewrite` once.
func rtkRewrite(cmdLine string) (string, bool) {
	if cmdLine == "" {
		return "", false
	}
	return rtkRewriteOnce(cmdLine)
}

// RunRtkHookCursor handles Cursor's native preToolUse hook contract.
func RunRtkHookCursor() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return 0
	}
	var req struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if json.Unmarshal(input, &req) != nil || req.ToolName != "Shell" {
		return 0
	}
	cmdLine, ok := req.ToolInput["command"].(string)
	if !ok || cmdLine == "" {
		return 0
	}
	updated := cloneMap(req.ToolInput)
	if newCmd, changed := rtkRewrite(cmdLine); changed {
		updated["command"] = newCmd
	}
	resp := map[string]any{"permission": "allow", "updated_input": updated}
	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
	return 0
}

// copilotRtkRewrite runs `rtk rewrite` with Copilot-specific guards:
// unsafe-find passthrough → single rewrite → per-segment fallback → fixpoint.
func copilotRtkRewrite(cmdLine string) (string, bool) {
	if cmdLine == "" {
		return "", false
	}
	if rtkUnsafeFind(cmdLine) {
		return "", false
	}
	if out, ok := rtkRewriteOnce(cmdLine); ok {
		return rtkRewriteFixpoint(out), true
	}
	if out, ok := rtkRewriteBySegment(cmdLine); ok {
		return rtkRewriteFixpoint(out), true
	}
	return "", false
}

func rtkRewriteFixpoint(cmdLine string) string {
	cur := cmdLine
	for i := 0; i < 3; i++ {
		next, ok := rtkRewriteOnce(cur)
		if !ok {
			return cur
		}
		cur = next
	}
	return cur
}

func rtkRewriteOnce(cmdLine string) (string, bool) {
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
	if strings.TrimSpace(newCmd) == strings.TrimSpace(cmdLine) {
		return "", false
	}
	if !rtkSegmentPresent(newCmd) {
		return "", false
	}
	if strings.HasPrefix(strings.TrimSpace(newCmd), "rtk rtk ") {
		return "", false
	}
	newCmd = stripRtkAbsPath(newCmd)
	return newCmd, true
}

func rtkSegmentPresent(cmdLine string) bool {
	for _, seg := range shellCommandSegments(cmdLine) {
		if segmentStartsWithRtk(seg) {
			return true
		}
	}
	return false
}

// rtkRewriteBySegment rewrites each shell command segment independently and
// rejoins with original operators.
func rtkRewriteBySegment(cmdLine string) (string, bool) {
	parts := shellParts(cmdLine)
	if len(parts) == 0 {
		return "", false
	}
	changed := false
	var b strings.Builder
	for _, p := range parts {
		if p.isOp {
			b.WriteString(p.text)
			continue
		}
		trimmed := strings.TrimSpace(p.text)
		if trimmed == "" || segmentStartsWithRtk(trimmed) {
			b.WriteString(p.text)
			continue
		}
		out, ok := rtkRewriteOnce(trimmed)
		if !ok {
			b.WriteString(p.text)
			continue
		}
		lead := p.text[:len(p.text)-len(strings.TrimLeft(p.text, " \t"))]
		trail := p.text[len(strings.TrimRight(p.text, " \t")):]
		b.WriteString(lead)
		b.WriteString(out)
		b.WriteString(trail)
		changed = true
	}
	if !changed {
		return "", false
	}
	return b.String(), true
}

type shellPart struct {
	isOp bool
	text string
}

// shellParts splits a line into alternating command segments and operators
// (&&, ||, ;, |), preserving operator text for rejoin.
func shellParts(line string) []shellPart {
	var parts []shellPart
	var seg strings.Builder
	inSingle, inDouble := false, false
	flushSeg := func() {
		if seg.Len() == 0 {
			return
		}
		parts = append(parts, shellPart{isOp: false, text: seg.String()})
		seg.Reset()
	}
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
		case !inSingle && !inDouble && c == '&' && i+1 < len(line) && line[i+1] == '&':
			flushSeg()
			parts = append(parts, shellPart{isOp: true, text: "&&"})
			i++
		case !inSingle && !inDouble && c == '|' && i+1 < len(line) && line[i+1] == '|':
			flushSeg()
			parts = append(parts, shellPart{isOp: true, text: "||"})
			i++
		case !inSingle && !inDouble && c == '|':
			flushSeg()
			parts = append(parts, shellPart{isOp: true, text: "|"})
		case !inSingle && !inDouble && c == ';':
			flushSeg()
			parts = append(parts, shellPart{isOp: true, text: ";"})
		default:
			seg.WriteByte(c)
		}
	}
	flushSeg()
	return parts
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

// RunRtkHook handles Antigravity PreToolUse for run_command.
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
	if req.ToolCall.Name != "run_command" || req.ToolCall.Args == nil {
		return 0
	}
	cmdLine, ok := req.ToolCall.Args["CommandLine"].(string)
	if !ok || cmdLine == "" {
		return 0
	}

	finalCmd := cmdLine
	if newCmd, changed := rtkRewrite(cmdLine); changed {
		finalCmd = newCmd
	} else if newCmd, segOK := rtkRewriteBySegment(cmdLine); segOK {
		finalCmd = newCmd
	}
	if finalCmd == cmdLine && !commandUsesRtk(cmdLine) {
		return 0
	}
	req.ToolCall.Args["CommandLine"] = finalCmd

	resp := struct {
		Decision  string                 `json:"decision"`
		AllowTool bool                   `json:"allowTool"`
		Overwrite map[string]interface{} `json:"overwrite"`
	}{
		Decision:  "allow",
		AllowTool: true,
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
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return 0
	}
	if req.ToolName != "Bash" && req.ToolName != "bash" {
		return 0
	}

	updated := cloneMap(req.ToolInput)
	cmdLine, _ := updated["command"].(string)
	if newCmd, changed := rtkRewrite(cmdLine); changed {
		updated["command"] = newCmd
	}

	type hookOut struct {
		HookEventName      string         `json:"hookEventName"`
		PermissionDecision string         `json:"permissionDecision"`
		UpdatedInput       map[string]any `json:"updatedInput"`
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

// RunRtkHookDroid handles transparent command rewriting for Factory Droid.
func RunRtkHookDroid() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return 0
	}

	var req map[string]json.RawMessage
	if err := json.Unmarshal(input, &req); err != nil {
		return 0
	}
	var toolName string
	if v, ok := req["tool_name"]; ok {
		_ = json.Unmarshal(v, &toolName)
	}
	if toolName != "Execute" {
		return 0
	}

	var toolInput map[string]any
	if ti, ok := req["tool_input"]; ok {
		_ = json.Unmarshal(ti, &toolInput)
	}
	cmdKey := ""
	var cmdLine string
	if c, ok := toolInput["command"].(string); ok && c != "" {
		cmdKey = "command"
		cmdLine = c
	} else if c, ok := toolInput["CommandLine"].(string); ok && c != "" {
		cmdKey = "CommandLine"
		cmdLine = c
	}
	if cmdKey == "" {
		return 0
	}

	updated := cloneMap(toolInput)
	if newCmd, changed := rtkRewrite(cmdLine); changed {
		updated[cmdKey] = newCmd
	}

	type hookOut struct {
		HookEventName      string         `json:"hookEventName"`
		PermissionDecision string         `json:"permissionDecision"`
		UpdatedInput       map[string]any `json:"updatedInput"`
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

// RunRtkHookCopilot rewrites bash/shell via `rtk rewrite` only.
var copilotRtkTracePath = filepath.Join(util.CopilotPathsResolved().Dir, "tokless-rtk.log")

func copilotRtkTrace(cmdLine string) {
	t := time.Now().UTC().Format(time.RFC3339)
	line := t + " | " + cmdLine + "\n"
	f, err := os.OpenFile(copilotRtkTracePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func RunRtkHookCopilot() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return 0
	}

	var req map[string]json.RawMessage
	if err := json.Unmarshal(input, &req); err != nil {
		return 0
	}

	if _, ok := req["toolResult"]; ok {
		return copilotRtkPost(req, false)
	}
	if _, ok := req["tool_result"]; ok {
		return copilotRtkPost(req, true)
	}

	// VS Code Chat / dual-fire PreToolUse.
	if v, ok := req["tool_name"]; ok {
		var toolName string
		if json.Unmarshal(v, &toolName) != nil || toolName == "" {
			return 0
		}
		if !copilotShellTool(toolName) {
			return 0
		}
		toolInput := map[string]any{}
		if ti, ok := req["tool_input"]; ok {
			_ = json.Unmarshal(ti, &toolInput)
		}
		cmdLine, _ := toolInput["command"].(string)
		if cmdLine == "" {
			return 0
		}
		newCmd, changed, approve := copilotRtkDecide(cmdLine)
		if !approve {
			return 0
		}
		outBody := map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": "RTK auto-approved",
		}
		effective := cmdLine
		if changed {
			effective = newCmd
			copilotRtkTrace(effective)
		} else if commandUsesRtk(effective) {
			copilotRtkTrace(effective)
		}
		if changed {
			updated := cloneMap(toolInput)
			updated["command"] = effective
			outBody["updatedInput"] = updated
		}
		resp := map[string]any{"hookSpecificOutput": outBody}
		if out, err := json.Marshal(resp); err == nil {
			fmt.Println(string(out))
		}
		return 0
	}

	// Copilot CLI preToolUse (camelCase).
	toolName := copilotToolName(req)
	if !copilotShellTool(toolName) {
		return 0
	}

	args := parseToolArgsMap(req["toolArgs"])
	cmdLine, _ := args["command"].(string)
	if cmdLine == "" {
		cmdLine = copilotCommand(req)
	}
	if cmdLine == "" {
		return 0
	}
	newCmd, changed, approve := copilotRtkDecide(cmdLine)
	if !approve {
		return 0
	}

	resp := map[string]any{
		"permissionDecision":       "allow",
		"permissionDecisionReason": "RTK auto-approved",
	}
	effective := cmdLine
	if changed {
		effective = newCmd
		copilotRtkTrace(effective)
	} else if commandUsesRtk(effective) {
		copilotRtkTrace(effective)
	}
	if changed {
		modified := cloneMap(args)
		if len(modified) == 0 {
			modified = map[string]any{}
		}
		modified["command"] = effective
		resp["modifiedArgs"] = modified
	}
	if out, err := json.Marshal(resp); err == nil {
		fmt.Println(string(out))
	}
	return 0
}

// RunRtkHookCline rewrites shell-execution tool commands for Cline's CLI
// PreToolUse hook using Cline's overrideInput response.
func RunRtkHookCline() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return 0
	}

	var req map[string]json.RawMessage
	if err := json.Unmarshal(input, &req); err != nil {
		fmt.Println("{}")
		return 0
	}
	toolName, toolInput, ok := clineToolInput(req)
	if !ok || !clineShellTool(toolName) {
		fmt.Println("{}")
		return 0
	}
	if commands, changed := clineRewriteCommands(toolInput); changed {
		updated := cloneMap(toolInput)
		updated["commands"] = commands
		if out, err := json.Marshal(map[string]any{"overrideInput": updated}); err == nil {
			fmt.Println(string(out))
		}
		return 0
	}
	cmdLine, _ := toolInput["command"].(string)
	if cmdLine == "" {
		fmt.Println("{}")
		return 0
	}

	newCmd, changed, approve := copilotRtkDecide(cmdLine)
	if !approve || !changed || newCmd == "" || newCmd == cmdLine {
		fmt.Println("{}")
		return 0
	}
	updated := cloneMap(toolInput)
	updated["command"] = newCmd
	if out, err := json.Marshal(map[string]any{"overrideInput": updated}); err == nil {
		fmt.Println(string(out))
	}
	return 0
}

func clineRewriteCommands(toolInput map[string]any) ([]any, bool) {
	commands, ok := toolInput["commands"].([]any)
	if !ok {
		return nil, false
	}
	updated := make([]any, len(commands))
	changed := false
	for i, item := range commands {
		updated[i] = item
		cmdLine, ok := item.(string)
		if !ok {
			continue
		}
		newCmd, didChange, approve := copilotRtkDecide(cmdLine)
		if approve && didChange && newCmd != "" && newCmd != cmdLine {
			updated[i] = newCmd
			changed = true
		}
	}
	if !changed {
		return nil, false
	}
	return updated, true
}

func clineToolInput(req map[string]json.RawMessage) (string, map[string]any, bool) {
	if v, ok := req["tool_call"]; ok {
		var tc struct {
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}
		if json.Unmarshal(v, &tc) == nil && tc.Name != "" {
			return tc.Name, tc.Input, true
		}
	}
	if v, ok := req["preToolUse"]; ok {
		var pt struct {
			ToolName   string         `json:"toolName"`
			Parameters map[string]any `json:"parameters"`
		}
		if json.Unmarshal(v, &pt) == nil && pt.ToolName != "" {
			return pt.ToolName, pt.Parameters, true
		}
	}
	return "", nil, false
}

func clineShellTool(name string) bool {
	switch name {
	case "execute_command", "run_commands":
		return true
	default:
		return false
	}
}

// copilotRtkPost traces the rtk-wrapped command to stderr for verification.
func copilotRtkPost(req map[string]json.RawMessage, snake bool) int {
	var toolName string
	if snake {
		if v, ok := req["tool_name"]; ok {
			_ = json.Unmarshal(v, &toolName)
		}
	} else {
		toolName = copilotToolName(req)
	}
	if !copilotShellTool(toolName) {
		return 0
	}

	var cmdLine string
	if snake {
		if v, ok := req["tool_input"]; ok {
			var ti map[string]any
			if json.Unmarshal(v, &ti) == nil {
				cmdLine, _ = ti["command"].(string)
			}
		}
	} else {
		args := parseToolArgsMap(req["toolArgs"])
		cmdLine, _ = args["command"].(string)
		if cmdLine == "" {
			cmdLine = copilotCommand(req)
		}
	}
	if cmdLine == "" {
		return 0
	}

	fmt.Fprintf(os.Stderr, "rtk: %s\n", cmdLine)
	copilotRtkTrace(cmdLine)
	return 0
}

func copilotShellTool(name string) bool {
	switch strings.ToLower(name) {
	case "bash", "powershell", "shell", "runterminalcommand":
		return true
	default:
		return false
	}
}

func parseToolArgsMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var asStr string
	if json.Unmarshal(raw, &asStr) == nil && asStr != "" {
		var m map[string]any
		if json.Unmarshal([]byte(asStr), &m) == nil {
			return m
		}
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		return m
	}
	return nil
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// copilotRtkDecide: only rewrite what `rtk rewrite` supports.
func copilotRtkDecide(cmdLine string) (newCmd string, changed bool, approve bool) {
	newCmd, changed = copilotRtkRewrite(cmdLine)
	if changed {
		return newCmd, true, true
	}
	if commandUsesRtk(cmdLine) {
		return "", false, true
	}
	return "", false, false
}

// commandUsesRtk reports whether any shell command segment invokes rtk.
func commandUsesRtk(cmdLine string) bool {
	for _, seg := range shellCommandSegments(cmdLine) {
		if segmentStartsWithRtk(seg) {
			return true
		}
	}
	return false
}

func segmentStartsWithRtk(seg string) bool {
	trimmed := strings.TrimSpace(seg)
	if trimmed == "" {
		return false
	}
	bin := firstRawToken(trimmed)
	if len(bin) >= 2 {
		if (bin[0] == '"' && bin[len(bin)-1] == '"') || (bin[0] == '\'' && bin[len(bin)-1] == '\'') {
			bin = bin[1 : len(bin)-1]
		}
	}
	base := bin
	if i := strings.LastIndexAny(bin, "/\\"); i >= 0 {
		base = bin[i+1:]
	}
	lower := strings.ToLower(base)
	return lower == "rtk" || strings.TrimSuffix(lower, ".exe") == "rtk"
}

func firstRawToken(s string) string {
	var buf strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t' || c == '\n') && !inSingle && !inDouble:
			return buf.String()
		default:
			buf.WriteByte(c)
		}
	}
	return buf.String()
}

// shellCommandSegments splits a shell line into command segments at
// &&, ||, ;, and | (outside quotes). Operators are discarded.
func shellCommandSegments(line string) []string {
	var segs []string
	var seg strings.Builder
	inSingle, inDouble := false, false
	flush := func() {
		s := strings.TrimSpace(seg.String())
		if s != "" {
			segs = append(segs, s)
		}
		seg.Reset()
	}
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
		case !inSingle && !inDouble && c == '&' && i+1 < len(line) && line[i+1] == '&':
			flush()
			i++
		case !inSingle && !inDouble && c == '|' && i+1 < len(line) && line[i+1] == '|':
			flush()
			i++
		case !inSingle && !inDouble && (c == '|' || c == ';'):
			flush()
		default:
			seg.WriteByte(c)
		}
	}
	flush()
	return segs
}

// isAlreadyRtk reports whether the whole line is a single rtk invocation
// (first token only). Prefer commandUsesRtk for multi-segment lines.
func isAlreadyRtk(cmdLine string) bool {
	return segmentStartsWithRtk(strings.TrimSpace(cmdLine)) &&
		len(shellCommandSegments(cmdLine)) <= 1
}

func rtkRewriteIfNeeded(cmdLine string) (string, bool) {
	return rtkRewrite(cmdLine)
}

func copilotToolName(req map[string]json.RawMessage) string {
	for _, key := range []string{"toolName", "tool_name"} {
		if v, ok := req[key]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil && s != "" {
				return s
			}
		}
	}
	return ""
}

func copilotCommand(req map[string]json.RawMessage) string {
	if v, ok := req["toolArgs"]; ok {
		if c := commandFromToolArgs(v); c != "" {
			return c
		}
	}
	if v, ok := req["tool_input"]; ok {
		var ti struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(v, &ti) == nil {
			return ti.Command
		}
	}
	return ""
}

func commandFromToolArgs(raw json.RawMessage) string {
	var asStr string
	if json.Unmarshal(raw, &asStr) == nil {
		var args map[string]any
		if json.Unmarshal([]byte(asStr), &args) == nil {
			if c, ok := args["command"].(string); ok {
				return c
			}
		}
	}
	var args map[string]any
	if json.Unmarshal(raw, &args) == nil {
		if c, ok := args["command"].(string); ok {
			return c
		}
	}
	return ""
}
