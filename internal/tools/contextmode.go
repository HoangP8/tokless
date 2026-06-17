package tools

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func ctxEnsureInstalled(opts core.RunOpts) (bool, error) {
	if isTest() {
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would: npm install -g context-mode@latest (cache-skew-resistant)")
		return true, nil
	}
	opts.Reportf("checking", 0.1)
	if util.Which("context-mode") != "" && !opts.Upgrade {
		opts.Reportf("already installed", 1)
		return true, nil
	}
	opts.Reportf("npm install -g", 0.4)
	v, ok := util.NpmGlobalInstall("context-mode", "latest")
	if !ok {
		util.L.Err("context-mode install failed across all strategies (npm + tarball fallback).")
		if hint := util.NodeTooOldHint(18); hint != "" {
			util.L.Sub(hint)
		}
		return false, nil
	}
	opts.Reportf("ready", 1)
	util.L.Sub(util.C.Dim("context-mode @" + v + " installed"))
	return true, nil
}

func pluginIsContextMode(entry string) bool {
	return entry == "context-mode" || strings.HasPrefix(entry, "context-mode@")
}

// --- Claude ---

func ctxWireClaude(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would: claude mcp add context-mode -- context-mode")
		return true, nil
	}
	spawn := util.PickMcpSpawn("context-mode")
	if isTest() {
		cp := util.ClaudeCodePaths()
		_ = util.EnsureDir(cp.Dir)
		cfg := loadOrdered(cp.GlobalJSON)
		servers := getOrCreateMapT(cfg, "mcpServers")
		entry := util.NewOrderedMap()
		entry.Set("type", "stdio")
		entry.Set("command", spawn.Command)
		entry.Set("args", toAny(spawn.Args))
		servers.Set("context-mode", entry)
		_ = util.WriteFile(cp.GlobalJSON, util.StringifyJSON(cfg))
		agents.AllowClaudeMcpTool("context-mode")
		return true, nil
	}
	agents.ConfigureClaudeMcp("context-mode")
	util.L.Sub(util.C.Dim("tip: to enable slash commands, type inside Claude Code: /plugin marketplace add mksglu/context-mode && /plugin install context-mode@context-mode"))
	return true, nil
}

// --- OpenCode ---

func ctxWireOpenCode(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would add 'context-mode' to opencode.json plugin[]")
		return true, nil
	}
	op := util.OpenCodePathsResolved()
	_ = util.EnsureDir(op.Dir)
	cfg := loadOrdered(op.Config)
	setContextModePlugin(cfg)
	_ = util.WriteFile(op.Config, util.StringifyJSON(cfg))
	if isTest() {
		return true, nil
	}
	copyOpenCodeAgentsMd(op.Dir)
	cleanAllContextModeCache()
	runPostinstallInOpenCodeCache()
	return true, nil
}

var contextModeLatest = func() *string { return util.LatestVersionFor("context-mode") }

// contextModePluginSpec pins context-mode to the resolved latest version.
func contextModePluginSpec() string {
	if v := contextModeLatest(); v != nil && *v != "" {
		return "context-mode@" + *v
	}
	return "context-mode"
}

func setContextModePlugin(cfg *util.OrderedMap) {
	plugins := getArr(cfg, "plugin")
	kept := make([]any, 0, len(plugins))
	for _, p := range plugins {
		if s, ok := p.(string); ok && pluginIsContextMode(s) {
			continue
		}
		kept = append(kept, p)
	}
	kept = append(kept, contextModePluginSpec())
	cfg.Set("plugin", kept)
	if mv, ok := cfg.Get("mcp"); ok {
		if mm, ok := mv.(*util.OrderedMap); ok {
			if _, has := mm.Get("context-mode"); has {
				mm.Delete("context-mode")
				if mm.Len() == 0 {
					cfg.Delete("mcp")
				}
			}
		}
	}
}

// cleanAllContextModeCache clears stale/dangling cached dirs so bare @latest refetches.
func cleanAllContextModeCache() {
	cacheRoot := filepath.Join(util.Home(), ".cache", "opencode", "packages")
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return
	}
	n := 0
	for _, e := range entries {
		d := e.Name()
		if d == "context-mode" || strings.HasPrefix(d, "context-mode@") {
			_ = os.RemoveAll(filepath.Join(cacheRoot, d))
			n++
		}
	}
	if n > 0 {
		util.L.Sub(util.C.Dim("cleaned " + strconv.Itoa(n) + " old context-mode cache dir(s)"))
	}
}

