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

// IDE (VS Code) paths — project-scoped, written relative to cwd.

func copilotIdeHooksDir() string  { return filepath.Join(".", ".github", "hooks") }
func copilotIdeHooksFile(name string) string {
	return filepath.Join(copilotIdeHooksDir(), name)
}
func copilotIdeMcpFile() string          { return filepath.Join(".", ".vscode", "mcp.json") }
func copilotIdeInstructionsFile() string { return filepath.Join(".", ".github", "copilot-instructions.md") }

// InstallCopilotContextModeHook writes the context-mode hook file.
func InstallCopilotContextModeHook() {
	p := util.CopilotPathsResolved()
	_ = util.EnsureDir(p.HooksDir)

	events := []struct{ event, token string }{
		{"preToolUse", "pretooluse"},
		{"postToolUse", "posttooluse"},
		{"sessionStart", "sessionstart"},
		{"userPromptSubmitted", "userpromptsubmit"},
		{"agentStop", "stop"},
		{"preCompact", "precompact"},
	}
	hooks := util.NewOrderedMap()
	for _, e := range events {
		h := util.NewOrderedMap()
		h.Set("type", "command")
		h.Set("command", "context-mode hook copilot-cli "+e.token)
		hooks.Set(e.event, []any{h})
	}

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

// InstallCopilotIdeContextModeHook writes IDE context-mode hooks (.github/hooks/).
func InstallCopilotIdeContextModeHook() {
	_ = util.EnsureDir(copilotIdeHooksDir())
	events := []string{"PreToolUse", "PostToolUse", "SessionStart", "Stop"}
	tokens := []string{"pretooluse", "posttooluse", "sessionstart", "stop"}
	hooks := util.NewOrderedMap()
	for i, ev := range events {
		h := util.NewOrderedMap()
		h.Set("type", "command")
		h.Set("command", "context-mode hook copilot-vscode "+tokens[i])
		h.Set("timeout", 10)
		hooks.Set(ev, []any{h})
	}
	root := util.NewOrderedMap()
	root.Set("hooks", hooks)
	_ = util.WriteFile(copilotIdeHooksFile("context-mode.json"), util.StringifyJSON(root))
}

func RemoveCopilotIdeContextModeHook() { _ = os.Remove(copilotIdeHooksFile("context-mode.json")) }

func HasCopilotIdeContextModeHook() bool {
	raw, ok := util.ReadFileSafe(copilotIdeHooksFile("context-mode.json"))
	return ok && strings.Contains(raw, "context-mode hook copilot-vscode")
}

func copilotRtkHookCommand() string {
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless"
	}
	return tok + " rtk-hook copilot"
}

// InstallCopilotRtkHook writes ~/.copilot/hooks/tokless-rtk.json.
func InstallCopilotRtkHook() {
	p := util.CopilotPathsResolved()
	_ = util.EnsureDir(p.HooksDir)

	cmd := copilotRtkHookCommand()

	// Flat format: {type,command,timeout} — used by both Copilot CLI and VS Code.
	flat := func() *util.OrderedMap {
		h := util.NewOrderedMap()
		h.Set("type", "command")
		h.Set("command", cmd)
		h.Set("timeout", 10)
		return h
	}

	hooks := util.NewOrderedMap()
	hooks.Set("PreToolUse", []any{flat()})
	hooks.Set("preToolUse", []any{flat()})
	hooks.Set("PostToolUse", []any{flat()})
	hooks.Set("postToolUse", []any{flat()})

	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	_ = util.WriteFile(copilotHooksFile("tokless-rtk.json"), util.StringifyJSON(root))
	EnsureCopilotRtkCommandApproval()
}

func RemoveCopilotRtkHook() { _ = os.Remove(copilotHooksFile("tokless-rtk.json")) }

