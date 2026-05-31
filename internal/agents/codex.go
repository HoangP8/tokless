package agents

import (
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ConfigureCodexMcp upserts a [mcp_servers.<tool>] block in config.toml.
func ConfigureCodexMcp(toolID string) (changed bool, file string) {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.Config)
	block := util.NewTomlBlock("mcp_servers." + toolID)
	block.Set("command", "codegraph")
	block.Set("args", []string{"serve", "--mcp"})
	block.Set("enabled", true)
	next := util.UpsertBlock(raw, block, false)
	if next == raw {
		return false, p.Config
	}
	_ = util.WriteFile(p.Config, next)
	return true, p.Config
}

func CodexHasMcp(toolID string) bool {
	p := util.CodexPathsResolved()
	raw, _ := util.ReadFileSafe(p.Config)
	return util.HasBlock(raw, "mcp_servers."+toolID)
}

var codex = &core.AgentManifest{
	ID:        "codex",
	Label:     "Codex",
	Homepage:  "https://github.com/openai/codex",
	CLIBin:    "codex",
	ConfigDir: func() string { return util.CodexPathsResolved().Dir },
	Detect: func() core.Detection {
		if util.Which("codex") != "" {
			return core.Detection{Installed: true, Source: "cli"}
		}
		if util.Exists(util.CodexPathsResolved().Dir) {
			return core.Detection{Installed: true, Source: "config"}
		}
		return core.Detection{Installed: false, Source: ""}
	},
}

// Register wires all agents into the core registry.
func Register() {
	core.RegisterAgent(claude)
	core.RegisterAgent(opencode)
	core.RegisterAgent(codex)
}
