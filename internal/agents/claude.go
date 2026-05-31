package agents

import (
	"encoding/json"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ConfigureClaudeMcp writes/updates an MCP stdio entry under ~/.claude.json.
func ConfigureClaudeMcp(toolID string) (changed bool, file string) {
	p := util.ClaudeCodePaths()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.GlobalJSON)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers := getOrCreateMap(cfg, "mcpServers")

	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.PickMcpSpawn("codegraph", "serve", "--mcp")
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

func RemoveClaudeMcp(toolID string) bool {
	p := util.ClaudeCodePaths()
	raw, ok := util.ReadFileSafe(p.GlobalJSON)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	servers, ok := cfg.Get("mcpServers")
	if !ok {
		return false
	}
	sm, ok := servers.(*util.OrderedMap)
	if !ok {
		return false
	}
	if _, ok := sm.Get(toolID); !ok {
		return false
	}
	sm.Delete(toolID)
	_ = util.WriteFile(p.GlobalJSON, util.StringifyJSON(cfg))
	return true
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

var claude = &core.AgentManifest{
	ID:        "claude",
	Label:     "Claude Code",
	Homepage:  "https://claude.com/claude-code",
	CLIBin:    "claude",
	ConfigDir: func() string { return util.ClaudeCodePaths().Dir },
	Detect: func() core.Detection {
		if util.Which("claude") != "" {
			return core.Detection{Installed: true, Source: "cli"}
		}
		if util.Exists(util.ClaudeCodePaths().Dir) {
			return core.Detection{Installed: true, Source: "config"}
		}
		return core.Detection{Installed: false, Source: ""}
	},
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