func runPostinstallInOpenCodeCache() {
	if util.Which("bun") == "" {
		return
	}
	cacheRoot := filepath.Join(util.Home(), ".cache", "opencode", "packages")
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		d := e.Name()
		if d != "context-mode" && !strings.HasPrefix(d, "context-mode@") {
			continue
		}
		pkgHost := filepath.Join(cacheRoot, d)
		if !util.Exists(filepath.Join(pkgHost, "node_modules", "context-mode")) {
			continue
		}
		r := util.Run("bun", []string{"pm", "trust", "context-mode"}, util.RunOptions{Cwd: pkgHost, Capture: true})
		if r.Code == 0 {
			util.L.Sub(util.C.Dim("healed OpenCode plugin cache (" + d + ")"))
		}
	}
}

func copyOpenCodeAgentsMd(opencodeDir string) {
	dest := filepath.Join(opencodeDir, "AGENTS.md")
	if util.Exists(dest) {
		return
	}
	if util.Which("npm") == "" {
		return
	}
	root := util.Run("npm", []string{"root", "-g"}, util.RunOptions{Capture: true})
	if root.Code != 0 {
		return
	}
	src := filepath.Join(strings.TrimSpace(root.Stdout), "context-mode", "configs", "opencode", "AGENTS.md")
	if !util.Exists(src) {
		return
	}
	if b, err := os.ReadFile(src); err == nil {
		_ = util.WriteFile(dest, string(b))
	}
}

// --- Codex ---

var codexHookEvents = []string{"PreToolUse", "PostToolUse", "SessionStart", "PreCompact", "UserPromptSubmit", "Stop"}

const codexPreToolMatcher = "local_shell|shell|shell_command|exec_command|Bash|Shell|apply_patch|Edit|Write|grep_files|ctx_execute|ctx_execute_file|ctx_batch_execute|ctx_fetch_and_index|ctx_search|ctx_index|mcp__"

func codexHookEntry(event string) *util.OrderedMap {
	entry := util.NewOrderedMap()
	if event == "PreToolUse" {
		entry.Set("matcher", codexPreToolMatcher)
	}
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", "context-mode hook codex "+strings.ToLower(event))
	entry.Set("hooks", []any{hook})
	return entry
}

func ctxWireCodex(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would wire context-mode for codex")
		return true, nil
	}
	if isTest() {
		return wireCodexManual(), nil
	}
	if util.Which("codex") == "" {
		util.L.Err("codex CLI not on PATH — install codex first.")
		return false, nil
	}
	if ctxVerifyCodex() {
		return true, nil
	}
	probe := util.Run("codex", []string{"plugin", "--help"}, util.RunOptions{Capture: true})
	if probe.Code == 0 {
		util.L.Sub("codex supports plugins — using `codex plugin marketplace add`")
		r := util.Run("codex", []string{"plugin", "marketplace", "add", "mksglu/context-mode"}, util.RunOptions{Capture: true})
		if r.Code == 0 {
			add := util.Run("codex", []string{"plugin", "add", "context-mode@context-mode"}, util.RunOptions{Capture: true})
			if add.Code == 0 {
				enableCodexFeatureFlags(true)
				return true, nil
			}
			util.L.Debug("codex plugin add failed; falling back to manual hooks")
		} else {
			util.L.Debug("codex marketplace add failed; falling back to manual hooks")
		}
	}
	return wireCodexManual(), nil
}

func wireCodexManual() bool {
	cx := util.CodexPathsResolved()
	_ = util.EnsureDir(cx.Dir)
	enableCodexFeatureFlags(false)
	raw, _ := util.ReadFileSafe(cx.Config)
	spawn := util.PickMcpSpawn("context-mode")
	block := util.NewTomlBlock("mcp_servers.context-mode")
	block.Set("command", spawn.Command)
	if len(spawn.Args) > 0 {
		block.Set("args", spawn.Args)
	}
	_ = util.WriteFile(cx.Config, util.UpsertBlock(raw, block, false))

	hooksPath := filepath.Join(cx.Dir, "hooks.json")
	existing := loadOrdered(hooksPath)
	_ = util.WriteFile(hooksPath, util.StringifyJSON(mergeCodexHooks(existing)))
	return true
}

func enableCodexFeatureFlags(pluginHooks bool) {
	cx := util.CodexPathsResolved()
	raw, _ := util.ReadFileSafe(cx.Config)
	block := util.NewTomlBlock("features")
	block.Set("hooks", true)
	if pluginHooks {
		block.Set("plugin_hooks", true)
	}
	_ = util.WriteFile(cx.Config, util.UpsertBlock(raw, block, true))
}

