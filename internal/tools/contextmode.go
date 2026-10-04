package tools

import (
	"encoding/json"
	"fmt"
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
		return ctxVerifyClaude(), nil
	}
	agents.ConfigureClaudeMcp("context-mode")
	agents.AllowClaudeMcpToolProjectLocal("context-mode")
	WriteOwner("claude", "context-mode")
	util.L.Sub(util.C.Dim("tip: to enable slash commands, type inside Claude Code: /plugin marketplace add mksglu/context-mode && /plugin install context-mode@context-mode"))
	return ctxVerifyClaude(), nil
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
	removeContextModePlugin(cfg)
	_ = util.WriteFile(op.Config, util.StringifyJSON(cfg))
	agents.ConfigureOpenCodeMcp("context-mode")
	WriteOwner("opencode", "context-mode")
	if isTest() {
		return ctxVerifyOpenCode(), nil
	}
	cleanAllContextModeCache()
	runPostinstallInOpenCodeCache()
	return ctxVerifyOpenCode(), nil
}

func ctxWireKilo(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	spawn := util.PickMcpSpawn("context-mode")
	expected := append([]string{spawn.Command}, spawn.Args...)
	if _, _, err := agents.ConfigureKiloMcpSafe("context-mode", expected); err != nil {
		return false, err
	}
	kiloWriteOwner("context-mode")
	return agents.KiloMcpMatches("context-mode", expected) && kiloHasOwner("context-mode"), nil
}

func ctxUnwireKilo(core.RunOpts) (bool, error) {
	if !agents.RemoveKiloMcp("context-mode") {
		return false, nil
	}
	kiloRemoveOwner("context-mode")
	return true, nil
}

func ctxWireCline(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	spawn := util.PickMcpSpawn("context-mode")
	expected := append([]string{spawn.Command}, spawn.Args...)
	if _, _, err := agents.ConfigureClineMcpSafe("context-mode", expected); err != nil {
		return false, err
	}
	WriteOwner("cline", "context-mode")
	return agents.ClineMcpMatches("context-mode", expected) && HasOwner("cline", "context-mode"), nil
}

func ctxUnwireCline(core.RunOpts) (bool, error) {
	if !agents.RemoveClineMcp("context-mode") {
		return false, nil
	}
	RemoveOwner("cline", "context-mode")
	return true, nil
}

func removeContextModePlugin(cfg *util.OrderedMap) {
	plugins := getArr(cfg, "plugin")
	kept := make([]any, 0, len(plugins))
	for _, p := range plugins {
		if s, ok := p.(string); ok && pluginIsContextMode(s) {
			continue
		}
		kept = append(kept, p)
	}
	cfg.Set("plugin", kept)
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

	return ctxVerifyCodex()
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
	cleanupCodexContextModeHooks()
	RemoveOwner("codex", "context-mode")
	return true, nil
}

// --- Grok ---

func ctxWireGrok(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would add bounded context-mode MCP to Grok")
		return true, nil
	}
	if _, _, err := agents.ConfigureGrokMcp("context-mode"); err != nil {
		return false, err
	}
	WriteOwner("grok", "context-mode")
	if !HasOwner("grok", "context-mode") {
		_, _ = agents.RemoveGrokMcp("context-mode")
		return false, nil
	}
	return ctxVerifyGrok(), nil
}

func ctxUnwireGrok(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	if _, err := agents.RemoveGrokMcp("context-mode"); err != nil {
		return false, err
	}
	RemoveOwner("grok", "context-mode")
	return true, nil
}

// --- Droid ---

func ctxWireDroid(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would: add context-mode MCP to ~/.factory/mcp.json + AGENTS.md")
		return true, nil
	}
	agents.ConfigureDroidMcp("context-mode")
	agents.RemoveDroidCtxModePreToolUse()
	WriteOwner("droid", "context-mode")
	return ctxVerifyDroid(), nil
}

func ctxUnwireDroid(opts core.RunOpts) (bool, error) {
	agents.RemoveDroidMcp("context-mode")
	agents.RemoveDroidCtxModePreToolUse()
	RemoveOwner("droid", "context-mode")
	return true, nil
}

func ctxVerifyDroid() bool {
	return agents.DroidMcpBounded("context-mode") && !agents.HasDroidCtxModePreToolUse()
}

