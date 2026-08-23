package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func grokDir() string {
	if dir := os.Getenv("GROK_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(util.Home(), ".grok")
}

func grokConfigFile() string { return filepath.Join(grokDir(), "config.toml") }

func GrokInstructionsFile() string { return filepath.Join(grokDir(), "rules", "tokless.md") }

var grok = &core.AgentManifest{
	ID:        "grok",
	Label:     "Grok",
	Homepage:  "https://x.ai/grok",
	CLIBin:    "grok",
	ConfigDir: func() string { return grokDir() },
	Detect: func() core.Detection {
		return detectAgent("grok", grokDir(), []string{filepath.Join(grokDir(), "bin")}, nil)
	},
}

// ConfigureGrokMcp upserts a native Grok MCP server block in config.toml.
func ConfigureGrokMcp(toolID string) (changed bool, file string, err error) {
	raw, _ := util.ReadFileSafe(grokConfigFile())
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("grok", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.McpSpawnFor(toolID)
	}
	block := util.NewTomlBlock("mcp_servers." + toolID)
	block.Set("command", spawn.Command)
	block.Set("args", spawn.Args)
	block.Set("enabled", true)
	next := util.UpsertBlock(raw, block, false)
	next = grokEnableMcp(next, toolID)
	if next == raw {
		return false, grokConfigFile(), nil
	}
	if err := util.WriteFile(grokConfigFile(), next); err != nil {
		return false, grokConfigFile(), err
	}
	return true, grokConfigFile(), nil
}

func RemoveGrokMcp(toolID string) (bool, error) {
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false, nil
	}
	next := util.RemoveBlock(raw, "mcp_servers."+toolID)
	if next == raw {
		return false, nil
	}
	if err := util.WriteFile(grokConfigFile(), next); err != nil {
		return false, err
	}
	return true, nil
}

func GrokMcpHas(toolID string) bool {
	raw, ok := util.ReadFileSafe(grokConfigFile())
	command, args, enabled, ok := grokMcpFields(raw, toolID)
	return ok && enabled && !grokMcpDisabled(raw, toolID) && grokToklessCommand(command) && toolID == "codegraph" &&
		grokCodegraphArgs(args)
}

// GrokContextModeMcpHas reports whether Context Mode uses Tokless's bounded MCP proxy.
func GrokContextModeMcpHas() bool {
	raw, ok := util.ReadFileSafe(grokConfigFile())
	command, args, enabled, ok := grokMcpFields(raw, "context-mode")
	return ok && enabled && !grokMcpDisabled(raw, "context-mode") && grokToklessCommand(command) && grokContextModeArgs(args)
}

func grokMcpDisabled(raw, toolID string) bool {
	_, _, servers, ok := grokDisabledServers(raw)
	if !ok {
		return false
	}
	for _, server := range servers {
		if server == toolID {
			return true
		}
	}
	return false
}

func grokToklessCommand(command string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/")))
	return base == "tokless" || base == "tokless.exe"
}

func grokCodegraphArgs(args []string) bool {
	command, tail, ok := grokMcpCommand(args, []string{"run-mcp", "--agent", "grok"})
	if !ok {
		return false
	}
	if len(tail) == 2 {
		return grokCommandName(command, "codegraph") && tail[0] == "serve" && tail[1] == "--mcp"
	}
	return len(tail) == 4 && grokCommandName(command, "npx") && tail[0] == "--no-install" &&
		tail[1] == "@colbymchenry/codegraph" && tail[2] == "serve" && tail[3] == "--mcp"
}

func grokContextModeArgs(args []string) bool {
	command, tail, ok := grokMcpCommand(args, []string{"run-mcp", "--context-mode"})
	if !ok {
		return false
	}
	if len(tail) == 0 {
		return grokCommandName(command, "context-mode")
	}
	return len(tail) == 2 && grokCommandName(command, "npx") &&
		tail[0] == "--no-install" && tail[1] == "context-mode"
}