// InstallCopilotIdeRtkHook writes .github/hooks/tokless-rtk.json for VS Code IDE.
func InstallCopilotIdeRtkHook() {
	_ = util.EnsureDir(copilotIdeHooksDir())
	cmd := copilotRtkHookCommand()
	entry := util.NewOrderedMap()
	entry.Set("type", "command")
	entry.Set("command", cmd)
	entry.Set("timeout", 10)

	hooks := util.NewOrderedMap()
	hooks.Set("PreToolUse", []any{entry})
	hooks.Set("PostToolUse", []any{entry})
	root := util.NewOrderedMap()
	root.Set("hooks", hooks)
	_ = util.WriteFile(copilotIdeHooksFile("tokless-rtk.json"), util.StringifyJSON(root))
}

func RemoveCopilotIdeRtkHook() { _ = os.Remove(copilotIdeHooksFile("tokless-rtk.json")) }

func HasCopilotIdeRtkHook() bool {
	raw, ok := util.ReadFileSafe(copilotIdeHooksFile("tokless-rtk.json"))
	return ok && strings.Contains(raw, "rtk-hook copilot")
}

func copilotPermissionsFile() string {
	return filepath.Join(util.CopilotPathsResolved().Dir, "permissions-config.json")
}

// EnsureCopilotRtkCommandApproval merges kind=commands commandIdentifiers=["rtk"]
// into every existing location in permissions-config.json.
func EnsureCopilotRtkCommandApproval() {
	path := copilotPermissionsFile()
	raw, _ := util.ReadFileSafe(path)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	locs, ok := mapChild(cfg, "locations")
	if !ok {
		return
	}
	changed := false
	for _, key := range locs.Keys() {
		locRaw, _ := locs.Get(key)
		loc, ok := locRaw.(*util.OrderedMap)
		if !ok {
			continue
		}
		var approvals []any
		if v, ok := loc.Get("tool_approvals"); ok {
			approvals, _ = v.([]any)
		}
		if copilotApprovalsHasRtk(approvals) {
			continue
		}
		entry := util.NewOrderedMap()
		entry.Set("kind", "commands")
		entry.Set("commandIdentifiers", []any{"rtk"})
		approvals = append(approvals, entry)
		loc.Set("tool_approvals", approvals)
		changed = true
	}
	if !changed {
		return
	}
	_ = util.WriteFile(path, util.StringifyJSON(cfg))
}

func copilotApprovalsHasRtk(approvals []any) bool {
	for _, a := range approvals {
		m, ok := a.(*util.OrderedMap)
		if !ok {
			if mm, ok := a.(map[string]any); ok {
				if kind, _ := mm["kind"].(string); kind != "commands" {
					continue
				}
				ids, _ := mm["commandIdentifiers"].([]any)
				for _, id := range ids {
					if s, ok := id.(string); ok && s == "rtk" {
						return true
					}
				}
			}
			continue
		}
		kind, _ := m.Get("kind")
		if ks, _ := kind.(string); ks != "commands" {
			continue
		}
		idsRaw, _ := m.Get("commandIdentifiers")
		ids, _ := idsRaw.([]any)
		for _, id := range ids {
			if s, ok := id.(string); ok && s == "rtk" {
				return true
			}
		}
	}
	return false
}

// InstallCopilotCodegraphIndexHook writes a sessionStart hook that syncs the
// per-project codegraph index once when a Copilot CLI session begins.
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
	hooks := util.NewOrderedMap()
	hooks.Set("sessionStart", []any{hook})
	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	_ = util.WriteFile(copilotHooksFile("tokless-codegraph-index.json"), util.StringifyJSON(root))
}

func RemoveCopilotCodegraphIndexHook() {
	_ = os.Remove(copilotHooksFile("tokless-codegraph-index.json"))
}

