package commands

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/util"
)

type runtimeIssue struct {
	kind   string
	detail string
}

// probeAgentRuntime checks that wired hook/MCP commands can actually resolve
// (and, where cheap, run). Catches Windows slash bugs that static VerifyFor miss.
func probeAgentRuntime(agentID string) []runtimeIssue {
	var out []runtimeIssue
	for _, cmd := range managedHookCommands(agentID) {
		if detail := probeHookCommand(cmd); detail != "" {
			out = append(out, runtimeIssue{kind: "hook", detail: detail})
		}
	}
	for _, spawn := range managedMcpSpawns(agentID) {
		if detail := probeMcpSpawn(spawn.Command, spawn.Args); detail != "" {
			out = append(out, runtimeIssue{kind: "mcp", detail: detail})
		}
	}
	return out
}

// --- hooks ---

func probeHookCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "empty hook command"
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "empty hook command"
	}
	exe := fields[0]
	if isToklessManagedHook(fields) && windowsBashHostilePath(exe) {
		return "backslash path breaks Git Bash hooks — re-run tokless (" + shortPath(exe) + ")"
	}
	if err := resolveRunnable(exe); err != "" {
		return "hook binary not found: " + shortPath(exe)
	}
	if !isToklessManagedHook(fields) {
		return ""
	}
	if detail := liveHook(fields); detail != "" {
		return detail
	}
	return ""
}

// windowsBashHostilePath reports unquoted Windows paths with backslashes.
func windowsBashHostilePath(exe string) bool {
	if runtime.GOOS != "windows" && !util.IsWin {
		return false
	}
	if !util.IsWin {
		return false
	}
	if strings.Contains(exe, "/") {
		return false
	}
	if len(exe) >= 3 && exe[1] == ':' && exe[2] == '\\' {
		return true
	}
	return strings.HasPrefix(exe, `\\`)
}

func isToklessManagedHook(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(fields[0], "\\", "/")))
	if base != "tokless" && base != "tokless.exe" {
		return false
	}
	switch fields[1] {
	case "rtk-hook", "codex-perm", "agy-hook", "copilot-hook", "index":
		return true
	default:
		return false
	}
}

// liveHook runs the hook with empty stdin. tokless rtk-hook exits 0 on empty input.
func liveHook(fields []string) string {
	exe, err := lookExe(fields[0])
	if err != "" {
		return "hook not runnable: " + shortPath(fields[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, fields[1:]...)
	cmd.Stdin = bytes.NewReader(nil)
	var stderr bytes.Buffer
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ""
		}
		msg := stderr.String() + err.Error()
		if isSpawnFail(err, msg) {
			return "hook not runnable: " + shortPath(fields[0])
		}
	}
	return ""
}

// --- MCP ---

func probeMcpSpawn(command string, args []string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "empty mcp command"
	}
	if err := resolveRunnable(command); err != "" {
		return "mcp binary not found: " + shortPath(command)
	}
	innerCmd, innerArgs := unwrapRunMcp(command, args)
	if !isCmdName(innerCmd) {
		if err := resolveRunnable(innerCmd); err != "" {
			if strings.ContainsAny(innerCmd, `/\:`) {
				return "mcp target not found: " + shortPath(innerCmd)
			}
		}
	}
	argv := append([]string{innerCmd}, innerArgs...)
	path := innerCmd
	if lp, err := exec.LookPath(innerCmd); err == nil {
		path = lp
	}
	exe, normArgs := resolveMcpCommand(path, argv)
	if isCmdName(exe) || isCmdName(filepath.Base(path)) {
		return probeCmdBatch(exe, normArgs)
	}
	if err := resolveRunnable(exe); err != "" {
		return "mcp binary not found: " + shortPath(exe)
	}
	if looksLikeCodegraph(exe, normArgs) {
		return liveCodegraphVersion(exe, normArgs)
	}
	return ""
}

func unwrapRunMcp(command string, args []string) (string, []string) {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/")))
	if base != "tokless" && base != "tokless.exe" {
		return command, args
	}
	// run-mcp --agent <id> <cmd> [args...]
	if len(args) >= 4 && args[0] == "run-mcp" && args[1] == "--agent" {
		return args[3], append([]string(nil), args[4:]...)
	}
	return command, args
}

func probeCmdBatch(exe string, args []string) string {
	if len(args) < 2 || !strings.EqualFold(args[0], "/c") {
		return ""
	}
	batch := args[1]
	if !fileExistsAny(batch) {
		return "mcp batch not found: " + shortPath(batch)
	}
	if !looksLikeCodegraph(batch, args[2:]) {
		return ""
	}
	return liveCmdBatchVersion(exe, batch)
}

