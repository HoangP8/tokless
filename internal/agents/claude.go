package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ConfigureClaudeMcp writes/updates an MCP stdio entry under ~/.claude.json.
func ConfigureClaudeMcp(toolID string) (changed bool, file string) {
	p := util.ClaudeCodePaths()
	_ = util.EnsureDir(p.Dir)
	AllowClaudeMcpTool(toolID)
	AllowClaudeMcpToolProjectLocal(toolID)
	raw, _ := util.ReadFileSafe(p.GlobalJSON)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers := getOrCreateMap(cfg, "mcpServers")

	spawn := util.SpawnForTool("claude", toolID)
	desired := util.NewOrderedMap()
	desired.Set("type", "stdio")
	desired.Set("command", spawn.Command)
	desired.Set("args", toAnySlice(spawn.Args))
	if existing, ok := servers.Get(toolID); ok {
		if claudeMcpEqual(existing, desired) {
			return false, p.GlobalJSON
		}
	}
	servers.Set(toolID, desired)
	_ = util.WriteFile(p.GlobalJSON, util.StringifyJSON(cfg))
	return true, p.GlobalJSON
}

// AllowClaudeMcpToolProjectLocal adds MCP permissions to project-local .claude/settings.local.json.
func AllowClaudeMcpToolProjectLocal(toolID string) {
	allowClaudeProjectLocalEntries(claudeMcpPermissionEntries(toolID)...)
}

func claudeMcpPermissionEntries(toolID string) []string {
	if toolID == "codegraph" {
		return []string{"mcp__codegraph__.*", "mcp__codegraph__codegraph_explore"}
	}
	if names := claudeMcpToolNames(toolID); len(names) > 0 {
		entries := make([]string, 0, len(names))
		for _, name := range names {
			entries = append(entries, "mcp__"+toolID+"__"+name)
		}
		return entries
	}
	return []string{"mcp__" + toolID + "__.*"}
}

func claudeMcpToolNames(toolID string) []string {
	switch toolID {
	case "context-mode":
		return []string{"ctx_search", "ctx_execute", "ctx_execute_file", "ctx_batch_execute", "ctx_index", "ctx_fetch_and_index"}
	case "codegraph":
		return []string{"codegraph_explore"}
	case "headroom":
		return []string{"headroom_compress", "headroom_retrieve", "headroom_stats"}
	case "projectmem":
		return []string{
			"get_instructions", "get_summary", "get_project_map", "get_plan", "precheck_file",
			"get_issue", "search_events", "get_context", "get_score", "get_global_gotchas",
			"log_issue", "record_attempt", "record_fix", "add_decision", "add_note",
		}
	}
	return nil
}

func allowClaudeProjectLocalEntries(entries ...string) {
	// Writes into the current project, not a sandboxed home — would litter the repo under test.
	if os.Getenv("TOKLESS_TEST") == "1" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	dir := filepath.Join(cwd, ".claude")
	_ = util.EnsureDir(dir)
	settingsFile := filepath.Join(dir, "settings.local.json")

	raw, _ := util.ReadFileSafe(settingsFile)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	perms := getOrCreateMap(cfg, "permissions")
	var allow []any
	seen := map[string]bool{}
	if v, ok := perms.Get("allow"); ok {
		if a, ok := v.([]any); ok {
			allow = a
			for _, x := range allow {
				if s, ok := x.(string); ok {
					seen[s] = true
				}
			}
		}
	}
	allow = removeClaudeContextModeWildcard(allow)
	seen = map[string]bool{}
	for _, x := range allow {
		if s, ok := x.(string); ok {
			seen[s] = true
		}
	}
	changed := false
	for _, entry := range entries {
		if seen[entry] {
			continue
		}
		allow = append(allow, entry)
		seen[entry] = true
		changed = true
	}
	if !changed {
		return
	}
	perms.Set("allow", allow)
	cfg.Set("permissions", perms)
	_ = util.WriteFile(settingsFile, util.StringifyJSON(cfg))
}

// AllowClaudeMcpTool auto-approves managed MCP tools.
func AllowClaudeMcpTool(toolID string) {
	p := util.ClaudeCodePaths()
	raw, _ := util.ReadFileSafe(p.Settings)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	perms := getOrCreateMap(cfg, "permissions")
	var allow []any
	if v, ok := perms.Get("allow"); ok {
		if a, ok := v.([]any); ok {
			allow = a
		}
	}
	allow = removeClaudeContextModeWildcard(allow)
	seen := make(map[string]bool, len(allow))
	for _, x := range allow {
		if s, ok := x.(string); ok {
			seen[s] = true
		}
	}
	changed := false
	for _, entry := range claudeMcpPermissionEntries(toolID) {
		if !seen[entry] {
			allow = append(allow, entry)
			changed = true
		}
	}
	if !changed && toolID != "context-mode" {
		return
	}
	perms.Set("allow", allow)
	cfg.Set("permissions", perms)
	_ = util.WriteFile(p.Settings, util.StringifyJSON(cfg))
}

