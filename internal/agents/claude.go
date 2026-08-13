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

	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("claude", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.McpSpawnFor(toolID)
	}
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
	case "headroom":
		return []string{"headroom_compress", "headroom_retrieve"}
	case "codegraph":
		return []string{"codegraph_explore"}
	}
	return nil
}

func allowClaudeProjectLocalEntries(entries ...string) {
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
	p := util.ClaudeCodePaths()
	removed := false
	if raw, ok := util.ReadFileSafe(p.GlobalJSON); ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			if servers, ok := cfg.Get("mcpServers"); ok {
				if sm, ok := servers.(*util.OrderedMap); ok {
					if _, has := sm.Get(toolID); has {
						sm.Delete(toolID)
						_ = util.WriteFile(p.GlobalJSON, util.StringifyJSON(cfg))
						removed = true
					}
				}
			}
		}
	}
	DisallowClaudeMcpTool(toolID)
	return removed
}

// --- Claude headroom HTTP proxy ---

const claudeProxyEnvKey = "ANTHROPIC_BASE_URL"

func ConfigureClaudeProxy() (changed bool, file string) {
	url := ProxyEndpointFor("claude")
	p := util.ClaudeCodePaths()
	_ = util.EnsureDir(p.Dir)
	raw, ok := util.ReadFileSafe(p.Settings)
	if util.HasJSONCComments(raw) {
		return false, p.Settings
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if ok {
			return false, p.Settings
		}
		cfg = util.NewOrderedMap()
	}
	env := util.NewOrderedMap()
	if v, ok := cfg.Get("env"); ok {
		em, isMap := v.(*util.OrderedMap)
		if !isMap {
			return false, p.Settings
		}
		env = em
	} else {
		cfg.Set("env", env)
	}
	if v, ok := env.Get(claudeProxyEnvKey); ok {
		if s, ok := v.(string); ok && s == url {
			return false, p.Settings
		}
		return false, p.Settings
	}
	env.Set(claudeProxyEnvKey, url)
	if err := util.WriteFile(p.Settings, util.StringifyJSON(cfg)); err != nil {
		return false, p.Settings
	}
	return true, p.Settings
}

// RemoveClaudeProxy deletes env.ANTHROPIC_BASE_URL only when it still equals
// the url tokless set.
func RemoveClaudeProxy() bool {
	url := ProxyEndpointFor("claude")
	p := util.ClaudeCodePaths()
	raw, ok := util.ReadFileSafe(p.Settings)
	if !ok {
		return false
	}
	if util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	env, ok := mapChild(cfg, "env")
	if !ok {
		return false
	}
	v, ok := env.Get(claudeProxyEnvKey)
	if !ok {
		return false
	}
	s, ok := v.(string)
	if !ok || s != url {
		return false
	}
	env.Delete(claudeProxyEnvKey)
	if env.Len() == 0 {
		cfg.Delete("env")
	}
	return util.WriteFile(p.Settings, util.StringifyJSON(cfg)) == nil
}

// ClaudeProxyWired reports whether ANTHROPIC_BASE_URL is set to url.
func ClaudeProxyWired() bool {
	url := ProxyEndpointFor("claude")
	raw, ok := util.ReadFileSafe(util.ClaudeCodePaths().Settings)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	env, ok := mapChild(cfg, "env")
	if !ok {
		return false
	}
	v, ok := env.Get(claudeProxyEnvKey)
	if !ok {
		return false
	}
	s, ok := v.(string)
	return ok && s == url
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