func liveCmdBatchVersion(comspec, batch string) string {
	if comspec == "" || isCmdName(comspec) {
		if p, err := exec.LookPath("cmd.exe"); err == nil {
			comspec = p
		} else if p, err := exec.LookPath("cmd"); err == nil {
			comspec = p
		} else {
			comspec = "cmd"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, comspec, "/c", batch, "--version")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		if strings.Contains(text, "is not recognized") || strings.Contains(err.Error(), "is not recognized") {
			return "mcp cmd /c path broken — re-run tokless (" + shortPath(batch) + ")"
		}
		if isSpawnFail(err, text) {
			return "mcp batch not runnable: " + shortPath(batch)
		}
	}
	if strings.Contains(text, "is not recognized") {
		return "mcp cmd /c path broken — re-run tokless (" + shortPath(batch) + ")"
	}
	return ""
}

func liveCodegraphVersion(exe string, args []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil && isSpawnFail(err, string(out)) {
		return "mcp not runnable: " + shortPath(exe)
	}
	_ = args
	return ""
}

func looksLikeCodegraph(path string, args []string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(path, "\\", "/")))
	base = strings.TrimSuffix(base, ".cmd")
	base = strings.TrimSuffix(base, ".bat")
	base = strings.TrimSuffix(base, ".exe")
	if base == "codegraph" {
		return true
	}
	for _, a := range args {
		b := strings.ToLower(filepath.Base(strings.ReplaceAll(a, "\\", "/")))
		b = strings.TrimSuffix(b, ".cmd")
		b = strings.TrimSuffix(b, ".bat")
		if b == "codegraph" {
			return true
		}
	}
	return false
}

// --- resolve helpers ---

func resolveRunnable(exe string) string {
	if _, err := lookExe(exe); err != "" {
		return err
	}
	return ""
}

func lookExe(exe string) (string, string) {
	if exe == "" {
		return "", "empty"
	}
	// Absolute / path-like: try as-is and OS-native separators.
	if strings.ContainsAny(exe, `/\`) || filepath.IsAbs(exe) || (len(exe) >= 2 && exe[1] == ':') {
		for _, cand := range pathCandidates(exe) {
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand, ""
			}
		}
		return "", "not found"
	}
	if p, err := exec.LookPath(exe); err == nil {
		return p, ""
	}
	return "", "not on PATH"
}

func pathCandidates(exe string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(exe)
	add(filepath.Clean(exe))
	add(strings.ReplaceAll(exe, "/", `\`))
	add(strings.ReplaceAll(exe, `\`, "/"))
	if runtime.GOOS == "windows" || util.IsWin {
		add(filepath.FromSlash(strings.ReplaceAll(exe, `\`, "/")))
	}
	return out
}

func fileExistsAny(p string) bool {
	for _, cand := range pathCandidates(p) {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func isCmdName(s string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(s, "\\", "/")))
	return base == "cmd" || base == "cmd.exe"
}

func isSpawnFail(err error, msg string) bool {
	if err == nil {
		return false
	}
	if ee, ok := err.(*exec.Error); ok && ee.Err == exec.ErrNotFound {
		return false
	}
	low := strings.ToLower(msg + err.Error())
	return strings.Contains(low, "executable file not found") ||
		strings.Contains(low, "no such file") ||
		strings.Contains(low, "cannot find the file") ||
		strings.Contains(low, "the system cannot find")
}

func shortPath(p string) string {
	if len(p) <= 64 {
		return p
	}
	return p[:28] + "…" + p[len(p)-28:]
}

// --- collect from agent configs ---

func managedHookCommands(agentID string) []string {
	var raws []string
	switch agentID {
	case "claude":
		raws = append(raws, readFileOrEmpty(util.ClaudeCodePaths().Settings))
	case "codex":
		raws = append(raws, readFileOrEmpty(filepath.Join(util.CodexPathsResolved().Dir, "hooks.json")))
	case "antigravity":
		raws = append(raws, readFileOrEmpty(filepath.Join(util.Home(), ".gemini", "config", "hooks.json")))
	case "copilot":
		p := util.CopilotPathsResolved()
		raws = append(raws, readFileOrEmpty(filepath.Join(p.HooksDir, "tokless-rtk.json")))
		raws = append(raws, readFileOrEmpty(filepath.Join(p.HooksDir, "tokless-codegraph-index.json")))
		raws = append(raws, readFileOrEmpty(filepath.Join(ideRootForDoctor(), ".github", "hooks", "tokless-rtk.json")))
		raws = append(raws, readFileOrEmpty(filepath.Join(ideRootForDoctor(), ".github", "hooks", "tokless-codegraph-index.json")))
	case "droid":
		raws = append(raws, readFileOrEmpty(filepath.Join(util.Home(), ".factory", "hooks.json")))
	}
	var cmds []string
	seen := map[string]bool{}
	for _, raw := range raws {
		for _, c := range extractJSONCommands(raw) {
			fields := strings.Fields(c)
			if !isToklessManagedHook(fields) {
				continue
			}
			if seen[c] {
				continue
			}
			seen[c] = true
			cmds = append(cmds, c)
		}
	}
	return cmds
}

func ideRootForDoctor() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func managedMcpSpawns(agentID string) []util.McpSpawn {
	var out []util.McpSpawn
	switch agentID {
	case "claude":
		out = append(out, mcpFromCommandArgsFile(util.ClaudeCodePaths().GlobalJSON, "mcpServers", "codegraph")...)
	case "opencode":
		out = append(out, mcpFromCommandArrayFile(util.OpenCodePathsResolved().Config, "mcp", "codegraph")...)
	case "codex":
		out = append(out, mcpFromCodexToml("codegraph")...)
	case "antigravity":
		out = append(out, mcpFromCommandArgsFile(util.AntigravityPathsResolved().McpConfigCLI, "mcpServers", "codegraph")...)
	case "copilot":
		p := util.CopilotPathsResolved()
		out = append(out, mcpFromCommandArgsFile(p.McpConfig, "mcpServers", "codegraph")...)
	case "droid":
		out = append(out, mcpFromCommandArgsFile(filepath.Join(util.Home(), ".factory", "mcp.json"), "mcpServers", "codegraph")...)
	case "pi":
		out = append(out, mcpFromCommandArgsFile(filepath.Join(agents.PiAgentDirResolved(), "mcp.json"), "mcpServers", "codegraph")...)
	}
	return out
}

func mcpFromCommandArgsFile(path, serversKey, toolID string) []util.McpSpawn {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return nil
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return nil
	}
	sv, ok := cfg.Get(serversKey)
	if !ok {
		return nil
	}
	sm, ok := sv.(*util.OrderedMap)
	if !ok {
		return nil
	}
	ev, ok := sm.Get(toolID)
	if !ok {
		return nil
	}
	em, ok := ev.(*util.OrderedMap)
	if !ok {
		return nil
	}
	cmd, _ := em.Get("command")
	cs, _ := cmd.(string)
	if cs == "" {
		return nil
	}
	var args []string
	if av, ok := em.Get("args"); ok {
		args = anyStringSlice(av)
	}
	return []util.McpSpawn{{Command: cs, Args: args}}
}

// OpenCode stores command as a single argv array.
func mcpFromCommandArrayFile(path, mcpKey, toolID string) []util.McpSpawn {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return nil
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return nil
	}
	mv, ok := cfg.Get(mcpKey)
	if !ok {
		return nil
	}
	mm, ok := mv.(*util.OrderedMap)
	if !ok {
		return nil
	}
	ev, ok := mm.Get(toolID)
	if !ok {
		return nil
	}
	em, ok := ev.(*util.OrderedMap)
	if !ok {
		return nil
	}
	cv, ok := em.Get("command")
	if !ok {
		return nil
	}
	arr := anyStringSlice(cv)
	if len(arr) == 0 {
		return nil
	}
	return []util.McpSpawn{{Command: arr[0], Args: arr[1:]}}
}

var (
	reTomlCommand = regexp.MustCompile(`(?m)^\s*command\s*=\s*"((?:\\.|[^"\\])*)"`)
	reTomlArgs    = regexp.MustCompile(`(?m)^\s*args\s*=\s*\[([^\]]*)\]`)
	reTomlStr     = regexp.MustCompile(`"((?:\\.|[^"\\])*)"`)
)