func ctxVerifyPi() bool {
	return agents.PiMcpBounded("context-mode")
}

func ctxVerifyCopilot() bool {
	return agents.CopilotMcpBounded("context-mode", false) && agents.CopilotMcpBounded("context-mode", true) && agents.HasCopilotContextModeHook() && agents.HasCopilotIdeContextModeHook()
}

func ctxVerifyGrok() bool {
	return agents.GrokContextModeMcpHas() && HasOwner("grok", "context-mode")
}

// --- WiredAny (loose presence, for resync gate only) ---

// ctxWiredAnyClaude reports whether context-mode appears in mcpServers (any shape).
func ctxWiredAnyClaude() bool {
	raw, ok := util.ReadFileSafe(util.ClaudeCodePaths().GlobalJSON)
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

// ctxWiredAnyOpenCode reports whether context-mode appears as a plugin or MCP entry.
func ctxWiredAnyOpenCode() bool {
	raw, ok := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if pv, ok := cfg.Get("plugin"); ok {
		if arr, ok := pv.([]any); ok {
			for _, p := range arr {
				if s, ok := p.(string); ok && pluginIsContextMode(s) {
					return true
				}
			}
		}
	}
	if mv, ok := cfg.Get("mcp"); ok {
		if mm, ok := mv.(*util.OrderedMap); ok {
			if _, has := mm.Get("context-mode"); has {
				return true
			}
		}
	}
	return false
}

// ctxWiredAnyCodex reports whether any context-mode MCP block exists (hyphen or underscore).
func ctxWiredAnyCodex() bool {
	raw, ok := util.ReadFileSafe(util.CodexPathsResolved().Config)
	if !ok {
		return false
	}
	return strings.Contains(raw, "[mcp_servers.context_mode]") || strings.Contains(raw, "[mcp_servers.context-mode]")
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
	return agents.AntigravityMcpBounded("context-mode") && !agents.HasAntigravityContextModeHook()
}

// --- Copilot (MCP + hooks + copilot-instructions.md) ---

func ctxWireCopilot(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would add context-mode MCP + hooks + copilot-instructions.md for copilot")
		return true, nil
	}
	err := withCopilotTransaction(func() error {
		if _, _, err := agents.ConfigureCopilotMcpSafe("context-mode"); err != nil {
			return err
		}
		if _, _, err := agents.ConfigureCopilotIdeMcpSafe("context-mode"); err != nil {
			return err
		}
		if err := agents.InstallCopilotContextModeHookSafe(); err != nil {
			return err
		}
		if err := agents.InstallCopilotIdeContextModeHookSafe(); err != nil {
			return err
		}
		if !WriteOwner("copilot", "context-mode") && !HasOwner("copilot", "context-mode") {
			return fmt.Errorf("failed to write Copilot context-mode owner")
		}
		if err := agents.SyncCopilotIdeInstructionsSafe(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return ctxVerifyCopilot(), nil
}

func ctxUnwireCopilot(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	err := withCopilotTransaction(func() error {
		if _, err := agents.RemoveCopilotMcpSafe("context-mode"); err != nil {
			return err
		}
		if _, err := agents.RemoveCopilotIdeMcpSafe("context-mode"); err != nil {
			return err
		}
		if err := agents.RemoveCopilotContextModeHookSafe(); err != nil {
			return err
		}
		if err := agents.RemoveCopilotIdeContextModeHookSafe(); err != nil {
			return err
		}
		if err := RemoveOwnerSafe("copilot", "context-mode"); err != nil {
			return err
		}
		if err := agents.SyncCopilotIdeInstructionsSafe(); err != nil {
			return err
		}
		return nil
	})
	return err == nil, err
}

// --- verify ---

func ctxVerifyClaude() bool {
	return claudeMcpBounded("context-mode")
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
	// Wire path removes the plugin and writes bounded MCP entry, so plugin
	// presence = bypass = fail.
	if pv, ok := cfg.Get("plugin"); ok {
		if arr, ok := pv.([]any); ok {
			for _, p := range arr {
				if s, ok := p.(string); ok && pluginIsContextMode(s) {
					return false
				}
			}
		}
	}
	return openCodeMcpBounded("context-mode")
}

func ctxVerifyCodex() bool {
	cx := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(cx.Config)
	if !ok {
		return false
	}
	// Legacy hyphen block must not exist — wire path writes the underscore block only.
	if strings.Contains(raw, "[mcp_servers.context-mode]") {
		return false
	}
	spawn := util.McpSpawnFor("context-mode")
	blockText, hasBlock := util.BlockText(raw, "mcp_servers.context_mode")
	if !hasBlock {
		return false
	}
	b := parseCodexContextModeBlock(blockText)
	if b.command != spawn.Command {
		return false
	}
	if !mcpStrArrEq(b.args, spawn.Args) {
		return false
	}
	agentsRaw, ok := util.ReadFileSafe(cx.Instructions)
	if !ok || !strings.Contains(agentsRaw, util.SectionsByOwner["context-mode"]) {
		return false
	}
	for _, dir := range codexHookDirs(cx.Dir) {
		if !util.Exists(filepath.Join(dir, "hooks.json")) {
			continue
		}
		data := loadOrdered(filepath.Join(dir, "hooks.json"))
		if hv, ok := data.Get("hooks"); ok {
			if hm, ok := hv.(*util.OrderedMap); ok {
				for _, ev := range []string{"PreToolUse", "PreCompact", "Stop", "SessionStart", "PostToolUse", "UserPromptSubmit", "PermissionRequest"} {
					if hasCodexHookEntry(hm, ev, "") {
						return false
					}
				}
			}
		}
	}
	return true
}

// codexCtxModeBlock holds the parsed command/args from a [mcp_servers.context_mode] block.
type codexCtxModeBlock struct {
	command string
	args    any
}

// parseCodexContextModeBlock extracts command and args fields from a TOML block text.
func parseCodexContextModeBlock(blockText string) codexCtxModeBlock {
	var b codexCtxModeBlock
	for _, line := range strings.Split(blockText, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") || t == "" {
			continue
		}
		if k, v, ok := strings.Cut(t, "="); ok {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(stripTomlCommentLocal(v))
			switch k {
			case "command":
				if s, err := strconv.Unquote(v); err == nil {
					b.command = s
				}
			case "args":
				if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
					if elems := parseTomlArgsArray(v); elems != nil {
						b.args = toAny(elems)
					}
				}
			}
		}
	}
	return b
}

// parseTomlArgsArray parses a double-quoted TOML array string into []string.
func parseTomlArgsArray(v string) []string {
	var arr []string
	if err := json.Unmarshal([]byte(v), &arr); err != nil {
		return nil
	}
	return arr
}

// stripTomlCommentLocal strips a trailing # comment, respecting quoted strings.
func stripTomlCommentLocal(t string) string {
	inStr, inLiteral, esc := false, false, false
	for i := 0; i < len(t); i++ {
		c := t[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if inLiteral {
			if c == '\'' {
				inLiteral = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '\'':
			inLiteral = true
		case '#':
			return strings.TrimRight(t[:i], " \t")
		}
	}
	return t
}

func codexHookDirs(activeCodexDir string) []string {
	dirs := []string{activeCodexDir}
	if cwd, err := os.Getwd(); err == nil {
		if project := filepath.Join(cwd, ".codex"); project != activeCodexDir {
			dirs = append(dirs, project)
		}
	}
	return dirs
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
		"claude":   ctxWireClaude,
		"opencode": ctxWireOpenCode,
		"codex":    ctxWireCodex,
		"cursor": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			if changed, _ := agents.ConfigureCursorMcp("context-mode"); !changed && !agents.CursorMcpHas("context-mode") {
				return false, nil
			}
			if !agents.ConfigureCursorMcpPermissions("context-mode") {
				return false, nil
			}
			return agents.CursorMcpHas("context-mode") && agents.HasCursorMcpPermissions("context-mode"), nil
		},
		"antigravity": ctxWireAntigravity,
		"copilot":     ctxWireCopilot,
		"droid":       ctxWireDroid,
		"grok":        ctxWireGrok,
		"pi": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would: upstream context-mode MCP via Pi built-in MCP (not pi package)")
				return true, nil
			}
			agents.PiPurgeContextModePackages()
			if !agents.PiRemoveMcpAdapter() {
				return false, nil
			}
			agents.ConfigurePiMcp("context-mode")
			WriteOwner("pi", "context-mode")
			return ctxVerifyPi(), nil
		},
		"omp": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would add Context-Mode MCP to ~/.omp/agent/mcp.json")
				return true, nil
			}
			agents.ConfigureOmpMcp("context-mode")
			if !agents.OmpMcpHas("context-mode") {
				return false, nil
			}
			WriteOwner("omp", "context-mode")
			return HasOwner("omp", "context-mode"), nil
		},
		"kilo":  ctxWireKilo,
		"cline": ctxWireCline,
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":   ctxUnwireClaude,
		"opencode": ctxUnwireOpenCode,
		"codex":    ctxUnwireCodex,
		"cursor": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			if !agents.RemoveCursorMcp("context-mode") || !agents.RemoveCursorMcpPermissions("context-mode") {
				return false, nil
			}
			RemoveOwner("cursor", "context-mode")
			return true, nil
		},
		"antigravity": ctxUnwireAntigravity,
		"copilot":     ctxUnwireCopilot,
		"droid":       ctxUnwireDroid,
		"grok":        ctxUnwireGrok,
		"pi": func(opts core.RunOpts) (bool, error) {
			agents.PiPurgeContextModePackages()
			agents.RemovePiMcp("context-mode")
			RemoveOwner("pi", "context-mode")
			return true, nil
		},
		"omp": func(core.RunOpts) (bool, error) {
			if !agents.RemoveOmpMcp("context-mode") {
				return false, nil
			}
			RemoveOwner("omp", "context-mode")
			return true, nil
		},
		"kilo":  ctxUnwireKilo,
		"cline": ctxUnwireCline,
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":   func() *bool { return core.BoolPtr(ctxVerifyClaude()) },
		"opencode": func() *bool { return core.BoolPtr(ctxVerifyOpenCode()) },
		"codex":    func() *bool { return core.BoolPtr(ctxVerifyCodex()) },
		"cursor": func() *bool {
			return core.BoolPtr(agents.CursorMcpHas("context-mode") && agents.HasCursorMcpPermissions("context-mode"))
		},
		"antigravity": func() *bool { return core.BoolPtr(ctxVerifyAntigravity()) },
		"copilot": func() *bool {
			return core.BoolPtr(ctxVerifyCopilot())
		},
		"droid": func() *bool { return core.BoolPtr(ctxVerifyDroid()) },
		"grok":  func() *bool { return core.BoolPtr(ctxVerifyGrok()) },
		"pi":    func() *bool { return core.BoolPtr(ctxVerifyPi()) },
		"omp":   func() *bool { return core.BoolPtr(agents.OmpMcpHas("context-mode") && HasOwner("omp", "context-mode")) },
		"kilo": func() *bool {
			spawn := util.PickMcpSpawn("context-mode")
			expected := append([]string{spawn.Command}, spawn.Args...)
			return core.BoolPtr(agents.KiloMcpMatches("context-mode", expected) && kiloHasOwner("context-mode"))
		},
		"cline": func() *bool {
			spawn := util.PickMcpSpawn("context-mode")
			expected := append([]string{spawn.Command}, spawn.Args...)
			return core.BoolPtr(agents.ClineMcpMatches("context-mode", expected) && HasOwner("cline", "context-mode"))
		},
	},
	WiredAnyFor: map[string]core.VerifyFn{
		"claude":      func() *bool { return core.BoolPtr(ctxWiredAnyClaude()) },
		"opencode":    func() *bool { return core.BoolPtr(ctxWiredAnyOpenCode()) },
		"codex":       func() *bool { return core.BoolPtr(ctxWiredAnyCodex()) },
		"droid":       func() *bool { return core.BoolPtr(agents.DroidMcpHas("context-mode")) },
		"pi":          func() *bool { return core.BoolPtr(agents.PiMcpHas("context-mode")) },
		"antigravity": func() *bool { return core.BoolPtr(agents.AntigravityMcpHas("context-mode")) },
		"copilot":     func() *bool { return core.BoolPtr(agents.CopilotMcpHas("context-mode")) },
	},
}

// Register wires all tools into the core registry, in canonical order.
// MD-block write order follows this sequence.
func Register() {
	core.RegisterTool(rtk)
	core.RegisterTool(caveman)
	core.RegisterTool(codegraph)
	core.RegisterTool(contextMode)
	core.RegisterTool(headroom)
	core.RegisterTool(ponytail)
	core.RegisterTool(projectmem)
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