func removeClaudeContextModeWildcard(entries []any) []any {
	kept := entries[:0]
	for _, entry := range entries {
		if s, ok := entry.(string); ok && s == "mcp__context-mode__.*" {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func DisallowClaudeMcpTool(toolID string) {
	p := util.ClaudeCodePaths()
	raw, ok := util.ReadFileSafe(p.Settings)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	perms, ok := cfg.Get("permissions")
	if !ok {
		return
	}
	pm, ok := perms.(*util.OrderedMap)
	if !ok {
		return
	}
	v, ok := pm.Get("allow")
	if !ok {
		return
	}
	allow, ok := v.([]any)
	if !ok {
		return
	}
	wants := map[string]bool{}
	for _, entry := range claudeMcpPermissionEntries(toolID) {
		wants[entry] = true
	}
	if toolID == "context-mode" {
		wants["mcp__context-mode__.*"] = true
	}
	kept := make([]any, 0, len(allow))
	changed := false
	for _, x := range allow {
		if s, ok := x.(string); ok && wants[s] {
			changed = true
			continue
		}
		kept = append(kept, x)
	}
	if !changed {
		return
	}
	pm.Set("allow", kept)
	_ = util.WriteFile(p.Settings, util.StringifyJSON(cfg))
}

// AllowClaudeBashPatternProjectLocal adds a Bash(specifier) entry to project-local settings.
func AllowClaudeBashPatternProjectLocal(pattern string) {
	// Same reason as allowClaudeProjectLocalEntries: writes into the current
	// project, so under test it would litter the repo.
	if os.Getenv("TOKLESS_TEST") == "1" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	dir := filepath.Join(cwd, ".claude")
	_ = util.EnsureDir(dir)
	settingsFile := filepath.Join(dir, "settings.local.json")

	raw, _ := util.ReadFileSafe(settingsFile)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	perms := getOrCreateMap(cfg, "permissions")
	var allow []any
	if v, ok := perms.Get("allow"); ok {
		if a, ok := v.([]any); ok {
			allow = a
		}
	}
	for _, x := range allow {
		if s, ok := x.(string); ok && s == pattern {
			return
		}
	}
	allow = append(allow, pattern)
	perms.Set("allow", allow)
	cfg.Set("permissions", perms)
	_ = util.WriteFile(settingsFile, util.StringifyJSON(cfg))
}

// SetClaudeEnv sets one env var Claude Code passes to its sessions. Empty
// value removes it. Returns false when nothing changed.
func SetClaudeEnv(key, value string) bool {
	p := util.ClaudeCodePaths()
	raw, _ := util.ReadFileSafe(p.Settings)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	env := getOrCreateMap(cfg, "env")
	cur, had := env.Get(key)
	switch {
	case value == "":
		if !had {
			return false
		}
		env.Delete(key)
	default:
		if s, ok := cur.(string); had && ok && s == value {
			return false
		}
		env.Set(key, value)
	}
	cfg.Set("env", env)
	_ = util.EnsureDir(p.Dir)
	_ = util.WriteFile(p.Settings, util.StringifyJSON(cfg))
	return true
}

func ClaudeEnv(key string) string {
	raw, ok := util.ReadFileSafe(util.ClaudeCodePaths().Settings)
	if !ok {
		return ""
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return ""
	}
	v, ok := cfg.Get("env")
	if !ok {
		return ""
	}
	env, ok := v.(*util.OrderedMap)
	if !ok {
		return ""
	}
	s, _ := env.Get(key)
	out, _ := s.(string)
	return out
}

// AllowClaudeBashPattern adds a Bash(specifier) entry to permissions.allow.
func AllowClaudeBashPattern(pattern string) {
	p := util.ClaudeCodePaths()
	raw, _ := util.ReadFileSafe(p.Settings)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	perms := getOrCreateMap(cfg, "permissions")
	var allow []any
	if v, ok := perms.Get("allow"); ok {
		if a, ok := v.([]any); ok {
			allow = a
		}
	}
	for _, x := range allow {
		if s, ok := x.(string); ok && s == pattern {
			return
		}
	}
	allow = append(allow, pattern)
	perms.Set("allow", allow)
	cfg.Set("permissions", perms)
	_ = util.WriteFile(p.Settings, util.StringifyJSON(cfg))
}

// DisallowClaudeBashPattern removes a Bash(specifier) entry from permissions.allow.
func DisallowClaudeBashPattern(pattern string) {
	p := util.ClaudeCodePaths()
	raw, ok := util.ReadFileSafe(p.Settings)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	perms, ok := mapChild(cfg, "permissions")
	if !ok {
		return
	}
	v, ok := perms.Get("allow")
	if !ok {
		return
	}
	arr, ok := v.([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(arr))
	dropped := false
	for _, e := range arr {
		if s, ok := e.(string); ok && s == pattern {
			dropped = true
			continue
		}
		out = append(out, e)
	}
	if !dropped {
		return
	}
	if len(out) == 0 {
		perms.Delete("allow")
	} else {
		perms.Set("allow", out)
	}
	_ = util.WriteFile(p.Settings, util.StringifyJSON(cfg))
}

func RemoveClaudeMcp(toolID string) bool {
	removed := util.RemoveMcpEntry(util.ClaudeCodePaths().GlobalJSON, "mcpServers", toolID)
	DisallowClaudeMcpTool(toolID)
	return removed
}

// claudeMcpEqual compares command/args/env by canonical JSON.
func claudeMcpEqual(existing any, desired *util.OrderedMap) bool {
	em, ok := existing.(*util.OrderedMap)
	if !ok {
		return false
	}
	cmdA, _ := em.Get("command")
	cmdB, _ := desired.Get("command")
	if jsonStr(cmdA) != jsonStr(cmdB) {
		return false
	}
	argsA, _ := em.Get("args")
	argsB, _ := desired.Get("args")
	if jsonStr(orEmptyArr(argsA)) != jsonStr(orEmptyArr(argsB)) {
		return false
	}
	envA, _ := em.Get("env")
	envB, _ := desired.Get("env")
	return jsonStr(orEmptyObj(envA)) == jsonStr(orEmptyObj(envB))
}

func claudeKnownBinDirs() []string {
	return []string{filepath.Join(util.Home(), ".local", "bin")}
}

var goosForDetect = runtime.GOOS

func claudeDesktopPaths() []string {
	switch goosForDetect {
	case "windows":
		var paths []string
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			paths = append(paths, filepath.Join(local, "AnthropicClaude", "claude.exe"))
		}
		if roam := os.Getenv("APPDATA"); roam != "" {
			paths = append(paths, filepath.Join(roam, "Claude", "claude.exe"))
		}
		return paths
	case "darwin":
		return []string{"/Applications/Claude.app"}
	default:
		return nil
	}
}

var claude = &core.AgentManifest{
	ID:        "claude",
	Label:     "Claude Code",
	Homepage:  "https://github.com/anthropics/claude-code",
	CLIBin:    "claude",
	ConfigDir: func() string { return util.ClaudeCodePaths().Dir },
	Detect: func() core.Detection {
		return detectAgent("claude", util.ClaudeCodePaths().Dir, claudeKnownBinDirs(), claudeDesktopPaths())
	},
}

func detectAgent(cli, configDir string, knownDirs []string, desktopPaths []string) core.Detection {
	hasCLI := util.FindBinary(cli, knownDirs) != ""
	hasDesktop := false
	for _, p := range desktopPaths {
		if util.Exists(p) {
			hasDesktop = true
			break
		}
	}
	switch {
	case hasCLI && hasDesktop:
		return core.Detection{Installed: true, Source: "cli+desktop"}
	case hasCLI:
		return core.Detection{Installed: true, Source: "cli"}
	case hasDesktop:
		return core.Detection{Installed: true, Source: "desktop"}
	}
	if os.Getenv("TOKLESS_TEST") == "1" && util.Exists(configDir) {
		return core.Detection{Installed: true, Source: "config"}
	}
	return core.Detection{Installed: false, Source: ""}
}

// shared helpers

func getOrCreateMap(m *util.OrderedMap, key string) *util.OrderedMap {
	if v, ok := m.Get(key); ok {
		if om, ok := v.(*util.OrderedMap); ok {
			return om
		}
	}
	om := util.NewOrderedMap()
	m.Set(key, om)
	return om
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func orEmptyArr(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func orEmptyObj(v any) any {
	if v == nil {
		return map[string]any{}
	}
	return v
}