// InstallCopilotIdeCodegraphIndexHook writes .github/hooks/tokless-codegraph-index.json for VS Code.
func InstallCopilotIdeCodegraphIndexHook() {
	_ = util.EnsureDir(copilotIdeHooksDir())
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless"
	}
	cmd := tok + " copilot-hook codegraph-index"
	entry := util.NewOrderedMap()
	entry.Set("type", "command")
	entry.Set("command", cmd)
	entry.Set("timeout", 120)
	hooks := util.NewOrderedMap()
	hooks.Set("PostToolUse", []any{entry})
	root := util.NewOrderedMap()
	root.Set("hooks", hooks)
	_ = util.WriteFile(copilotIdeHooksFile("tokless-codegraph-index.json"), util.StringifyJSON(root))
}

func RemoveCopilotIdeCodegraphIndexHook() {
	_ = os.Remove(copilotIdeHooksFile("tokless-codegraph-index.json"))
}

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
	if toolID == "context-mode" {
		env := util.NewOrderedMap()
		env.Set("CONTEXT_MODE_PLATFORM", "copilot-cli")
		env.Set("CONTEXT_MODE_COPILOT_PLUGIN", "1")
		desired.Set("env", env)
	}

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

// --- IDE (VS Code) MCP: .vscode/mcp.json ---

func ConfigureCopilotIdeMcp(toolID string) (changed bool, file string) {
	path := copilotIdeMcpFile()
	_ = util.EnsureDir(filepath.Dir(path))
	raw, _ := util.ReadFileSafe(path)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers := getOrCreateMap(cfg, "servers")

	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.PickMcpSpawn("codegraph", "serve", "--mcp")
	} else {
		spawn = util.PickMcpSpawn(toolID)
	}
	desired := util.NewOrderedMap()
	desired.Set("type", "stdio")
	desired.Set("command", spawn.Command)
	desired.Set("args", toAnySlice(spawn.Args))
	if toolID == "context-mode" {
		env := util.NewOrderedMap()
		env.Set("CONTEXT_MODE_PLATFORM", "copilot-vscode")
		env.Set("CONTEXT_MODE_COPILOT_PLUGIN", "1")
		desired.Set("env", env)
	}

	if existing, ok := servers.Get(toolID); ok && claudeMcpEqual(existing, desired) {
		return false, path
	}
	servers.Set(toolID, desired)
	_ = util.WriteFile(path, util.StringifyJSON(cfg))
	return true, path
}

func RemoveCopilotIdeMcp(toolID string) bool {
	path := copilotIdeMcpFile()
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	servers, ok := cfg.Get("servers")
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
	_ = util.WriteFile(path, util.StringifyJSON(cfg))
	return true
}

func CopilotIdeMcpHas(toolID string) bool {
	raw, ok := util.ReadFileSafe(copilotIdeMcpFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if s, ok := cfg.Get("servers"); ok {
		if sm, ok := s.(*util.OrderedMap); ok {
			_, has := sm.Get(toolID)
			return has
		}
	}
	return false
}

// WriteCopilotIdeInstructions writes the TOKLESS body to .github/copilot-instructions.md.
func WriteCopilotIdeInstructions(owners []string) {
	path := copilotIdeInstructionsFile()
	_ = util.EnsureDir(filepath.Dir(path))
	body := util.ToklessAgentBody(owners)
	_ = util.WriteFile(path, body+"\n")
}

func RemoveCopilotIdeInstructions() {
	path := copilotIdeInstructionsFile()
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return
	}
	if strings.TrimSpace(raw) == "" || !strings.Contains(raw, "## Principles") {
		return
	}
	_ = os.Remove(path)
}

// SyncCopilotIdeInstructions copies the CLI merged instruction body to the IDE file.
func SyncCopilotIdeInstructions() {
	cliPath := util.CopilotPathsResolved().Instructions
	body, ok := util.ReadFileSafe(cliPath)
	if !ok || strings.TrimSpace(body) == "" {
		return
	}
	path := copilotIdeInstructionsFile()
	_ = util.EnsureDir(filepath.Dir(path))
	_ = util.WriteFile(path, strings.TrimRight(body, "\n")+"\n")
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
