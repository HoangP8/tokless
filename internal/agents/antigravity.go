package agents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func antigravityMcpConfigFile() string {
	return util.AntigravityPathsResolved().McpConfigCLI
}

func antigravityLegacyMcpFiles() []string {
	p := util.AntigravityPathsResolved()
	gemini := filepath.Join(util.Home(), ".gemini")
	files := []string{p.McpConfig, p.Settings, filepath.Join(gemini, "antigravity-cli", "mcp_config.json")}
	for _, variant := range []string{"antigravity-ide"} {
		if d := filepath.Join(gemini, variant); util.Exists(d) {
			files = append(files, filepath.Join(d, "mcp_config.json"))
		}
	}
	return files
}

func antigravitySettingsFiles() []string {
	gemini := filepath.Join(util.Home(), ".gemini")
	return []string{
		filepath.Join(gemini, "antigravity-cli", "settings.json"),
		filepath.Join(gemini, "antigravity-ide", "settings.json"),
	}
}

func antigravityDeadGuiSettingsFiles() []string {
	gemini := filepath.Join(util.Home(), ".gemini")
	return []string{
		filepath.Join(gemini, "antigravity-desktop", "settings.json"),
	}
}

// cleanAntigravityDeadGuiSettings removes permissions from desktop settings
func cleanAntigravityDeadGuiSettings() {
	for _, f := range antigravityDeadGuiSettingsFiles() {
		raw, ok := util.ReadFileSafe(f)
		if !ok {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		if _, ok := cfg.Get("permissions"); !ok {
			continue
		}
		cfg.Delete("permissions")
		_ = util.WriteFile(f, util.StringifyJSON(cfg))
	}
}

func antigravityHooksFile() string {
	return filepath.Join(util.Home(), ".gemini", "config", "hooks.json")
}

// HasAntigravityContextModeHook checks the minimal PreToolUse hook.
func HasAntigravityContextModeHook() bool {
	raw, ok := util.ReadFileSafe(antigravityHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	groupObj, ok := cfg.Get("context-mode")
	if !ok {
		return false
	}
	group, ok := groupObj.(*util.OrderedMap)
	if !ok {
		return false
	}
	pre, ok := group.Get("PreToolUse")
	if !ok {
		return false
	}
	arr, ok := pre.([]interface{})
	if !ok || len(arr) == 0 {
		return false
	}
	entry, ok := arr[0].(*util.OrderedMap)
	if !ok {
		return false
	}
	matcher, ok := entry.Get("matcher")
	if !ok || matcher != "run_command|view_file|grep_search|web_fetch|read_url_content" {
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
	return cmdStr == "context-mode hook antigravity-cli pretooluse"
}

// InstallAntigravityContextModeHook installs a minimal PreToolUse hook.
func InstallAntigravityContextModeHook() {
	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	var cfg *util.OrderedMap
	if ok {
		cfg = util.TryParseJsonc(raw)
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}

	hookCfg := util.NewOrderedMap()
	hookCfg.Set("type", "command")
	hookCfg.Set("command", "context-mode hook antigravity-cli pretooluse")
	hookCfg.Set("timeout", 10)

	entry := util.NewOrderedMap()
	entry.Set("matcher", "run_command|view_file|grep_search|web_fetch|read_url_content")
	entry.Set("hooks", []interface{}{hookCfg})

	group := util.NewOrderedMap()
	group.Set("PreToolUse", []interface{}{entry})

	cfg.Set("context-mode", group)

	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}
}

// RemoveAntigravityContextModeHook removes the context-mode hook group.
func RemoveAntigravityContextModeHook() {
	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	if _, ok := cfg.Get("context-mode"); ok {
		cfg.Delete("context-mode")
		if cfg.Len() == 0 {
			_ = os.Remove(hooksFile)
			return
		}
		_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
	}
}

// CleanupLegacyAntigravityContextMode removes legacy context-mode hook surfaces,
// keeping the exact new minimal PreToolUse command intact.
func CleanupLegacyAntigravityContextMode() {
	_ = os.RemoveAll(filepath.Join(util.Home(), ".gemini", "config", "plugins", "context-mode"))

	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	changed := false
	if _, ok := cfg.Get("ctx"); ok {
		cfg.Delete("ctx")
		changed = true
	}
	if _, ok := cfg.Get("tokless-context-mode"); ok {
		cfg.Delete("tokless-context-mode")
		changed = true
	}
	for _, groupName := range append([]string(nil), cfg.Keys()...) {
		groupObj, ok := cfg.Get(groupName)
		if !ok {
			continue
		}
		group, ok := groupObj.(*util.OrderedMap)
		if !ok {
			continue
		}
		for _, event := range append([]string(nil), group.Keys()...) {
			v, ok := group.Get(event)
			if !ok {
				continue
			}
			entries, ok := v.([]interface{})
			if !ok {
				continue
			}
			kept := filterContextModeHookEntries(entries)
			if len(kept) != len(entries) {
				changed = true
				if len(kept) == 0 {
					group.Delete(event)
				} else {
					group.Set(event, kept)
				}
			}
		}
		if group.Len() == 0 {
			cfg.Delete(groupName)
			changed = true
		}
	}
	if !changed {
		return
	}
	if cfg.Len() == 0 {
		_ = os.Remove(hooksFile)
		return
	}
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}
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
	cleanAntigravityDeadGuiSettings()
	RemoveAntigravityCodegraphToolDefs()
	cleanAntigravityDeadHookGroups()
	AllowAntigravityEntry("command(rtk )")

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

	cfg.Set("rtk", rtkGroup)

	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}
}

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
	return strings.Contains(cmdStr, "rtk-hook agy")
}