// mergeCodexHooks replaces our entries per event, keeping unrelated ones.
func mergeCodexHooks(existing *util.OrderedMap) *util.OrderedMap {
	out := existing
	if out == nil {
		out = util.NewOrderedMap()
	}
	hooks := getOrCreateMapT(out, "hooks")
	for _, event := range codexHookEvents {
		var arr []any
		if v, ok := hooks.Get(event); ok {
			if a, ok := v.([]any); ok {
				arr = a
			}
		}
		var filtered []any
		for _, entry := range arr {
			if !isOursForEvent(entry, event) {
				filtered = append(filtered, entry)
			}
		}
		filtered = append(filtered, codexHookEntry(event))
		hooks.Set(event, filtered)
	}
	out.Set("hooks", hooks)
	return out
}

func isOursForEvent(entry any, event string) bool {
	em, ok := entry.(*util.OrderedMap)
	if !ok {
		return false
	}
	hv, ok := em.Get("hooks")
	if !ok {
		return false
	}
	arr, ok := hv.([]any)
	if !ok {
		return false
	}
	prefix := "context-mode hook codex " + strings.ToLower(event)
	for _, h := range arr {
		hm, ok := h.(*util.OrderedMap)
		if !ok {
			continue
		}
		if cmd, ok := hm.Get("command"); ok {
			if s, ok := cmd.(string); ok && strings.HasPrefix(s, prefix) {
				return true
			}
		}
	}
	return false
}

// --- unwire ---

func ctxUnwireClaude(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	agents.RemoveClaudeMcp("context-mode")
	return true, nil
}

