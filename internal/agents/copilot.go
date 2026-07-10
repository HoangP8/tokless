package agents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func copilotHooksFile(name string) string {
	return filepath.Join(util.CopilotPathsResolved().HooksDir, name)
}

// InstallCopilotContextModeHook writes the context-mode hook file.
func InstallCopilotContextModeHook() {
	p := util.CopilotPathsResolved()
	_ = util.EnsureDir(p.HooksDir)

	mk := func(cmd string) *util.OrderedMap {
		h := util.NewOrderedMap()
		h.Set("type", "command")
		h.Set("command", cmd)
		h.Set("timeoutSec", 10)
		e := util.NewOrderedMap()
		e.Set("hooks", []any{h})
		return e
	}
	hooks := util.NewOrderedMap()
	hooks.Set("preToolUse", []any{mk("context-mode hook copilot-cli pretooluse")})
	hooks.Set("postToolUse", []any{mk("context-mode hook copilot-cli posttooluse")})
	hooks.Set("sessionStart", []any{mk("context-mode hook copilot-cli sessionstart")})
	hooks.Set("preCompact", []any{mk("context-mode hook copilot-cli precompact")})

	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	_ = util.WriteFile(copilotHooksFile("context-mode.json"), util.StringifyJSON(root))
}

func RemoveCopilotContextModeHook() { _ = os.Remove(copilotHooksFile("context-mode.json")) }

func HasCopilotContextModeHook() bool {
	raw, ok := util.ReadFileSafe(copilotHooksFile("context-mode.json"))
	return ok && strings.Contains(raw, "context-mode hook copilot-cli")
}

func copilotRtkHookCommand() string {
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless"
	}
	return tok + " rtk-hook copilot"
}

// InstallCopilotRtkHook writes ~/.copilot/hooks/tokless-rtk.json.
// Dual events match upstream rtk: PreToolUse = VS Code Chat, preToolUse = Copilot CLI.
func InstallCopilotRtkHook() {
	p := util.CopilotPathsResolved()
	_ = util.EnsureDir(p.HooksDir)

	cmd := copilotRtkHookCommand()

	// VS Code Chat: flat {type,command,timeout} under PreToolUse (upstream rtk shape).
	ideFlat := util.NewOrderedMap()
	ideFlat.Set("type", "command")
	ideFlat.Set("command", cmd)
	ideFlat.Set("timeout", 10)

	// Copilot CLI: nested hooks[] under preToolUse with timeoutSec.
	cliHook := util.NewOrderedMap()
	cliHook.Set("type", "command")
	cliHook.Set("command", cmd)
	cliHook.Set("timeoutSec", 10)
	cliEntry := util.NewOrderedMap()
	cliEntry.Set("hooks", []any{cliHook})

	hooks := util.NewOrderedMap()
	hooks.Set("PreToolUse", []any{ideFlat})
	hooks.Set("preToolUse", []any{cliEntry})

	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	_ = util.WriteFile(copilotHooksFile("tokless-rtk.json"), util.StringifyJSON(root))
}

func RemoveCopilotRtkHook() { _ = os.Remove(copilotHooksFile("tokless-rtk.json")) }

// InstallCopilotCodegraphIndexHook writes a postToolUse hook that keeps the
// per-project codegraph index in sync after each tool call (mirrors agy).
func InstallCopilotCodegraphIndexHook() {
	p := util.CopilotPathsResolved()
	_ = util.EnsureDir(p.HooksDir)
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless"
	}
	cmd := tok + " copilot-hook codegraph-index"
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", cmd)
	hook.Set("timeoutSec", 120)
	entry := util.NewOrderedMap()
	entry.Set("hooks", []any{hook})
	hooks := util.NewOrderedMap()
	hooks.Set("postToolUse", []any{entry})
	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	_ = util.WriteFile(copilotHooksFile("tokless-codegraph-index.json"), util.StringifyJSON(root))
}

func RemoveCopilotCodegraphIndexHook() { _ = os.Remove(copilotHooksFile("tokless-codegraph-index.json")) }

func HasCopilotCodegraphIndexHook() bool {
	raw, ok := util.ReadFileSafe(copilotHooksFile("tokless-codegraph-index.json"))
	return ok && strings.Contains(raw, "copilot-hook codegraph-index")
}

func HasCopilotRtkHook() bool {
	raw, ok := util.ReadFileSafe(copilotHooksFile("tokless-rtk.json"))
	return ok && strings.Contains(raw, "rtk-hook copilot")
}

// ConfigureCopilotMcp upserts a tool entry in ~/.copilot/mcp-config.json.
func ConfigureCopilotMcp(toolID string) (changed bool, file string) {
	p := util.CopilotPathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.McpConfig)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers := getOrCreateMap(cfg, "mcpServers")

	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.PickMcpSpawn("codegraph", "serve", "--mcp")
	} else {
		spawn = util.PickMcpSpawn(toolID)
	}
	desired := util.NewOrderedMap()
	desired.Set("type", "local")
	desired.Set("command", spawn.Command)
	desired.Set("args", toAnySlice(spawn.Args))
	desired.Set("tools", []any{"*"})

	if existing, ok := servers.Get(toolID); ok && claudeMcpEqual(existing, desired) {
		return false, p.McpConfig
	}
	servers.Set(toolID, desired)
	_ = util.WriteFile(p.McpConfig, util.StringifyJSON(cfg))
	return true, p.McpConfig
}

// RemoveCopilotMcp deletes a tool entry from ~/.copilot/mcp-config.json.
func RemoveCopilotMcp(toolID string) bool {
	p := util.CopilotPathsResolved()
	raw, ok := util.ReadFileSafe(p.McpConfig)
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
	if _, has := sm.Get(toolID); !has {
		return false
	}
	sm.Delete(toolID)
	_ = util.WriteFile(p.McpConfig, util.StringifyJSON(cfg))
	return true
}

// CopilotMcpHas reports whether toolID is registered in copilot's MCP config.
func CopilotMcpHas(toolID string) bool {
	p := util.CopilotPathsResolved()
	raw, ok := util.ReadFileSafe(p.McpConfig)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if s, ok := cfg.Get("mcpServers"); ok {
		if sm, ok := s.(*util.OrderedMap); ok {
			_, has := sm.Get(toolID)
			return has
		}
	}
	return false
}

func copilotKnownBinDirs() []string {
	dirs := []string{
		filepath.Join(util.Home(), ".local", "bin"),
		"/usr/local/bin",
	}
	if util.IsWin {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs, filepath.Join(la, "Microsoft", "WinGet", "Links"))
		}
	}
	return dirs
}

var copilot = &core.AgentManifest{
	ID:        "copilot",
	Label:     "GitHub Copilot",
	Homepage:  "https://github.com/github/copilot-cli",
	CLIBin:    "copilot",
	ConfigDir: func() string { return util.CopilotPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("copilot", util.CopilotPathsResolved().Dir, copilotKnownBinDirs(), nil)
	},
}
