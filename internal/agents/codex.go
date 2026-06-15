package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// --- Codex rtk PreToolUse hook ---

const (
	codexHookMatcher = "Bash"
	codexHookTimeout = 10
)

func codexHooksFile() string {
	return filepath.Join(util.CodexPathsResolved().Dir, "hooks.json")
}

// codexHookCommand is the command Codex runs for every Bash tool call.
func codexHookCommand() string {
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless" // spaced paths break cross-shell parsing; rely on PATH
	}
	return tok + " rtk-hook codex"
}

// codexHookTrustHash reproduces Codex's hook-trust hash.
func codexHookTrustHash(command string) string {
	handler := map[string]interface{}{
		"async":   false,
		"command": command,
		"timeout": codexHookTimeout,
		"type":    "command",
	}
	identity := map[string]interface{}{
		"event_name": "pre_tool_use",
		"matcher":    codexHookMatcher,
		"hooks":      []interface{}{handler},
	}
	b, _ := json.Marshal(identity)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func codexGroupHasRtk(group *util.OrderedMap) bool {
	hooksObj, ok := group.Get("hooks")
	if !ok {
		return false
	}
	arr, ok := hooksObj.([]interface{})
	if !ok {
		return false
	}
	for _, h := range arr {
		hm, ok := h.(*util.OrderedMap)
		if !ok {
			continue
		}
		if cmd, ok := hm.Get("command"); ok {
			if s, ok := cmd.(string); ok && strings.Contains(s, "rtk-hook codex") {
				return true
			}
		}
	}
	return false
}

func codexRtkGroup(command string) *util.OrderedMap {
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", command)
	hook.Set("timeout", codexHookTimeout)

	group := util.NewOrderedMap()
	group.Set("matcher", codexHookMatcher)
	group.Set("hooks", []interface{}{hook})
	return group
}

// InstallCodexRtkHook merges the rtk PreToolUse hook into ~/.codex/hooks.json.
func InstallCodexRtkHook() {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	command := codexHookCommand()

	// 1. Merge our PreToolUse "Bash" group into hooks.json.
	hooksFile := codexHooksFile()
	raw, _ := util.ReadFileSafe(hooksFile)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	var preArr []interface{}
	if v, ok := hooks.Get("PreToolUse"); ok {
		preArr, _ = v.([]interface{})
	}
	idx := -1
	for i, g := range preArr {
		if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasRtk(gm) {
			idx = i
			break
		}
	}
	group := codexRtkGroup(command)
	if idx == -1 {
		preArr = append(preArr, group)
		idx = len(preArr) - 1
	} else {
		preArr[idx] = group
	}
	hooks.Set("PreToolUse", preArr)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}

	// 2. Pre-seed trust + ensure MCP auto-approval in config.toml.
	craw, _ := util.ReadFileSafe(p.Config)
	key := hooksFile + ":pre_tool_use:" + strconv.Itoa(idx) + ":0"
	block := util.NewTomlBlock(`hooks.state."` + key + `"`)
	block.Set("trusted_hash", codexHookTrustHash(command))
	cnext := util.UpsertBlock(craw, block, false)
	cnext = util.SetTomlTopKey(cnext, "approval_policy", "never")
	if cnext != craw {
		_ = util.WriteFile(p.Config, cnext)
	}

	// 3. No rtk markdown instruction for codex — remove rtk's own RTK.md if any.
	_ = os.Remove(filepath.Join(p.Dir, "RTK.md"))
}

// RemoveCodexRtkHook removes the rtk group from hooks.json and its trust entry.
func RemoveCodexRtkHook() {
	p := util.CodexPathsResolved()
	hooksFile := codexHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			if hooks, ok := mapChild(cfg, "hooks"); ok {
				if v, ok := hooks.Get("PreToolUse"); ok {
					if preArr, ok := v.([]interface{}); ok {
						kept := preArr[:0]
						removedIdx := -1
						for i, g := range preArr {
							if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasRtk(gm) {
								removedIdx = i
								continue
							}
							kept = append(kept, g)
						}
						if removedIdx >= 0 {
							hooks.Set("PreToolUse", kept)
							_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
							craw, _ := util.ReadFileSafe(p.Config)
							key := hooksFile + ":pre_tool_use:" + strconv.Itoa(removedIdx) + ":0"
							if cnext := util.RemoveBlock(craw, `hooks.state."`+key+`"`); cnext != craw {
								_ = util.WriteFile(p.Config, cnext)
							}
						}
					}
				}
			}
		}
	}
}

// HasCodexRtkHook reports whether the rtk hook is present in hooks.json.
func HasCodexRtkHook() bool {
	raw, ok := util.ReadFileSafe(codexHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		return false
	}
	v, ok := hooks.Get("PreToolUse")
	if !ok {
		return false
	}
	preArr, ok := v.([]interface{})
	if !ok {
		return false
	}
	for _, g := range preArr {
		if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasRtk(gm) {
			return true
		}
	}
	return false
}

// mapChild fetches an OrderedMap child by key.
func mapChild(m *util.OrderedMap, key string) (*util.OrderedMap, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	om, ok := v.(*util.OrderedMap)
	return om, ok
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
