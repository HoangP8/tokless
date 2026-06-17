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
	for _, variant := range []string{"antigravity-desktop", "antigravity-cli", "antigravity-ide"} {
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
	}
}

func antigravityDeadGuiSettingsFiles() []string {
	gemini := filepath.Join(util.Home(), ".gemini")
	return []string{
		filepath.Join(gemini, "antigravity-ide", "settings.json"),
		filepath.Join(gemini, "antigravity-desktop", "settings.json"),
	}
}

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

// InstallAntigravityContextModeHook installs PreToolUse hook using the official
// context-mode routing engine (routePreToolUse deny/context/passthrough).
// Writes to hooks.json (CLI PreToolUse) and all settings surfaces (IDE BeforeTool).
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

	group := util.NewOrderedMap()

	preToolHook := util.NewOrderedMap()
	preToolHook.Set("type", "command")
	preToolHook.Set("command", "context-mode hook gemini-cli beforetool")
	preToolHook.Set("timeout", 10)
	preToolUseEntry := util.NewOrderedMap()
	preToolUseEntry.Set("matcher", "web_fetch|Bash|read_file")
	preToolUseEntry.Set("hooks", []interface{}{preToolHook})
	group.Set("PreToolUse", []interface{}{preToolUseEntry})

	if _, ok := cfg.Get("tokless-context-mode"); ok {
		cfg.Delete("tokless-context-mode")
	}
	cfg.Set("ctx", group)

	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}

	// Also install BeforeTool hook in all settings surfaces for IDE/desktop.
	installContextModeBeforeToolAll()
}

// allInternalSettingsFiles returns every agy settings surface (CLI, IDE, desktop).
func allInternalSettingsFiles() []string {
	gemini := filepath.Join(util.Home(), ".gemini")
	var files []string
	for _, variant := range []string{"antigravity-cli", "antigravity-ide", "antigravity-desktop"} {
		f := filepath.Join(gemini, variant, "settings.json")
		if util.Exists(filepath.Dir(f)) {
			files = append(files, f)
		}
	}
	return files
}

// installContextModeBeforeToolAll writes the official context-mode BeforeTool hook
// to every agy settings surface (CLI, IDE, desktop).
func installContextModeBeforeToolAll() {
	for _, p := range allInternalSettingsFiles() {
		_ = util.EnsureDir(filepath.Dir(p))
		raw, ok := util.ReadFileSafe(p)
		var cfg *util.OrderedMap
		if ok {
			cfg = util.TryParseJsonc(raw)
		}
		if cfg == nil {
			cfg = util.NewOrderedMap()
		}
		hooks := getOrCreateMap(cfg, "hooks")
		btArr := removeContextModeBeforeToolEntries(getOrCreateArr(hooks, "BeforeTool"))

		hookCfg := util.NewOrderedMap()
		hookCfg.Set("type", "command")
		hookCfg.Set("command", "context-mode hook gemini-cli beforetool")
		hookCfg.Set("timeout", 10)

		entry := util.NewOrderedMap()
		entry.Set("matcher", "web_fetch|Bash|read_file")
		entry.Set("hooks", []interface{}{hookCfg})

		btArr = append(btArr, entry)
		hooks.Set("BeforeTool", btArr)
		if next := util.StringifyJSON(cfg); next != raw {
			_ = util.WriteFile(p, next)
		}
	}
}