// grokMcpCommand accepts direct commands or Tokless's Windows cmd /c wrapper.
func grokMcpCommand(args, prefix []string) (string, []string, bool) {
	if len(args) <= len(prefix) {
		return "", nil, false
	}
	for i, want := range prefix {
		if args[i] != want {
			return "", nil, false
		}
	}
	command, tail := args[len(prefix)], args[len(prefix)+1:]
	if command == "cmd" && len(tail) >= 2 && tail[0] == "/c" {
		return tail[1], tail[2:], true
	}
	return command, tail, true
}

func grokCommandName(command, name string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/")))
	return base == name || base == name+".exe" || base == name+".cmd" || base == name+".bat"
}

// grokMcpFields reads command, args, and enabled from exactly one MCP TOML table.
func grokMcpFields(raw, name string) (string, []string, bool, bool) {
	header := "mcp_servers." + name
	inside, commandSet, argsSet, enabledSet := false, false, false, false
	var command string
	var args []string
	var enabled bool
	lines := strings.Split(raw, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if table, ok := grokTomlTable(trimmed); ok {
			if inside {
				break
			}
			inside = table == header
			continue
		}
		if !inside || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "command":
			if commandSet {
				return "", nil, false, false
			}
			var parsed bool
			command, parsed = grokTomlString(grokTomlValue(value))
			if !parsed {
				return "", nil, false, false
			}
			commandSet = true
		case "args":
			if argsSet {
				return "", nil, false, false
			}
			value, i = grokTomlArray(value, lines, i)
			var parsed bool
			args, parsed = grokTomlStrings(value)
			if !parsed {
				return "", nil, false, false
			}
			argsSet = true
		case "enabled":
			if enabledSet || grokTomlValue(value) != "true" {
				return "", nil, false, false
			}
			enabled, enabledSet = true, true
		}
	}
	return command, args, enabled, inside && commandSet && argsSet && enabledSet
}

// grokTomlArray collects a TOML string array, including native multiline args.
func grokTomlArray(value string, lines []string, i int) (string, int) {
	value = grokTomlValue(value)
	for !strings.Contains(value, "]") && i+1 < len(lines) {
		i++
		value += "\n" + grokTomlValue(lines[i])
	}
	return value, i
}

// grokEnableMcp removes one native root disabled_mcp_servers entry.
func grokEnableMcp(raw, toolID string) string {
	start, end, servers, ok := grokDisabledServers(raw)
	if !ok {
		return raw
	}
	kept := servers[:0]
	for _, server := range servers {
		if server != toolID {
			kept = append(kept, server)
		}
	}
	if len(kept) == len(servers) {
		return raw
	}
	if len(kept) == 0 {
		return raw[:start] + raw[end:]
	}
	encoded, _ := json.Marshal(kept)
	return raw[:start] + "disabled_mcp_servers = " + string(encoded) + "\n" + raw[end:]
}

// grokDisabledServers extracts only root disabled_mcp_servers, before tables.
func grokDisabledServers(raw string) (int, int, []string, bool) {
	lines := strings.SplitAfter(raw, "\n")
	offset := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found || strings.TrimSpace(key) != "disabled_mcp_servers" {
			offset += len(line)
			continue
		}
		start, end := offset, offset+len(line)
		value = grokTomlValue(value)
		for !strings.Contains(value, "]") && i+1 < len(lines) {
			i++
			value += "\n" + grokTomlValue(lines[i])
			end += len(lines[i])
		}
		value = strings.Replace(value, ",]", "]", 1)
		value = strings.Replace(value, ",\n]", "\n]", 1)
		var servers []string
		if err := json.Unmarshal([]byte(value), &servers); err != nil {
			return 0, 0, nil, false
		}
		return start, end, servers, true
	}
	return 0, 0, nil, false
}

func grokTomlTable(line string) (string, bool) {
	line = grokTomlValue(line)
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return "", false
	}
	return strings.TrimSpace(line[1 : len(line)-1]), true
}

func grokTomlValue(value string) string {
	quoted, escaped := false, false
	for i, r := range value {
		if r == '"' && !escaped {
			quoted = !quoted
		}
		if r == '#' && !quoted {
			return strings.TrimSpace(value[:i])
		}
		escaped = r == '\\' && !escaped
		if r != '\\' {
			escaped = false
		}
	}
	return strings.TrimSpace(value)
}