func mcpFromCodexToml(toolID string) []util.McpSpawn {
	p := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return nil
	}
	header := "mcp_servers." + toolID
	if !util.HasBlock(raw, header) {
		return nil
	}
	start := strings.Index(raw, "["+header+"]")
	if start < 0 {
		return nil
	}
	rest := raw[start:]
	end := len(rest)
	if m := regexp.MustCompile(`\n\[`).FindStringIndex(rest[1:]); m != nil {
		end = m[0] + 1
	}
	block := rest[:end]
	cm := reTomlCommand.FindStringSubmatch(block)
	if cm == nil {
		return nil
	}
	cmd := tomlUnescape(cm[1])
	var args []string
	if am := reTomlArgs.FindStringSubmatch(block); am != nil {
		for _, sm := range reTomlStr.FindAllStringSubmatch(am[1], -1) {
			args = append(args, tomlUnescape(sm[1]))
		}
	}
	return []util.McpSpawn{{Command: cmd, Args: args}}
}

func tomlUnescape(s string) string {
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\"`, `"`)
	return s
}

func anyStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func extractJSONCommands(raw string) []string {
	if raw == "" {
		return nil
	}
	// Cheap scan: "command": "..."
	re := regexp.MustCompile(`"command"\s*:\s*"((?:\\.|[^"\\])*)"`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(raw, -1) {
		out = append(out, jsonUnescape(m[1]))
	}
	return out
}

func jsonUnescape(s string) string {
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\/`, `/`)
	return s
}

func readFileOrEmpty(path string) string {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return ""
	}
	return raw
}

// formatRuntimeIssues joins unique short details for doctor UI.
func formatRuntimeIssues(issues []runtimeIssue) string {
	if len(issues) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var parts []string
	for _, is := range issues {
		if seen[is.detail] {
			continue
		}
		seen[is.detail] = true
		parts = append(parts, is.detail)
	}
	return joinComma(parts)
}