// removeContextModeBeforeToolEntries filters out official context-mode entries.
func removeContextModeBeforeToolEntries(btArr []interface{}) []interface{} {
	var kept []interface{}
	for _, e := range btArr {
		em, ok := e.(*util.OrderedMap)
		if !ok {
			kept = append(kept, e)
			continue
		}
		hooksArr, ok := em.Get("hooks")
		if !ok {
			kept = append(kept, e)
			continue
		}
		ha, ok := hooksArr.([]interface{})
		if !ok {
			kept = append(kept, e)
			continue
		}
		found := false
		for _, h := range ha {
			hm, ok := h.(*util.OrderedMap)
			if ok {
				if c, ok := hm.Get("command"); ok {
					if s, ok := c.(string); ok && strings.Contains(s, "context-mode hook gemini") {
						found = true
						break
					}
				}
			}
		}
		if !found {
			kept = append(kept, e)
		}
	}
	return kept
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

// RemoveAntigravityContextModeHook removes the context-mode hook for agy.
func RemoveAntigravityContextModeHook() {
	hooksFile := antigravityHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if ok {
		cfg := util.TryParseJsonc(raw)
		if cfg != nil {
			if _, ok := cfg.Get("ctx"); ok {
				cfg.Delete("ctx")
				_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
			}
			if _, ok := cfg.Get("tokless-context-mode"); ok {
				cfg.Delete("tokless-context-mode")
				_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
			}
		}
	}
	// Also remove from all settings surfaces (IDE/desktop/CLI).
	for _, p := range allInternalSettingsFiles() {
		raw, ok := util.ReadFileSafe(p)
		if !ok { continue }
		cfg := util.TryParseJsonc(raw)
		if cfg == nil { continue }
		hv, ok := cfg.Get("hooks")
		if !ok { continue }
		hooks, ok := hv.(*util.OrderedMap)
		if !ok { continue }
		v, ok := hooks.Get("BeforeTool")
		if !ok { continue }
		btArr, ok := v.([]interface{})
		if !ok { continue }
		kept := removeContextModeBeforeToolEntries(btArr)
		if len(kept) != len(btArr) {
			if len(kept) == 0 {
				hooks.Delete("BeforeTool")
				if hooks.Len() == 0 { cfg.Delete("hooks") }
			} else {
				hooks.Set("BeforeTool", kept)
			}
			_ = util.WriteFile(p, util.StringifyJSON(cfg))
		}
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
	return strings.Contains(cmdStr, "rtk-hook agy")
}

// HasAntigravityContextModeHook reports whether the context-mode hook is installed.
func HasAntigravityContextModeHook() bool {
	raw, ok := util.ReadFileSafe(antigravityHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	groupObj, ok := cfg.Get("ctx")
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
	return strings.Contains(cmdStr, "context-mode hook gemini")
}

// InstallAntigravityCodegraphIndexHook installs a PreInvocation hook that
// runs `tokless agy-hook codegraph-index --sync` to initialize .codegraph before the agent starts.
func InstallAntigravityCodegraphIndexHook() {
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless"
	}
	command := tok + " agy-hook codegraph-index --sync"

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

	preInvocationEntry := util.NewOrderedMap()
	preInvocationEntry.Set("matcher", "")
	preInvocationEntry.Set("hooks", []interface{}{hookCfg})

	group.Set("PreInvocation", []interface{}{preInvocationEntry})

	cfg.Set("tokless-codegraph-index", group)

	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}
}

// InstallAntigravityCodegraphToolDefs writes static codegraph MCP tool definitions
// to the IDE variant's MCP directory.
func InstallAntigravityCodegraphToolDefs() {
	gemini := filepath.Join(util.Home(), ".gemini")
	ideMcpDir := filepath.Join(gemini, "antigravity-ide", "mcp", "codegraph")
	if !util.Exists(filepath.Join(gemini, "antigravity-ide")) {
		return
	}
	_ = util.EnsureDir(ideMcpDir)
	for _, td := range codegraphToolDefs {
		_ = util.WriteFile(filepath.Join(ideMcpDir, td.name+".json"), td.json)
	}
}

type codegraphToolDef struct {
	name string
	json string
}

var codegraphToolDefs = []codegraphToolDef{
	{"codegraph_explore", codegraphExploreJSON},
	{"codegraph_node", codegraphNodeJSON},
	{"codegraph_search", codegraphSearchJSON},
	{"codegraph_callers", codegraphCallersJSON},
}

const codegraphExploreJSON = `{"name":"codegraph_explore","description":"PRIMARY TOOL \u2014 call FIRST for almost any question OR before an edit: how does X work, architecture, a bug, where/what is X, surveying an area, or the symbols you are about to change. Returns the verbatim source of the relevant symbols grouped by file in ONE capped call (Read-equivalent \u2014 treat the shown source as already Read; do NOT re-open those files), plus the call path among them. Query can be a natural-language question OR a bag of symbol/file names. Usually the ONLY call you need \u2014 more accurate context, in far fewer tokens and round-trips than a search/Read/Grep loop.","inputSchema":{"type":"object","properties":{"query":{"type":"string","description":"Symbol names, file names, or short code terms to explore (e.g., \"AuthService loginUser session-manager\", \"GraphTraverser BFS impact traversal.ts\"). For a flow question, name the symbols spanning the flow (e.g. \"mutateElement renderScene\"). A natural-language question works too \u2014 no prior codegraph_search needed."},"maxFiles":{"type":"number","description":"Maximum number of files to include source code from (default: 12)","default":12},"projectPath":{"type":"string","description":"Path to a different project with .codegraph/ initialized. If omitted, uses current project. Use this to query other codebases."}},"required":["query"]}}`

const codegraphNodeJSON = `{"name":"codegraph_node","description":"Two modes. (1) READ A FILE \u2014 use INSTEAD of the Read tool: pass file (a path or basename) with no symbol and it returns that file's current on-disk source with line numbers, exactly the shape Read gives you, narrowable with offset/limit just like Read \u2014 PLUS a one-line note of which files depend on it. Same bytes as Read, faster (served from the index), with the blast radius attached. Use it whenever you would Read a source file. (2) ONE SYMBOL you can name \u2014 its location, signature, verbatim source (includeCode=true) and caller/callee trail in one call, so before changing it you see what calls it and what your edit would break. For an AMBIGUOUS name it returns EVERY matching definition's body in one call (so you never Read a file to find the right overload); pass file/line to pin one. Use codegraph_explore for several related symbols or the full flow.","inputSchema":{"type":"object","properties":{"symbol":{"type":"string","description":"Name of the symbol to read (symbol mode). Omit it and pass file alone to read a whole file like Read."},"includeCode":{"type":"boolean","description":"Symbol mode: include the symbol's full body (default: false). Ignored in file mode, which always returns source unless symbolsOnly is set.","default":false},"file":{"type":"string","description":"A file path or basename (e.g. \"harness.rs\", \"src/auth/session.ts\"). Pass it ALONE (no symbol) to READ the file like the Read tool \u2014 its full source with line numbers + which files depend on it. Or pass it WITH a symbol to disambiguate an overloaded name to the definition in this file."},"offset":{"type":"number","description":"File mode: 1-based line to start reading from, exactly like Read's offset. Defaults to the start of the file."},"limit":{"type":"number","description":"File mode: maximum number of lines to return, exactly like Read's limit. Defaults to the whole file (capped at 2000 lines, like Read)."},"symbolsOnly":{"type":"boolean","description":"File mode: return just the file's symbol map + dependents (a cheap structural overview) instead of its source.","default":false},"line":{"type":"number","description":"Symbol mode only: disambiguate to the definition at/around this line (use with the file:line a trail showed you)."},"projectPath":{"type":"string","description":"Path to a different project with .codegraph/ initialized. If omitted, uses current project. Use this to query other codebases."}},"required":[]}}`

const codegraphSearchJSON = `{"name":"codegraph_search","description":"Quick symbol search by name. Returns locations only (no code). Use codegraph_explore instead to get the actual source / understand an area in one call.","inputSchema":{"type":"object","properties":{"query":{"type":"string","description":"Symbol name or partial name (e.g., \"auth\", \"signIn\", \"UserService\")"},"kind":{"type":"string","description":"Filter by node kind","enum":["function","method","class","interface","type","variable","route","component"]},"limit":{"type":"number","description":"Maximum results (default: 10)","default":10},"projectPath":{"type":"string","description":"Path to a different project with .codegraph/ initialized. If omitted, uses current project. Use this to query other codebases."}},"required":["query"]}}`

const codegraphCallersJSON = `{"name":"codegraph_callers","description":"List functions that call <symbol>. For the full flow, use codegraph_explore.","inputSchema":{"type":"object","properties":{"symbol":{"type":"string","description":"Name of the function, method, or class to find callers for"},"file":{"type":"string","description":"Narrow to the definition in this file (path or suffix) when several same-named symbols exist (e.g. one UserService per app in a monorepo)"},"limit":{"type":"number","description":"Maximum number of callers to return (default: 20)","default":20},"projectPath":{"type":"string","description":"Path to a different project with .codegraph/ initialized. If omitted, uses current project. Use this to query other codebases."}},"required":["symbol"]}}`

// RemoveAntigravityCodegraphToolDefs deletes the static codegraph tool definitions
// from the IDE variant's MCP directory.
func RemoveAntigravityCodegraphToolDefs() {
	gemini := filepath.Join(util.Home(), ".gemini")
	ideMcpDir := filepath.Join(gemini, "antigravity-ide", "mcp", "codegraph")
	for _, td := range codegraphToolDefs {
		_ = os.Remove(filepath.Join(ideMcpDir, td.name+".json"))
	}
}

// RemoveAntigravityCodegraphIndexHook removes the codegraph index hook for agy.
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
	pre, ok := group.Get("PreInvocation")
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
	return strings.Contains(cmdStr, "agy-hook codegraph-index")
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

// ConfigureAntigravityMcp upserts mcpServers.<tool> into every surface's MCP config.
func ConfigureAntigravityMcp(toolID string) (changed bool, file string) {
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("antigravity", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.PickMcpSpawn(toolID)
	}
	AllowAntigravityEntry("mcp(" + toolID + "/*)")
	AllowAntigravityEntry("command(rtk)")
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
	AllowAntigravityEntry("mcp(" + toolID + "/*)")
	AllowAntigravityEntry("command(rtk)")
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

var antigravitySettingsPath = func() string {
	return filepath.Join(util.Home(), ".gemini", "settings.json")
}

// InstallCodegraphBeforeToolHook adds a BeforeTool hook to ~/.gemini/settings.json.
func InstallCodegraphBeforeToolHook() {
	tok := getToklessAbs()
	if strings.ContainsAny(tok, " \t") {
		tok = "tokless"
	}
	command := tok + " agy-hook codegraph-index"

	p := antigravitySettingsPath()
	raw, ok := util.ReadFileSafe(p)
	var cfg *util.OrderedMap
	if ok {
		cfg = util.TryParseJsonc(raw)
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}

	hooks := getOrCreateMap(cfg, "hooks")
	btArr := removeCodegraphBeforeToolEntries(getOrCreateArr(hooks, "BeforeTool"))

	hookCfg := util.NewOrderedMap()
	hookCfg.Set("type", "command")
	hookCfg.Set("command", command)
	hookCfg.Set("timeout", 5)

	entry := util.NewOrderedMap()
	entry.Set("matcher", ".*")
	entry.Set("hooks", []interface{}{hookCfg})

	btArr = append(btArr, entry)
	hooks.Set("BeforeTool", btArr)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(p, next)
	}
}

// removeCodegraphBeforeToolEntries filters out all codegraph-index entries (any path variant).
func removeCodegraphBeforeToolEntries(btArr []interface{}) []interface{} {
	var kept []interface{}
	for _, e := range btArr {
		em, ok := e.(*util.OrderedMap)
		if !ok {
			kept = append(kept, e)
			continue
		}
		hooksArr, ok := em.Get("hooks")
		if !ok {
			kept = append(kept, e)
			continue
		}
		ha, ok := hooksArr.([]interface{})
		if !ok {
			kept = append(kept, e)
			continue
		}
		found := false
		for _, h := range ha {
			hm, ok := h.(*util.OrderedMap)
			if ok {
				if c, ok := hm.Get("command"); ok {
					if s, ok := c.(string); ok && strings.HasSuffix(s, " agy-hook codegraph-index") {
						found = true
						break
					}
				}
			}
		}
		if !found {
			kept = append(kept, e)
		}
	}
	return kept
}

// RemoveCodegraphBeforeToolHook drops ALL BeforeTool entries matching tokless codegraph-index
// (any path variant) from the shared settings.
func RemoveCodegraphBeforeToolHook() {
	p := antigravitySettingsPath()
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	hv, ok := cfg.Get("hooks")
	if !ok {
		return
	}
	hooks, ok := hv.(*util.OrderedMap)
	if !ok {
		return
	}
	v, ok := hooks.Get("BeforeTool")
	if !ok {
		return
	}
	btArr, ok := v.([]interface{})
	if !ok {
		return
	}
	kept := removeCodegraphBeforeToolEntries(btArr)
	if len(kept) != len(btArr) {
		if len(kept) == 0 {
			hooks.Delete("BeforeTool")
			if hooks.Len() == 0 {
				cfg.Delete("hooks")
			}
		} else {
			hooks.Set("BeforeTool", kept)
		}
		_ = util.WriteFile(p, util.StringifyJSON(cfg))
	}
}

// getOrCreateArr gets or creates an array value in the OrderedMap.
func getOrCreateArr(m *util.OrderedMap, key string) []interface{} {
	if v, ok := m.Get(key); ok {
		if arr, ok := v.([]interface{}); ok {
			return arr
		}
	}
	return []interface{}{}
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
	RemoveAntigravityEntry("mcp(" + toolID + "/*)")
}
