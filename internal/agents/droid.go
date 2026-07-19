package agents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// --- paths ---

func droidDir() string {
	return filepath.Join(util.Home(), ".factory")
}

func droidHooksFile() string {
	return filepath.Join(droidDir(), "hooks.json")
}

func droidMcpFile() string {
	return filepath.Join(droidDir(), "mcp.json")
}

func droidInstructionsFile() string {
	return filepath.Join(droidDir(), "AGENTS.md")
}

// --- detection ---

func droidKnownBinDirs() []string {
	if goosForDetect == "windows" {
		dirs := []string{
			filepath.Join(util.Home(), ".factory", "bin"),
			filepath.Join(util.Home(), ".local", "bin"),
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "factory", "bin"))
		}
		return dirs
	}
	return []string{
		filepath.Join(util.Home(), ".factory", "bin"),
		filepath.Join(util.Home(), ".local", "bin"),
	}
}

func droidDesktopPaths() []string {
	switch goosForDetect {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return []string{filepath.Join(local, "Programs", "Factory", "Factory.exe")}
		}
		return nil
	case "darwin":
		return []string{"/Applications/Factory.app"}
	default:
		return []string{"/usr/bin/factory"}
	}
}

// --- agent manifest ---

func init() {
	core.RegisterAgent(droid)
}

var droid = &core.AgentManifest{
	ID:        "droid",
	Label:     "Factory Droid",
	Homepage:  "https://factory.ai",
	CLIBin:    "droid",
	ConfigDir: func() string { return droidDir() },
	Detect: func() core.Detection {
		return detectAgent("droid", droidDir(), droidKnownBinDirs(), droidDesktopPaths())
	},
}

// --- MCP management ---

var droidEnabledTools = map[string][]string{
	"codegraph":    CodegraphDroidToolNames,
	"context-mode": ContextModeDroidToolNames,
}

func ConfigureDroidMcp(toolID string) (changed bool, file string) {
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.PickMcpSpawn("codegraph", "serve", "--mcp")
	} else {
		spawn = util.PickMcpSpawn(toolID)
	}

	f := droidMcpFile()
	_ = util.EnsureDir(filepath.Dir(f))
	raw, _ := util.ReadFileSafe(f)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers := getOrCreateMap(cfg, "mcpServers")

	entry := util.NewOrderedMap()
	entry.Set("command", spawn.Command)
	if len(spawn.Args) > 0 {
		entry.Set("args", spawn.Args)
	}
	if tools, ok := droidEnabledTools[toolID]; ok {
		entry.Set("enabledTools", tools)
	}

	if existing, ok := servers.Get(toolID); ok {
		if em, ok := existing.(*util.OrderedMap); ok {
			ec, _ := em.Get("command")
			ea, _ := em.Get("args")
			et, _ := em.Get("enabledTools")
			if ec == spawn.Command && argsEq(ea, spawn.Args) && enabledToolsEq(et, droidEnabledTools[toolID]) {
				return false, f
			}
		}
	}

	servers.Set(toolID, entry)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(f, next)
		changed = true
		file = f
	}
	return changed, file
}

// enabledToolsEq compares an enabledTools value read from JSONC (any) against
// the expected []string. Order-insensitive — Droid reads it as a set.
func enabledToolsEq(have any, want []string) bool {
	if want == nil {
		return have == nil
	}
	arr, ok := have.([]any)
	if !ok || len(arr) != len(want) {
		return false
	}
	got := make(map[string]bool, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			return false
		}
		got[s] = true
	}
	for _, w := range want {
		if !got[w] {
			return false
		}
	}
	return true
}

// RemoveDroidMcp deletes mcpServers.<toolID> from ~/.factory/mcp.json.
func RemoveDroidMcp(toolID string) bool {
	f := droidMcpFile()
	raw, ok := util.ReadFileSafe(f)
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
	_ = util.WriteFile(f, util.StringifyJSON(cfg))
	return true
}

// DroidMcpHas reports whether ~/.factory/mcp.json registers the tool.
func DroidMcpHas(toolID string) bool {
	raw, ok := util.ReadFileSafe(droidMcpFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if s, ok := cfg.Get("mcpServers"); ok {
		if sm, ok := s.(*util.OrderedMap); ok {
			_, found := sm.Get(toolID)
			return found
		}
	}
	return false
}

// Known MCP tool names per server, used to pre-populate persistentPermissions.
var ContextModeDroidToolNames = []string{
	"ctx_search", "ctx_execute", "ctx_execute_file", "ctx_batch_execute",
	"ctx_index", "ctx_fetch_and_index",
}

var CodegraphDroidToolNames = []string{
	"codegraph_explore",
}

// --- hooks management ---

// droidHooksLoad loads or creates the hooks config.
func droidHooksLoad() *util.OrderedMap {
	raw, ok := util.ReadFileSafe(droidHooksFile())
	if ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			return cfg
		}
	}
	return util.NewOrderedMap()
}

// droidHooksSave writes the hooks config if changed.
func droidHooksSave(cfg *util.OrderedMap, raw string) {
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(droidHooksFile(), next)
	}
}

// droidRawHooks returns the raw file content for diff comparison.
func droidRawHooks() string {
	raw, _ := util.ReadFileSafe(droidHooksFile())
	return raw
}