func ctxUnwireOpenCode(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	op := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(op.Config)
	if !ok {
		return true, nil
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	if pv, ok := cfg.Get("plugin"); ok {
		if arr, ok := pv.([]any); ok {
			var kept []any
			for _, p := range arr {
				if s, ok := p.(string); ok && pluginIsContextMode(s) {
					continue
				}
				kept = append(kept, p)
			}
			if len(kept) == 0 {
				cfg.Delete("plugin")
			} else {
				cfg.Set("plugin", kept)
			}
		}
	}
	_ = util.WriteFile(op.Config, util.StringifyJSON(cfg))
	return true, nil
}

func ctxUnwireCodex(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	cx := util.CodexPathsResolved()
	if raw, ok := util.ReadFileSafe(cx.Config); ok {
		next := util.RemoveBlock(raw, "mcp_servers.context-mode")
		if next != raw {
			_ = util.WriteFile(cx.Config, next)
		}
	}
	hooksPath := filepath.Join(cx.Dir, "hooks.json")
	if !util.Exists(hooksPath) {
		return true, nil
	}
	data := loadOrdered(hooksPath)
	hv, ok := data.Get("hooks")
	if !ok {
		return true, nil
	}
	hooks, ok := hv.(*util.OrderedMap)
	if !ok {
		return true, nil
	}
	for _, event := range codexHookEvents {
		var arr []any
		if v, ok := hooks.Get(event); ok {
			if a, ok := v.([]any); ok {
				arr = a
			}
		}
		var kept []any
		for _, entry := range arr {
			if !isOursForEvent(entry, event) {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			hooks.Delete(event)
		} else {
			hooks.Set(event, kept)
		}
	}
	if hooks.Len() == 0 {
		_ = os.Remove(hooksPath)
	} else {
		_ = util.WriteFile(hooksPath, util.StringifyJSON(data))
	}
	return true, nil
}

// --- Antigravity (upstream: MCP-only, no hooks; tokless adds hooks + bridge script) ---

const ctxGeminiMarker = "context-mode — MANDATORY routing rules"

func routingFilePath() string {
	return filepath.Join(util.Home(), ".gemini", "config", "tokless", "context-mode-routing.md")
}

// copyAntigravityRoutingFile copies context-mode's antigravity GEMINI.md
// to ~/.gemini/config/tokless/context-mode-routing.md for PreInvocation injection.
func copyAntigravityRoutingFile() string {
	ctxDir := findContextModeDir()
	if ctxDir == "" {
		return ""
	}
	src := filepath.Join(ctxDir, "configs", "antigravity", "GEMINI.md")
	raw, ok := util.ReadFileSafe(src)
	if !ok {
		return ""
	}
	clean := strings.Replace(raw,
		"Antigravity has NO hooks — these instructions are ONLY enforcement. Follow strictly.\n\n",
		"", 1)
	dest := routingFilePath()
	_ = util.EnsureDir(filepath.Dir(dest))
	_ = util.WriteFile(dest, clean)
	return dest
}

func geminiMdPath() string {
	return filepath.Join(util.Home(), ".gemini", "GEMINI.md")
}

func upsertGeminiMdSection(open, close, content string) {
	p := geminiMdPath()
	existing, ok := util.ReadFileSafe(p)
	if ok {
		oi := strings.Index(existing, open)
		ci := strings.Index(existing, close)
		if oi >= 0 && ci > oi {
			ci += len(close)
			for ci < len(existing) && existing[ci] == '\n' { ci++ }
			r := existing[:oi] + content + "\n" + existing[ci:]
			_ = util.WriteFile(p, strings.TrimRight(r, "\n")+"\n")
			return
		}
		existing = strings.TrimRight(existing, "\n")
		if existing != "" { existing += "\n\n" }
		_ = util.WriteFile(p, existing+content+"\n")
		return
	}
	_ = util.WriteFile(p, content+"\n")
}

func removeGeminiMdSection(open, close string) {
	p := geminiMdPath()
	existing, ok := util.ReadFileSafe(p)
	if !ok { return }
	oi := strings.Index(existing, open)
	ci := strings.Index(existing, close)
	if oi < 0 || ci <= oi { return }
	ci += len(close)
	for oi > 0 && existing[oi-1] == '\n' { oi-- }
	for ci < len(existing) && existing[ci] == '\n' { ci++ }
	c := existing[:oi] + existing[ci:]
	c = strings.TrimRight(c, "\n")
	if strings.TrimSpace(c) == "" {
		_ = os.Remove(p)
		return
	}
	_ = util.WriteFile(p, c+"\n")
}

func ctxWireAntigravity(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would add context-mode to mcp_config.json and install context-mode-hook agy")
		return true, nil
	}
	agents.ConfigureAntigravityMcp("context-mode")

	if isTest() {
		dest := routingFilePath()
		_ = util.EnsureDir(filepath.Dir(dest))
		_ = util.WriteFile(dest, "# context-mode — MANDATORY routing rules\n\ncontext-mode MCP tools available.\n")
	}

	agents.InstallAntigravityContextModeHook()
	routingPath := copyAntigravityRoutingFile()
	agents.AllowAntigravityEntry("command(echo)")

	// Write routing block to ~/.gemini/GEMINI.md for system-level injection.
	if raw, ok := util.ReadFileSafe(routingPath); ok {
		section := "<!-- CONTEXT-MODE_START -->\n\n" + strings.TrimSpace(raw) + "\n\n<!-- CONTEXT-MODE_END -->"
		upsertGeminiMdSection("<!-- CONTEXT-MODE_START -->", "<!-- CONTEXT-MODE_END -->", section)
	}

	return ctxVerifyAntigravity(), nil
}

// findContextModeDir finds the context-mode npm package root directory.
func findContextModeDir() string {
	if util.Which("npm") != "" {
		root := util.Run("npm", []string{"root", "-g"}, util.RunOptions{Capture: true})
		if root.Code == 0 {
			ctxRoot := strings.TrimSpace(root.Stdout)
			ctxPkg := filepath.Join(ctxRoot, "context-mode")
			if util.Exists(filepath.Join(ctxPkg, "configs", "antigravity", "GEMINI.md")) {
				return ctxPkg
			}
		}
	}
	if util.Which("node") != "" {
		script := "try{const p=require.resolve('context-mode/package.json');process.stdout.write(require('path').dirname(p))}catch(e){}"
		r := util.Run("node", []string{"-e", script}, util.RunOptions{Capture: true})
		if r.Code == 0 && strings.TrimSpace(r.Stdout) != "" {
			ctxRoot := strings.TrimSpace(r.Stdout)
			if util.Exists(filepath.Join(ctxRoot, "configs", "antigravity", "GEMINI.md")) {
				return ctxRoot
			}
		}
	}
	if cm := util.Which("context-mode"); cm != "" {
		script := "const{realpathSync}=require('fs');const{dirname,join}=require('path');const r=realpathSync(process.argv[1]);process.stdout.write(join(dirname(dirname(r)),'context-mode'))"
		r := util.Run("node", []string{"-e", script, cm}, util.RunOptions{Capture: true})
		if r.Code == 0 && strings.TrimSpace(r.Stdout) != "" {
			ctxRoot := strings.TrimSpace(r.Stdout)
			if util.Exists(filepath.Join(ctxRoot, "configs", "antigravity", "GEMINI.md")) {
				return ctxRoot
			}
		}
	}
	return ""
}

func ctxUnwireAntigravity(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would remove context-mode from mcp_config.json, uninstall hook, and delete routing file")
		return true, nil
	}
	agents.RemoveAntigravityMcp("context-mode")
	agents.RemoveAntigravityContextModeHook()
	_ = os.Remove(routingFilePath())
	agents.RemoveAntigravityEntry("command(echo)")
	removeGeminiMdSection("<!-- CONTEXT-MODE_START -->", "<!-- CONTEXT-MODE_END -->")
	if cwd, err := os.Getwd(); err == nil {
		dest := filepath.Join(cwd, "GEMINI.md")
		if raw, ok := util.ReadFileSafe(dest); ok && strings.Contains(raw, ctxGeminiMarker) {
			_ = os.Remove(dest)
		}
	}
	return true, nil
}

