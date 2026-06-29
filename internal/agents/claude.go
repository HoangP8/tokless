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
		spawn = util.PickMcpSpawn("context-mode")
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

// AllowClaudeMcpTool auto-approves every tool of an MCP server.
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
	entry := "mcp__" + toolID + "__.*"
	for _, x := range allow {
		if s, ok := x.(string); ok && s == entry {
			return
		}
	}
	allow = append(allow, entry)
	perms.Set("allow", allow)
	cfg.Set("permissions", perms)
	_ = util.WriteFile(p.Settings, util.StringifyJSON(cfg))
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
	want := "mcp__" + toolID + "__.*"
	kept := make([]any, 0, len(allow))
	changed := false
	for _, x := range allow {
		if s, ok := x.(string); ok && s == want {
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

func ensureClaudeSkillDir() string {
	p := util.ClaudeCodePaths()
	_ = util.EnsureDir(p.SkillsDir)
	return p.SkillsDir
}

func LocateClaudeCaveman() string {
	return filepath.Join(ensureClaudeSkillDir(), "caveman")
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
