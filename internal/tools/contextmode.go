package tools

import (
	"os"
	"path/filepath"
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
	v, ok := util.NpmGlobalInstall("context-mode", "latest")
	if !ok {
		util.L.Err("context-mode install failed across all strategies (npm + tarball fallback).")
		return false, nil
	}
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
		return true, nil
	}
	if util.Which("claude") == "" {
		util.L.Err("claude CLI not on PATH — install Claude Code first (https://claude.com/claude-code).")
		return false, nil
	}
	args := append([]string{"mcp", "add", "context-mode", "--", spawn.Command}, spawn.Args...)
	r := util.Run("claude", args, util.RunOptions{Capture: true})
	if r.Code != 0 {
		util.L.Err("claude mcp add failed: " + clip(r.Stderr))
		return false, nil
	}
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
	plugins := getArr(cfg, "plugin")
	found := false
	for _, p := range plugins {
		if s, ok := p.(string); ok && pluginIsContextMode(s) {
			found = true
			break
		}
	}
	if !found {
		plugins = append(plugins, "context-mode")
	}
	cfg.Set("plugin", plugins)
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
	_ = util.WriteFile(op.Config, util.StringifyJSON(cfg))
	if isTest() {
		return true, nil
	}
	copyOpenCodeAgentsMd(op.Dir)
	runPostinstallInOpenCodeCache()
	return true, nil
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
	block := util.NewTomlBlock("mcp_servers.context-mode")
	block.Set("command", "context-mode")
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
		"claude":   ctxWireClaude,
		"opencode": ctxWireOpenCode,
		"codex":    ctxWireCodex,
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":   ctxUnwireClaude,
		"opencode": ctxUnwireOpenCode,
		"codex":    ctxUnwireCodex,
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":   func() *bool { return core.BoolPtr(ctxVerifyClaude()) },
		"opencode": func() *bool { return core.BoolPtr(ctxVerifyOpenCode()) },
		"codex":    func() *bool { return core.BoolPtr(ctxVerifyCodex()) },
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
