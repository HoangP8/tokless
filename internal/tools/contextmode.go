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
	// context-mode needs Node 22+ (better-sqlite3 native prebuild).
	if !util.NodeAgeAlreadyChecked() {
		if maj := util.NodeMajor(); maj > 0 && maj < contextModeMinNode {
			util.L.Warn("Node.js v" + strconv.Itoa(maj) + " is too old for context-mode (need v" + strconv.Itoa(contextModeMinNode) + "+).")
			if util.Confirm("Upgrade Node.js now? (y/n)", true) {
				if !util.InstallNodeForTools() {
					util.L.Err("couldn't upgrade Node.js. context-mode needs v" + strconv.Itoa(contextModeMinNode) + "+.")
					util.L.Sub("Manual: https://nodejs.org/en/download")
					return false, nil
				}
			} else {
				util.L.Sub("Skipping. context-mode may fail to install.")
			}
		}
	}
	opts.Reportf("npm install -g", 0.4)
	v, ok, _ := util.NpmGlobalInstall("context-mode", "latest")
	if !ok {
		util.L.Err("context-mode install failed across all strategies (npm + tarball fallback).")
		util.L.Sub("Each attempt was logged above. Common causes: old Node, no build tools, or a registry mirror.")
		if hint := util.NodeTooOldHint(contextModeMinNode); hint != "" {
			util.L.Sub(hint)
		}
		return false, nil
	}
	opts.Reportf("ready", 1)
	util.L.Sub(util.C.Dim("context-mode @" + v + " installed"))
	return true, nil
}

const contextModeMinNode = 22

func pluginIsContextMode(entry string) bool {
	return entry == "context-mode" || strings.HasPrefix(entry, "context-mode@")
}

// setContextModePluginBare ensures the opencode.jsonc plugin array ends with
// the bare `context-mode` entry.
func setContextModePluginBare(cfg *util.OrderedMap) {
	plugins := getArr(cfg, "plugin")
	kept := make([]any, 0, len(plugins))
	for _, p := range plugins {
		if s, ok := p.(string); ok && pluginIsContextMode(s) {
			continue
		}
		kept = append(kept, p)
	}
	kept = append(kept, "context-mode")
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
		WriteOwner("claude", "context-mode")
		return true, nil
	}
	agents.ConfigureClaudeMcp("context-mode")
	WriteOwner("claude", "context-mode")
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
	WriteOwner("opencode", "context-mode")
	if isTest() {
		return true, nil
	}
	cleanAllContextModeCache()
	runPostinstallInOpenCodeCache()
	return true, nil
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
	kept = append(kept, "context-mode")
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

// cleanAllContextModeCache clears stale cached dirs so bare @latest refetches.
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
		r := util.Run(util.ResolveBunBinary(), []string{"pm", "trust", "context-mode"}, util.RunOptions{Cwd: pkgHost, Capture: true})
		if r.Code == 0 {
			util.L.Sub(util.C.Dim("healed OpenCode plugin cache (" + d + ")"))
		}
	}
}

// --- Codex ---

func ctxWireCodex(opts core.RunOpts) (bool, error) {
	if isTest() {
		return wireCodexManual(), nil
	}
	if util.Which("codex") == "" {
		util.L.Err("codex CLI not on PATH — install codex first.")
		return false, nil
	}
	return wireCodexManual(), nil
}

func wireCodexManual() bool {
	cx := util.CodexPathsResolved()
	_ = util.EnsureDir(cx.Dir)
	raw, _ := util.ReadFileSafe(cx.Config)
	raw = util.RemoveBlock(raw, "mcp_servers.context-mode")
	spawn := util.PickMcpSpawn("context-mode")
	block := util.NewTomlBlock("mcp_servers.context_mode")
	block.Set("command", spawn.Command)
	if len(spawn.Args) > 0 {
		block.Set("args", spawn.Args)
	}
	block.Set("enabled", true)
	block.Set("default_tools_approval_mode", "approve")
	_ = util.WriteFile(cx.Config, util.UpsertBlock(raw, block, false))

	cleanupCodexContextModeHooks()
	cleanupWorkspaceCodexContextModeMcp(cx.Dir)
	writeCodexAgentsMd()

	agents.InstallCodexContextModeHook()

	return true
}

// writeCodexAgentsMd writes the unified TOKLESS block with context-mode as one owner.
func writeCodexAgentsMd() {
	cx := util.CodexPathsResolved()
	if cx.Instructions == "" {
		return
	}
	WriteOwner("codex", "context-mode")
}

// removeCodexContextModeHooks removes context-mode hook entries, keeping unrelated hooks.
func removeCodexContextModeHooks(existing *util.OrderedMap) *util.OrderedMap {
	out := existing
	if out == nil {
		out = util.NewOrderedMap()
	}
	hooks := getOrCreateMapT(out, "hooks")
	for _, event := range []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "SessionStart", "PreCompact", "Stop", "PermissionRequest"} {
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
		if len(filtered) == 0 {
			hooks.Delete(event)
		} else {
			hooks.Set(event, filtered)
		}
	}
	if hooks.Len() == 0 {
		out.Delete("hooks")
	} else {
		out.Set("hooks", hooks)
	}
	return out
}

func cleanupCodexContextModeHooks() {
	cx := util.CodexPathsResolved()
	cleanupCodexContextModeHooksInDir(cx.Dir)
	if cwd, err := os.Getwd(); err == nil {
		projectCodex := filepath.Join(cwd, ".codex")
		if projectCodex != cx.Dir {
			cleanupCodexContextModeHooksInDir(projectCodex)
		}
	}
}

