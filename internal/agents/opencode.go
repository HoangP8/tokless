package agents

import (
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ConfigureOpenCodeMcp writes/updates a local MCP entry in opencode config.
func ConfigureOpenCodeMcp(toolID string) (changed bool, file string) {
	p := util.OpenCodePathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.Config)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://opencode.ai/config.json")
	}
	mcp := getOrCreateMap(cfg, "mcp")

	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.PickMcpSpawn("codegraph", "serve", "--mcp")
	} else {
		spawn = util.PickMcpSpawn("context-mode")
	}
	command := append([]string{spawn.Command}, spawn.Args...)
	desired := util.NewOrderedMap()
	desired.Set("type", "local")
	desired.Set("command", toAnySlice(command))
	desired.Set("enabled", true)

	if existing, ok := mcp.Get(toolID); ok {
		if em, ok := existing.(*util.OrderedMap); ok {
			ec, _ := em.Get("command")
			if anyArrEq(ec, command) && notDisabled(em) {
				return false, p.Config
			}
		}
	}
	mcp.Set(toolID, desired)
	_ = util.WriteFile(p.Config, util.StringifyJSON(cfg))
	return true, p.Config
}

func RemoveOpenCodeMcp(toolID string) bool {
	p := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	mcpV, ok := cfg.Get("mcp")
	if !ok {
		return false
	}
	mcp, ok := mcpV.(*util.OrderedMap)
	if !ok {
		return false
	}
	if _, ok := mcp.Get(toolID); !ok {
		return false
	}
	mcp.Delete(toolID)
	_ = util.WriteFile(p.Config, util.StringifyJSON(cfg))
	return true
}

func notDisabled(m *util.OrderedMap) bool {
	if v, ok := m.Get("enabled"); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
}

func anyArrEq(a any, b []string) bool {
	arr, ok := a.([]any)
	if !ok || len(arr) != len(b) {
		return false
	}
	for i, x := range arr {
		s, ok := x.(string)
		if !ok || s != b[i] {
			return false
		}
	}
	return true
}

var opencode = &core.AgentManifest{
	ID:        "opencode",
	Label:     "OpenCode",
	Homepage:  "https://opencode.ai",
	CLIBin:    "opencode",
	ConfigDir: func() string { return util.OpenCodePathsResolved().Dir },
	Detect: func() core.Detection {
		if util.Which("opencode") != "" {
			return core.Detection{Installed: true, Source: "cli"}
		}
		if util.Exists(util.OpenCodePathsResolved().Dir) {
			return core.Detection{Installed: true, Source: "config"}
		}
		return core.Detection{Installed: false, Source: ""}
	},
}