const antigravityContextModeHookCommand = "context-mode hook antigravity-cli pretooluse"

func filterContextModeHookEntries(arr []interface{}) []interface{} {
	var kept []interface{}
	for _, e := range arr {
		em, ok := e.(*util.OrderedMap)
		if !ok {
			kept = append(kept, e)
			continue
		}
		hv, ok := em.Get("hooks")
		if !ok {
			kept = append(kept, e)
			continue
		}
		ha, ok := hv.([]interface{})
		if !ok {
			kept = append(kept, e)
			continue
		}
		drop := false
		for _, h := range ha {
			hm, ok := h.(*util.OrderedMap)
			if !ok {
				continue
			}
			if c, ok := hm.Get("command"); ok {
				if s, ok := c.(string); ok && strings.Contains(s, "context-mode") {
					drop = true
					break
				}
			}
		}
		if !drop {
			kept = append(kept, e)
		}
	}
	return kept
}

var codegraphToolDefFiles = []string{
	"codegraph_explore.json",
	"codegraph_node.json",
	"codegraph_search.json",
	"codegraph_callers.json",
	"instructions.md",
}

// RemoveAntigravityCodegraphToolDefs deletes the static codegraph tool definitions.
func RemoveAntigravityCodegraphToolDefs() {
	gemini := filepath.Join(util.Home(), ".gemini")
	for _, variant := range []string{"antigravity-cli", "antigravity-ide"} {
		toolDir := filepath.Join(gemini, variant, "mcp", "codegraph")
		for _, file := range codegraphToolDefFiles {
			_ = os.Remove(filepath.Join(toolDir, file))
		}
		_ = os.Remove(toolDir)
	}
}

