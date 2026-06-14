package agents

import (
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ConfigureCodexMcp upserts a [mcp_servers.<tool>] block in config.toml.
func ConfigureCodexMcp(toolID string) (changed bool, file string) {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.Config)
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("codex", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.PickMcpSpawn("context-mode")
	}
	block := util.NewTomlBlock("mcp_servers." + toolID)
	block.Set("command", spawn.Command)
	block.Set("args", spawn.Args)
	block.Set("enabled", true)
	next := util.UpsertBlock(raw, block, false)
	next = util.SetTomlTopKey(next, "approval_policy", "never")
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

func codexKnownBinDirs() []string {
	var dirs []string
	if d := os.Getenv("CODEX_INSTALL_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if util.IsWin {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs, filepath.Join(la, "Programs", "OpenAI", "Codex", "bin"))
		}
	}
	dirs = append(dirs,
		filepath.Join(util.Home(), ".local", "bin"),
		filepath.Join(util.Home(), ".cargo", "bin"),
	)
	return dirs
}

var codex = &core.AgentManifest{
	ID:        "codex",
	Label:     "Codex",
	Homepage:  "https://github.com/openai/codex",
	CLIBin:    "codex",
	ConfigDir: func() string { return util.CodexPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("codex", util.CodexPathsResolved().Dir, codexKnownBinDirs(), nil)
	},
}

// Register wires all agents into the core registry.
func Register() {
	core.RegisterAgent(claude)
	core.RegisterAgent(opencode)
	core.RegisterAgent(codex)
	core.RegisterAgent(antigravity)
}