func grokTomlString(value string) (string, bool) {
	var parsed string
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return "", false
	}
	return parsed, true
}

func grokTomlStrings(value string) ([]string, bool) {
	var args []string
	value = strings.TrimSpace(value)
	value = strings.Replace(value, ",]", "]", 1)
	value = strings.Replace(value, ",\n]", "\n]", 1)
	if err := json.Unmarshal([]byte(value), &args); err != nil {
		return nil, false
	}
	return args, true
}

// --- rtk PreToolUse hook ---

const grokRtkHookFile = "tokless-rtk.json"
const grokCodegraphSessionHookFile = "tokless-codegraph-session.json"

type grokRtkCommandHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type grokRtkPreToolUse struct {
	Hooks []grokRtkCommandHook `json:"hooks"`
}

type grokRtkHook struct {
	Hooks struct {
		PreToolUse []grokRtkPreToolUse `json:"PreToolUse"`
	} `json:"hooks"`
}

func grokRtkHookPath() string {
	return filepath.Join(grokDir(), "hooks", grokRtkHookFile)
}

func grokCodegraphSessionHookPath() string {
	return filepath.Join(grokDir(), "hooks", grokCodegraphSessionHookFile)
}

// InstallGrokRtkHook writes ~/.grok/hooks/tokless-rtk.json.
func InstallGrokRtkHook() error {
	command := toklessCommand("rtk-hook", "grok")
	payload := grokRtkHook{}
	payload.Hooks.PreToolUse = []grokRtkPreToolUse{{Hooks: []grokRtkCommandHook{{
		Type: "command", Command: command, Timeout: 10,
	}}}}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFile(grokRtkHookPath(), string(raw)+"\n")
}

// RemoveGrokRtkHook removes ~/.grok/hooks/tokless-rtk.json.
func RemoveGrokRtkHook() {
	_ = os.Remove(grokRtkHookPath())
}

// HasGrokRtkHook reports whether the managed rtk hook file is present.
func HasGrokRtkHook() bool {
	raw, ok := util.ReadFileSafe(grokRtkHookPath())
	if !ok {
		return false
	}
	var payload grokRtkHook
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Hooks.PreToolUse) != 1 {
		return false
	}
	hooks := payload.Hooks.PreToolUse[0].Hooks
	return len(hooks) == 1 && hooks[0].Type == "command" &&
		hooks[0].Command == toklessCommand("rtk-hook", "grok") && hooks[0].Timeout == 10
}

type grokSessionStartHook struct {
	Hooks struct {
		SessionStart []grokRtkPreToolUse `json:"SessionStart"`
	} `json:"hooks"`
}

// InstallGrokCodegraphSessionHook writes the Grok SessionStart bootstrap hook.
func InstallGrokCodegraphSessionHook() error {
	command := toklessCommand("grok-hook", "session-start")
	payload := grokSessionStartHook{}
	payload.Hooks.SessionStart = []grokRtkPreToolUse{{Hooks: []grokRtkCommandHook{{
		Type: "command", Command: command, Timeout: 120,
	}}}}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFile(grokCodegraphSessionHookPath(), string(raw)+"\n")
}

// HasGrokCodegraphSessionHook validates the managed Grok SessionStart hook.
func HasGrokCodegraphSessionHook() bool {
	raw, ok := util.ReadFileSafe(grokCodegraphSessionHookPath())
	if !ok {
		return false
	}
	var payload grokSessionStartHook
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Hooks.SessionStart) != 1 {
		return false
	}
	hooks := payload.Hooks.SessionStart[0].Hooks
	return len(hooks) == 1 && hooks[0].Type == "command" &&
		hooks[0].Command == toklessCommand("grok-hook", "session-start") && hooks[0].Timeout == 120
}

// RemoveGrokCodegraphSessionHook removes the managed Grok SessionStart hook.
func RemoveGrokCodegraphSessionHook() {
	_ = os.Remove(grokCodegraphSessionHookPath())
}