// HasAntigravityCodegraphIndexHook reports whether the codegraph index hook is installed.
func HasAntigravityCodegraphIndexHook() bool {
	raw, ok := util.ReadFileSafe(antigravityHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	groupObj, ok := cfg.Get("tokless-codegraph-index")
	if !ok {
		return false
	}
	group, ok := groupObj.(*util.OrderedMap)
	if !ok {
		return false
	}
	for _, ev := range []string{"PostToolUse", "PreInvocation"} {
		if hasCodegraphIndexEntry(group, ev) {
			return true
		}
	}
	return false
}

func hasCodegraphIndexEntry(group *util.OrderedMap, event string) bool {
	v, ok := group.Get(event)
	if !ok {
		return false
	}
	arr, ok := v.([]interface{})
	if !ok || len(arr) == 0 {
		return false
	}
	entry, ok := arr[0].(*util.OrderedMap)
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
	return strings.Contains(cmdStr, "agy-hook codegraph-index")
}

// RemoveAntigravityCodegraphIndexHook removes the codegraph index hook group from hooks.json.
func RemoveAntigravityCodegraphIndexHook() {
	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	if _, ok := cfg.Get("tokless-codegraph-index"); ok {
		cfg.Delete("tokless-codegraph-index")
		_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
	}
}

// InstallAntigravityCodegraphIndexHook installs hooks for codegraph auto-init.
func InstallAntigravityCodegraphIndexHook() {
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless"
	}
	command := tok + " agy-hook codegraph-index"

	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	var cfg *util.OrderedMap
	if ok {
		cfg = util.TryParseJsonc(raw)
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}

	group := util.NewOrderedMap()

	hookCfg := util.NewOrderedMap()
	hookCfg.Set("type", "command")
	hookCfg.Set("command", command)
	hookCfg.Set("timeout", 120)

	// PostToolUse: fires in IDE (toolCall=null startup) + CLI (every tool call).
	postToolEntry := util.NewOrderedMap()
	postToolEntry.Set("matcher", "")
	postToolEntry.Set("hooks", []interface{}{hookCfg})
	group.Set("PostToolUse", []interface{}{postToolEntry})

	// PreInvocation: fires in agy CLI at first prompt. IDE does not fire it.
	preInvEntry := util.NewOrderedMap()
	preInvEntry.Set("matcher", "")
	preInvEntry.Set("hooks", []interface{}{hookCfg})
	group.Set("PreInvocation", []interface{}{preInvEntry})

	cfg.Set("tokless-codegraph-index", group)

	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}
}

// SetAntigravityCompactToolOutput sets ui.compactToolOutput in agy settings.json.
// false = full tool output (not collapsed). MCP tool calls show the full command.
func SetAntigravityCompactToolOutput(enabled bool) {
	for _, f := range antigravitySettingsFiles() {
		_ = util.EnsureDir(filepath.Dir(f))
		raw, _ := util.ReadFileSafe(f)
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			cfg = util.NewOrderedMap()
		}
		ui := getOrCreateMap(cfg, "ui")
		ui.Set("compactToolOutput", enabled)
		if next := util.StringifyJSON(cfg); next != raw {
			_ = util.WriteFile(f, next)
		}
	}
}