func ctxVerifyAntigravity() bool { return agents.AntigravityMcpHas("context-mode") && agents.HasAntigravityContextModeHook() }

// --- verify ---

func ctxVerifyClaude() bool {
	cp := util.ClaudeCodePaths()
	raw, ok := util.ReadFileSafe(cp.GlobalJSON)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if s, ok := cfg.Get("mcpServers"); ok {
		if sm, ok := s.(*util.OrderedMap); ok {
			_, has := sm.Get("context-mode")
			return has
		}
	}
	return false
}

func ctxVerifyOpenCode() bool {
	op := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(op.Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hasPlugin := false
	if pv, ok := cfg.Get("plugin"); ok {
		if arr, ok := pv.([]any); ok {
			for _, p := range arr {
				if s, ok := p.(string); ok && pluginIsContextMode(s) {
					hasPlugin = true
					break
				}
			}
		}
	}
	hasLegacy := false
	if mv, ok := cfg.Get("mcp"); ok {
		if mm, ok := mv.(*util.OrderedMap); ok {
			_, hasLegacy = mm.Get("context-mode")
		}
	}
	return hasPlugin && !hasLegacy
}

func ctxVerifyCodex() bool {
	cx := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(cx.Config)
	if !ok {
		return false
	}
	if strings.Contains(raw, `[plugins."context-mode@context-mode"]`) {
		return true
	}
	if !strings.Contains(raw, "[mcp_servers.context-mode]") {
		return false
	}
	hooksPath := filepath.Join(cx.Dir, "hooks.json")
	data := loadOrdered(hooksPath)
	if hv, ok := data.Get("hooks"); ok {
		if hm, ok := hv.(*util.OrderedMap); ok {
			_, has := hm.Get("PreToolUse")
			return has
		}
	}
	return false
}

var contextMode = &core.ToolManifest{
	ID:          "context-mode",
	Label:       "Context-Mode",
	Description: "Routes long context off-thread to a sandbox, keeping the agent's window small.",
	Homepage:    "https://github.com/mksglu/context-mode",
	InstallHint: "npm i -g context-mode",
	Channel:     core.ChannelNpm,
	Install:     ctxEnsureInstalled,
	WireFor: map[string]core.AgentFn{
		"claude":      ctxWireClaude,
		"opencode":    ctxWireOpenCode,
		"codex":       ctxWireCodex,
		"antigravity": ctxWireAntigravity,
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":      ctxUnwireClaude,
		"opencode":    ctxUnwireOpenCode,
		"codex":       ctxUnwireCodex,
		"antigravity": ctxUnwireAntigravity,
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":      func() *bool { return core.BoolPtr(ctxVerifyClaude()) },
		"opencode":    func() *bool { return core.BoolPtr(ctxVerifyOpenCode()) },
		"codex":       func() *bool { return core.BoolPtr(ctxVerifyCodex()) },
		"antigravity": func() *bool { return core.BoolPtr(ctxVerifyAntigravity()) },
	},
}

// Register wires all tools into the core registry, in canonical order.
func Register() {
	core.RegisterTool(rtk)
	core.RegisterTool(caveman)
	core.RegisterTool(codegraph)
	core.RegisterTool(contextMode)
}

// helpers shared in tools package

func loadOrdered(path string) *util.OrderedMap {
	if raw, ok := util.ReadFileSafe(path); ok {
		if m := util.TryParseJsonc(raw); m != nil {
			return m
		}
	}
	return util.NewOrderedMap()
}

func getArr(m *util.OrderedMap, key string) []any {
	if v, ok := m.Get(key); ok {
		if a, ok := v.([]any); ok {
			return a
		}
	}
	return []any{}
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
