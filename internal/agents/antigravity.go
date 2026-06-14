package agents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// antigravityMcpFiles returns every MCP config surface agy reads.
func antigravityMcpFiles() []string {
	p := util.AntigravityPathsResolved()
	files := []string{p.McpConfig, p.McpConfigCLI, p.Settings}
	gemini := filepath.Join(util.Home(), ".gemini")
	for _, variant := range []string{"antigravity-desktop", "antigravity-cli"} {
		if d := filepath.Join(gemini, variant); util.Exists(d) {
			files = append(files, filepath.Join(d, "mcp_config.json"))
		}
	}
	return files
}

func antigravitySettingsFiles() []string {
	gemini := filepath.Join(util.Home(), ".gemini")
	files := []string{
		filepath.Join(gemini, "antigravity-cli", "settings.json"),
		filepath.Join(gemini, "antigravity-ide", "settings.json"),
	}
	if d := filepath.Join(gemini, "antigravity-desktop"); util.Exists(d) {
		files = append(files, filepath.Join(d, "settings.json"))
	}
	return files
}

func antigravityHooksFile() string {
	return filepath.Join(util.Home(), ".gemini", "config", "hooks.json")
}

func antigravityRewriteScript() string {
	return filepath.Join(util.Home(), ".gemini", "config", "tokless", "rtk-rewrite.sh")
}

func antigravityLegacyRewriteScript() string {
	return filepath.Join(util.Home(), ".gemini", "config", "tokless-rtk-rewrite.sh")
}

func getToklessAbs() string {
	exe, err := os.Executable()
	if err == nil && exe != "" {
		return exe
	}
	return "tokless"
}

// InstallAntigravityRtkHook installs the PreToolUse hook for agy.
func InstallAntigravityRtkHook() {
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless"
	}
	command := tok + " rtk-hook agy"

	_ = os.Remove(antigravityRewriteScript())
	_ = os.Remove(antigravityLegacyRewriteScript())

	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	var cfg *util.OrderedMap
	if ok {
		cfg = util.TryParseJsonc(raw)
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}

	rtkGroup := util.NewOrderedMap()

	hookCfg := util.NewOrderedMap()
	hookCfg.Set("type", "command")
	hookCfg.Set("command", command)
	hookCfg.Set("timeout", 10)

	preToolUseEntry := util.NewOrderedMap()
	preToolUseEntry.Set("matcher", "")
	preToolUseEntry.Set("hooks", []interface{}{hookCfg})

	rtkGroup.Set("PreToolUse", []interface{}{preToolUseEntry})
	rtkGroup.Set("PostToolUse", nil)
	rtkGroup.Set("PreInvocation", nil)
	rtkGroup.Set("PostInvocation", nil)
	rtkGroup.Set("Stop", nil)

	cfg.Set("rtk", rtkGroup)

	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}
}

// RemoveAntigravityRtkHook removes the PreToolUse hook for agy.
func RemoveAntigravityRtkHook() {
	_ = os.Remove(antigravityRewriteScript())
	_ = os.Remove(antigravityLegacyRewriteScript())
	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	if _, ok := cfg.Get("rtk"); ok {
		cfg.Delete("rtk")
		_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
	}
}

// HasAntigravityRtkHook reports whether the hook is installed.
func HasAntigravityRtkHook() bool {
	raw, ok := util.ReadFileSafe(antigravityHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	rtkObj, ok := cfg.Get("rtk")
	if !ok {
		return false
	}
	rtkGroup, ok := rtkObj.(*util.OrderedMap)
	if !ok {
		return false
	}
	pre, ok := rtkGroup.Get("PreToolUse")
	if !ok {
		return false
	}
	preArr, ok := pre.([]interface{})
	if !ok || len(preArr) == 0 {
		return false
	}
	entry, ok := preArr[0].(*util.OrderedMap)
	if !ok {
		return false
	}
	hooksObj, ok := entry.Get("hooks")
	if !ok {
		return false
	}
	hooksArr, ok := hooksObj.([]interface{})
	if !ok || len(hooksArr) == 0 {
		return false
	}
	hook, ok := hooksArr[0].(*util.OrderedMap)
	if !ok {
		return false
	}
	cmd, ok := hook.Get("command")
	if !ok {
		return false
	}
	cmdStr, ok := cmd.(string)
	if !ok {
		return false
	}
	return strings.Contains(cmdStr, "tokless-rtk-rewrite")
}

// allowAntigravityEntry adds a permissions.allow rule so agy auto-approves it.
func allowAntigravityEntry(entry string) {
	for _, f := range antigravitySettingsFiles() {
		_ = util.EnsureDir(filepath.Dir(f))
		raw, _ := util.ReadFileSafe(f)
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			cfg = util.NewOrderedMap()
		}
		perms := getOrCreateMap(cfg, "permissions")
		var allow []any
		if v, ok := perms.Get("allow"); ok {
			if arr, ok := v.([]any); ok {
				allow = arr
			}
		}
		has := false
		for _, e := range allow {
			if s, ok := e.(string); ok && s == entry {
				has = true
				break
			}
		}
		if !has {
			allow = append(allow, entry)
		}
		perms.Set("allow", allow)
		if next := util.StringifyJSON(cfg); next != raw {
			_ = util.WriteFile(f, next)
		}
	}
}