func cleanupWorkspaceCodexContextModeMcp(activeCodexDir string) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	projectCodex := filepath.Join(cwd, ".codex")
	if projectCodex == activeCodexDir {
		return
	}
	configPath := filepath.Join(projectCodex, "config.toml")
	raw, ok := util.ReadFileSafe(configPath)
	if !ok {
		return
	}
	next := util.RemoveBlock(raw, "mcp_servers.context-mode")
	next = util.RemoveBlock(next, "mcp_servers.context_mode")
	if next != raw {
		_ = util.WriteFile(configPath, next)
	}
}

func cleanupCodexContextModeHooksInDir(dir string) {
	hooksPath := filepath.Join(dir, "hooks.json")
	if !util.Exists(hooksPath) {
		return
	}
	raw, _ := util.ReadFileSafe(hooksPath)
	next := removeCodexContextModeHooks(loadOrdered(hooksPath))
	if next.Len() == 0 {
		_ = os.Remove(hooksPath)
		return
	}
	if s := util.StringifyJSON(next); s != raw {
		_ = util.WriteFile(hooksPath, s)
	}
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
	legacyPrefix := "tokless context-mode-hook codex " + strings.ToLower(event)
	altPrefix := "tokless codex-sessionstart"
	for _, h := range arr {
		hm, ok := h.(*util.OrderedMap)
		if !ok {
			continue
		}
		if cmd, ok := hm.Get("command"); ok {
			if s, ok := cmd.(string); ok && (strings.HasPrefix(s, prefix) || strings.HasPrefix(s, legacyPrefix) || strings.HasPrefix(s, altPrefix)) {
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
	RemoveOwner("claude", "context-mode")
	return true, nil
}

func ctxUnwireOpenCode(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	op := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(op.Config)
	if !ok {
		RemoveOwner("opencode", "context-mode")
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
	RemoveOwner("opencode", "context-mode")
	return true, nil
}

func ctxUnwireCodex(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	cx := util.CodexPathsResolved()
	if raw, ok := util.ReadFileSafe(cx.Config); ok {
		next := util.RemoveBlock(raw, "mcp_servers.context-mode")
		next = util.RemoveBlock(next, "mcp_servers.context_mode")
		if next != raw {
			_ = util.WriteFile(cx.Config, next)
		}
	}
	agents.RemoveCodexContextModeHook()
	cleanupCodexContextModeHooks()
	RemoveOwner("codex", "context-mode")
	return true, nil
}

// --- Antigravity (MCP + GEMINI.md, no PreToolUse hook) ---

const ctxGeminiMarker = "context-mode — MANDATORY routing rules"

func ctxWireAntigravity(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would add context-mode MCP and GEMINI.md for antigravity")
		return true, nil
	}
	agents.ConfigureAntigravityMcp("context-mode")
	agents.CleanupLegacyAntigravityContextMode()
	agents.CleanupDeadIdeHooks()
	agents.RemoveAntigravityEntry("command(echo)")
	agents.RemoveAntigravityContextModeHook()
	WriteOwner("antigravity", "context-mode")
	return ctxVerifyAntigravity(), nil
}

func ctxUnwireAntigravity(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would remove context-mode MCP and GEMINI.md")
		return true, nil
	}
	agents.RemoveAntigravityMcp("context-mode")
	agents.CleanupLegacyAntigravityContextMode()
	agents.CleanupDeadIdeHooks()
	agents.RemoveAntigravityContextModeHook()
	RemoveOwner("antigravity", "context-mode")
	if cwd, err := os.Getwd(); err == nil {
		dest := filepath.Join(cwd, "GEMINI.md")
		if raw, ok := util.ReadFileSafe(dest); ok && strings.Contains(raw, ctxGeminiMarker) {
			_ = os.Remove(dest)
		}
	}
	return true, nil
}

func ctxVerifyAntigravity() bool {
	return agents.AntigravityMcpHas("context-mode") && !agents.HasAntigravityContextModeHook()
}

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
	if !strings.Contains(raw, "[mcp_servers.context_mode]") {
		return false
	}
	agentsRaw, ok := util.ReadFileSafe(cx.Instructions)
	if !ok || !strings.Contains(agentsRaw, util.SectionsByOwner["context-mode"]) {
		return false
	}
	if !agents.HasCodexContextModeHook() {
		return false
	}
	if util.Exists(filepath.Join(cx.Dir, "hooks.json")) {
		data := loadOrdered(filepath.Join(cx.Dir, "hooks.json"))
		if hv, ok := data.Get("hooks"); ok {
			if hm, ok := hv.(*util.OrderedMap); ok {
				for _, ev := range []string{"PreCompact", "Stop", "SessionStart", "PostToolUse", "UserPromptSubmit", "PermissionRequest"} {
					if hasCodexHookEntry(hm, ev, "") {
						return false
					}
				}
			}
		}
	}
	return true
}

// hasCodexHookEntry returns true when hooks.json contains one of context-mode's entries.
func hasCodexHookEntry(hooks *util.OrderedMap, event, _ string) bool {
	v, ok := hooks.Get(event)
	if !ok {
		return false
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, entry := range arr {
		if isOursForEvent(entry, event) {
			return true
		}
	}
	return false
}

var contextMode = &core.ToolManifest{
	ID:           "context-mode",
	Label:        "Context-Mode",
	Description:  "Routes long context off-thread to a sandbox, keeping the agent's window small.",
	Homepage:     "https://github.com/mksglu/context-mode",
	InstallHint:  "npm i -g context-mode",
	Channel:      core.ChannelNpm,
	MinNodeMajor: contextModeMinNode,
	Install:      ctxEnsureInstalled,
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
// MD-block write order follows this sequence.
func Register() {
	core.RegisterTool(rtk)
	core.RegisterTool(caveman)
	core.RegisterTool(codegraph)
	core.RegisterTool(contextMode)
	core.RegisterTool(ponytail)
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