// AllowAntigravityEntry adds a permissions.allow rule so agy auto-approves it.
func AllowAntigravityEntry(entry string) {
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

// RemoveAntigravityEntry drops a permissions.allow rule.
func RemoveAntigravityEntry(entry string) {
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

// ConfigureAntigravityMcp upserts mcpServers.<tool> into agy's canonical MCP config.
func ConfigureAntigravityMcp(toolID string) (changed bool, file string) {
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.PickMcpSpawn("codegraph", "serve", "--mcp")
		RemoveAntigravityCodegraphToolDefs()
	} else {
		spawn = util.PickMcpSpawn(toolID)
	}
	f := antigravityMcpConfigFile()
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
	removeAntigravityMcpFromFiles(toolID, antigravityLegacyMcpFiles())
	AllowAntigravityEntry("mcp(" + toolID + "/*)")
	return changed, file
}

// AntigravityMcpHas reports whether agy's canonical MCP config registers the tool.
func AntigravityMcpHas(toolID string) bool {
	raw, ok := util.ReadFileSafe(antigravityMcpConfigFile())
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

func cleanAntigravityDeadHookGroups() {
	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	changed := false
	for _, name := range append([]string(nil), cfg.Keys()...) {
		g, ok := cfg.Get(name)
		if !ok {
			continue
		}
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			continue
		}
		if _, has := gm.Get("hook_event_name"); has {
			cfg.Delete(name)
			changed = true
		}
	}
	if changed {
		_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
	}
}

// CleanupDeadIdeHooks removes stale codegraph-index hooks from flat settings.json files
// and the grouped hooks.json.
func CleanupDeadIdeHooks() {
	gemini := filepath.Join(util.Home(), ".gemini")
	for _, p := range []string{
		filepath.Join(gemini, "settings.json"),
		filepath.Join(gemini, "antigravity", "settings.json"),
		filepath.Join(gemini, "antigravity-ide", "settings.json"),
		filepath.Join(gemini, "antigravity-cli", "settings.json"),
	} {
		raw, ok := util.ReadFileSafe(p)
		if !ok {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		hv, ok := cfg.Get("hooks")
		if !ok {
			continue
		}
		hooks, ok := hv.(*util.OrderedMap)
		if !ok {
			continue
		}
		changed := false
		for _, event := range append([]string(nil), hooks.Keys()...) {
			arr, ok := hooks.Get(event)
			if !ok {
				continue
			}
			entries, ok := arr.([]interface{})
			if !ok {
				continue
			}
			kept := filterCodegraphIndexEntries(entries)
			if len(kept) != len(entries) {
				changed = true
				if len(kept) == 0 {
					hooks.Delete(event)
				} else {
					hooks.Set(event, kept)
				}
			}
		}
		if !changed {
			continue
		}
		if hooks.Len() == 0 {
			cfg.Delete("hooks")
		}
		if next := util.StringifyJSON(cfg); next != raw {
			_ = util.WriteFile(p, next)
		}
	}
}

func filterCodegraphIndexEntries(arr []interface{}) []interface{} {
	var kept []interface{}
	for _, e := range arr {
		em, ok := e.(*util.OrderedMap)
		if !ok {
			kept = append(kept, e)
			continue
		}
		hv, ok := em.Get("hooks")
		if !ok {
			kept = append(kept, e)
			continue
		}
		ha, ok := hv.([]interface{})
		if !ok {
			kept = append(kept, e)
			continue
		}
		drop := false
		for _, h := range ha {
			hm, ok := h.(*util.OrderedMap)
			if !ok {
				continue
			}
			if c, ok := hm.Get("command"); ok {
				if s, ok := c.(string); ok {
					if strings.Contains(s, "agy-hook codegraph-index") || strings.Contains(s, "context-mode hook gemini-cli") {
						drop = true
						break
					}
				}
			}
		}
		if !drop {
			kept = append(kept, e)
		}
	}
	return kept
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

// RemoveAntigravityMcp deletes mcpServers.<tool> from canonical and legacy MCP config surfaces.
func RemoveAntigravityMcp(toolID string) {
	files := append([]string{antigravityMcpConfigFile()}, antigravityLegacyMcpFiles()...)
	removeAntigravityMcpFromFiles(toolID, files)
	RemoveAntigravityEntry("mcp(" + toolID + "/*)")
}

func removeAntigravityMcpFromFiles(toolID string, files []string) {
	entry := "mcp(" + toolID + "/*)"
	for _, f := range files {
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
		if p, ok := cfg.Get("permissions"); ok {
			if pm, ok := p.(*util.OrderedMap); ok {
				if v, ok := pm.Get("allow"); ok {
					if arr, ok := v.([]any); ok {
						out := make([]any, 0, len(arr))
						for _, e := range arr {
							if s, ok := e.(string); ok && s == entry {
								continue
							}
							out = append(out, e)
						}
						if len(out) == 0 {
							pm.Delete("allow")
						} else {
							pm.Set("allow", out)
						}
						if pm.Len() == 0 {
							cfg.Delete("permissions")
						}
						_ = util.WriteFile(f, util.StringifyJSON(cfg))
					}
				}
			}
		}
	}
}