// removeAntigravityEntry drops a permissions.allow rule.
func removeAntigravityEntry(entry string) {
	want := entry
	for _, f := range antigravitySettingsFiles() {
		raw, ok := util.ReadFileSafe(f)
		if !ok {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		p, ok := cfg.Get("permissions")
		if !ok {
			continue
		}
		pm, ok := p.(*util.OrderedMap)
		if !ok {
			continue
		}
		v, ok := pm.Get("allow")
		if !ok {
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		out := make([]any, 0, len(arr))
		dropped := false
		for _, e := range arr {
			if s, ok := e.(string); ok && s == want {
				dropped = true
				continue
			}
			out = append(out, e)
		}
		if dropped {
			pm.Set("allow", out)
			_ = util.WriteFile(f, util.StringifyJSON(cfg))
		}
	}
}

// ConfigureAntigravityMcp upserts mcpServers.<tool> into every surface's MCP config.
func ConfigureAntigravityMcp(toolID string) (changed bool, file string) {
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("antigravity", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.PickMcpSpawn(toolID)
	}
	allowAntigravityEntry("mcp(" + toolID + "/*)")
	allowAntigravityEntry("command(rtk)")
	for _, f := range antigravityMcpFiles() {
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
		entry.Set("trust", true)
		servers.Set(toolID, entry)
		if next := util.StringifyJSON(cfg); next != raw {
			_ = util.WriteFile(f, next)
			changed = true
			file = f
		}
	}
	allowAntigravityEntry("mcp(" + toolID + "/*)")
	allowAntigravityEntry("command(rtk)")
	return changed, file
}

// AntigravityMcpHas reports whether every surface's MCP config registers the tool.
func AntigravityMcpHas(toolID string) bool {
	for _, f := range antigravityMcpFiles() {
		raw, ok := util.ReadFileSafe(f)
		if !ok {
			return false
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			return false
		}
		found := false
		if s, ok := cfg.Get("mcpServers"); ok {
			if sm, ok := s.(*util.OrderedMap); ok {
				_, found = sm.Get(toolID)
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func agyKnownBinDirs() []string {
	if goosForDetect == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return []string{filepath.Join(local, "agy", "bin")}
		}
		return nil
	}
	return []string{filepath.Join(util.Home(), ".local", "bin")}
}

// antigravityDesktopPaths probes the Antigravity desktop app and IDE.
func antigravityDesktopPaths() []string {
	switch goosForDetect {
	case "windows":
		var paths []string
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			paths = append(paths,
				filepath.Join(local, "Programs", "Antigravity", "Antigravity.exe"),
				filepath.Join(local, "Programs", "Antigravity IDE", "Antigravity IDE.exe"))
		}
		return paths
	case "darwin":
		return []string{"/Applications/Antigravity.app", "/Applications/Antigravity IDE.app"}
	default:
		return []string{"/opt/antigravity", "/opt/antigravity-ide",
			"/usr/local/bin/antigravity", "/usr/local/bin/antigravity-ide"}
	}
}

var antigravity = &core.AgentManifest{
	ID:        "antigravity",
	Label:     "Antigravity",
	Homepage:  "https://antigravity.google",
	CLIBin:    "agy",
	ConfigDir: func() string { return util.AntigravityPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("agy", util.AntigravityPathsResolved().Dir, agyKnownBinDirs(), antigravityDesktopPaths())
	},
}

// RemoveAntigravityMcp deletes mcpServers.<tool> from every surface's MCP config.
func RemoveAntigravityMcp(toolID string) {
	for _, f := range antigravityMcpFiles() {
		raw, ok := util.ReadFileSafe(f)
		if !ok {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		if s, ok := cfg.Get("mcpServers"); ok {
			if sm, ok := s.(*util.OrderedMap); ok {
				if _, has := sm.Get(toolID); has {
					sm.Delete(toolID)
					_ = util.WriteFile(f, util.StringifyJSON(cfg))
				}
			}
		}
	}
	removeAntigravityEntry("mcp(" + toolID + "/*)")
}