// droidAddHookGroup adds or replaces one managed hook in a top-level event array.
func droidAddHookGroup(cfg *util.OrderedMap, event string, matcher string, managedArgs []string, hookCfg *util.OrderedMap) {
	var arr []any
	if v, ok := cfg.Get(event); ok {
		arr, _ = v.([]any)
	}

	found := false
	out := make([]any, 0, len(arr)+1)
	for _, existing := range arr {
		em, ok := existing.(*util.OrderedMap)
		if !ok {
			out = append(out, existing)
			continue
		}
		emMatcher, _ := em.Get("matcher")
		hv, _ := em.Get("hooks")
		ha, _ := hv.([]any)
		if emMatcher == matcher && len(ha) > 0 {
			kept := make([]any, 0, len(ha))
			for _, h := range ha {
				hm, ok := h.(*util.OrderedMap)
				if !ok {
					kept = append(kept, h)
					continue
				}
				c, _ := hm.Get("command")
				cs, _ := c.(string)
				if !toklessManagedCommand(cs, managedArgs...) {
					kept = append(kept, h)
					continue
				}
				if !found {
					kept = append(kept, hookCfg)
					found = true
				}
			}
			if len(kept) > 0 || len(ha) == 0 {
				em.Set("hooks", kept)
				out = append(out, em)
			}
		} else {
			out = append(out, em)
		}
	}
	if !found {
		entry := util.NewOrderedMap()
		entry.Set("matcher", matcher)
		entry.Set("hooks", []any{hookCfg})
		out = append(out, entry)
	}
	cfg.Set(event, out)
}

// droidRemoveHookGroup removes managed hooks from a top-level event.
func droidRemoveHookGroup(cfg *util.OrderedMap, event string, managedArgs ...string) {
	v, ok := cfg.Get(event)
	if !ok {
		return
	}
	arr, ok := v.([]any)
	if !ok {
		return
	}
	var kept []any
	for _, g := range arr {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			kept = append(kept, g)
			continue
		}
		hv, _ := gm.Get("hooks")
		ha, _ := hv.([]any)
		keptHooks := make([]any, 0, len(ha))
		for _, h := range ha {
			hm, ok := h.(*util.OrderedMap)
			if !ok {
				keptHooks = append(keptHooks, h)
				continue
			}
			c, _ := hm.Get("command")
			cs, _ := c.(string)
			if !toklessManagedCommand(cs, managedArgs...) {
				keptHooks = append(keptHooks, h)
			}
		}
		if len(keptHooks) > 0 || len(ha) == 0 {
			gm.Set("hooks", keptHooks)
			kept = append(kept, gm)
		}
	}
	if len(kept) == 0 {
		cfg.Delete(event)
	} else {
		cfg.Set(event, kept)
	}
}

// droidHasHook checks if a hook with the given command substring exists in the event.
func droidHasHook(event string, cmdSubstring string) bool {
	cfg := droidHooksLoad()
	v, ok := cfg.Get(event)
	if !ok {
		return false
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, g := range arr {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			continue
		}
		if hv, ok := gm.Get("hooks"); ok {
			if ha, ok := hv.([]any); ok {
				for _, h := range ha {
					if hm, ok := h.(*util.OrderedMap); ok {
						if c, ok := hm.Get("command"); ok {
							if cs, ok := c.(string); ok && strings.Contains(cs, cmdSubstring) {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

// --- RTK hook ---

// InstallDroidRtkHook installs the PreToolUse hook for Execute tool.
func InstallDroidRtkHook() {
	command := toklessCommand("rtk-hook", "droid")

	raw := droidRawHooks()
	cfg := droidHooksLoad()

	hookCfg := util.NewOrderedMap()
	hookCfg.Set("type", "command")
	hookCfg.Set("command", command)
	hookCfg.Set("timeout", 10)

	droidAddHookGroup(cfg, "PreToolUse", "Execute", []string{"rtk-hook", "droid"}, hookCfg)
	droidHooksSave(cfg, raw)
}

// RemoveDroidRtkHook removes the RTK PreToolUse hook from Droid hooks.
func RemoveDroidRtkHook() {
	raw := droidRawHooks()
	cfg := droidHooksLoad()
	droidRemoveHookGroup(cfg, "PreToolUse", "rtk-hook", "droid")
	droidHooksSave(cfg, raw)
}

// HasDroidRtkHook reports whether the RTK PreToolUse hook is installed.
func HasDroidRtkHook() bool {
	return droidHasHook("PreToolUse", "rtk-hook droid")
}

// --- CodeGraph index hook ---

func InstallDroidCodegraphIndexHook() {
	command := toklessCommand("index", "--auto", "droid")

	raw := droidRawHooks()
	cfg := droidHooksLoad()

	hookCfg := util.NewOrderedMap()
	hookCfg.Set("type", "command")
	hookCfg.Set("command", command)
	hookCfg.Set("timeout", 120)

	droidAddHookGroup(cfg, "SessionStart", "", []string{"index", "--auto", "droid"}, hookCfg)
	droidHooksSave(cfg, raw)
}

func RemoveDroidCodegraphIndexHook() {
	raw := droidRawHooks()
	cfg := droidHooksLoad()
	droidRemoveHookGroup(cfg, "SessionStart", "index", "--auto", "droid")
	droidHooksSave(cfg, raw)
}

// HasDroidCodegraphIndexHook reports whether the codegraph SessionStart hook is installed.
func HasDroidCodegraphIndexHook() bool {
	return droidHasHook("SessionStart", "index --auto droid")
}

// --- Context-Mode: MCP + AGENTS.md instruction only, no PreToolUse hook (agy pattern) ---

func RemoveDroidCtxModePreToolUse() {
	raw := droidRawHooks()
	cfg := droidHooksLoad()
	droidRemoveHookGroup(cfg, "PreToolUse", "ctx-hook", "droid")
	droidHooksSave(cfg, raw)
}

func HasDroidCtxModePreToolUse() bool {
	return droidHasHook("PreToolUse", "ctx-hook droid")
}

// --- helpers ---

func argsEq(a any, b []string) bool {
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