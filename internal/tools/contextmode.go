package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

var (
	ctxNeedsCLIOnce sync.Once
	ctxNeedsCLI     bool
)

// CtxNeedsGlobalCLI reports whether a detected non-OpenCode harness needs the global CLI.
// Memoized per process because agent Detect() can spawn a CLI; tests recompute.
func CtxNeedsGlobalCLI() bool {
	if isTest() {
		return ctxDetectNeedsCLI()
	}
	ctxNeedsCLIOnce.Do(func() { ctxNeedsCLI = ctxDetectNeedsCLI() })
	return ctxNeedsCLI
}

func ctxDetectNeedsCLI() bool {
	hasOpenCode := false
	for _, a := range core.ListAgents() {
		if !a.Detect().Installed {
			continue
		}
		if a.ID == "opencode" || a.ID == "opencode2" {
			hasOpenCode = true
			continue
		}
		return true
	}
	return !hasOpenCode
}

func CtxOpenCodePinStale(latest string) bool {
	for _, v2 := range []bool{false, true} {
		if !HasOwner(ctxOpenCodeAgentID(v2), "context-mode") {
			continue
		}
		for _, s := range agents.OpenCodePluginSpecs(v2, ctxPluginPkg) {
			if ver, ok := strings.CutPrefix(s, ctxPluginPkg+"@"); ok && ver != latest {
				return true
			}
		}
	}
	return false
}

// CtxOpenCodeMigrateStale reports a leftover tokless MCP entry (legacy, or duplicating the plugin).
// A tokless-written entry proves ownership; no AGENTS.md marker needed.
func CtxOpenCodeMigrateStale() bool {
	for _, v2 := range []bool{false, true} {
		if a := core.GetAgent(ctxOpenCodeAgentID(v2)); a != nil && a.Detect().Installed &&
			agents.OpenCodeToklessMcpHas(v2, ctxPluginPkg) {
			return true
		}
	}
	return false
}

func CtxOpenCodeHideStale() bool {
	for _, v2 := range []bool{false, true} {
		if HasOwner(ctxOpenCodeAgentID(v2), "context-mode") &&
			len(agents.OpenCodePluginSpecs(v2, ctxPluginPkg)) > 0 &&
			agents.OpenCodeHidingMissing(v2, agents.OpenCodeHiddenCtxTools) {
			return true
		}
	}
	return false
}

func CtxToolPermsStale() bool {
	on := func(id string) bool {
		a := core.GetAgent(id)
		return a != nil && a.Detect().Installed && HasOwner(id, "context-mode")
	}
	return (on("claude") && ctxWiredAnyClaude() && agents.ClaudeMcpAllowMissing("context-mode")) ||
		(on("droid") && agents.DroidMcpHas("context-mode") && agents.DroidEnabledToolsMissing("context-mode"))
}

// ctxMcpHarnesses are the harnesses whose context-mode entry is a tokless run-mcp spawn.
var ctxMcpHarnesses = []string{"claude", "codex", "cursor", "antigravity", "copilot", "droid", "grok", "pi", "omp", "kilo", "cline", "amp", "kimi", "cmd", "dsh", "zcode"}

// CtxSpawnStale reports a tokless-owned context-mode MCP entry that no longer
// matches the spawn written today: a missing --ctx-agent host id, or tokless
// path drift.
func CtxSpawnStale() bool {
	for _, id := range ctxMcpHarnesses {
		a := core.GetAgent(id)
		if a == nil || !a.Detect().Installed || !HasOwner(id, "context-mode") {
			continue
		}
		if mcpHas(id, "context-mode", true) && !mcpHas(id, "context-mode", false) {
			return true
		}
	}
	return false
}

// Test seams.
var (
	ctxResolveRuntime func() (util.CtxRuntime, error)
	ctxResolveLatest  func() (version, minNode string, err error)
)

// ctxCLIReady: global CLI installed, Node satisfied, not a major behind latest.
func ctxCLIReady() (bool, string) {
	resolve := util.ResolveContextMode
	if ctxResolveRuntime != nil {
		resolve = ctxResolveRuntime
	} else if isTest() {
		return true, ""
	}
	rt, err := resolve()
	if err != nil {
		return false, "context-mode CLI is not usable: " + err.Error()
	}
	if rt.Degraded {
		return false, "context-mode needs Node >= " + rt.MinNode + "; skipped"
	}
	latest := util.ContextModeLatest
	if ctxResolveLatest != nil {
		latest = ctxResolveLatest
	} else if isTest() {
		return true, ""
	}
	if ver, minNode, err := latest(); err == nil && util.VersionMajor(rt.Version) < util.VersionMajor(ver) {
		return false, "context-mode " + rt.Version + " installed; upgrade to " + ver + " needs Node >= " + minNode + "; skipped"
	}
	return true, ""
}

func ctxGated(fn core.AgentFn) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if !opts.DryRun {
			if ok, why := ctxCLIReady(); !ok {
				util.L.Warn(why)
				return false, nil
			}
		}
		return fn(opts)
	}
}

// ctxGateCLI skips OpenCode, which runs a plugin rather than the CLI.
func ctxGateCLI(wires map[string]core.AgentFn) map[string]core.AgentFn {
	for id, fn := range wires {
		if id != "opencode" && id != "opencode2" {
			wires[id] = ctxGated(fn)
		}
	}
	return wires
}

func ctxEnsureInstalled(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would: npm install -g context-mode@<latest>")
		return true, nil
	}
	agents.MigrateCopilotContextModeHooks()
	if isTest() {
		return true, nil
	}
	opts.Reportf("checking", 0.1)
	if !CtxNeedsGlobalCLI() {
		opts.Reportf("plugin", 1)
		return true, nil
	}
	rt, rerr := util.ResolveContextMode()
	installed := rerr == nil
	ver, minNode, err := util.ContextModeLatest()
	if err != nil {
		if installed {
			util.L.Sub(util.C.Dim("couldn't check latest context-mode (" + err.Error() + "); keeping installed"))
			return true, nil
		}
		util.L.Err("couldn't resolve latest context-mode: " + err.Error())
		return false, nil
	}
	behind := installed && util.VersionMajor(rt.Version) < util.VersionMajor(ver)
	if installed && !opts.Upgrade && !behind && !rt.Degraded {
		opts.Reportf("already installed", 1)
		return true, nil
	}
	if _, err := util.EnsureNodeAtLeast(minNode, opts.AllowNodeInstall); err != nil {
		if installed && !behind && !rt.Degraded {
			util.L.Debug(err.Error())
			util.L.Warn("context-mode " + ver + " needs Node >= " + minNode + "; keeping " + rt.Version)
			return true, nil
		}
		util.L.Debug(err.Error())
		util.L.Warn("context-mode " + ver + " needs Node >= " + minNode + "; skipped")
		return ctxHasOpenCode(), nil
	}
	if installed && !opts.Upgrade && !behind {
		opts.Reportf("ready", 1)
		return true, nil
	}
	opts.Reportf("npm install -g", 0.4)
	v, ok, _ := util.NpmGlobalInstall("context-mode", ver)
	if !ok {
		util.L.Err("context-mode install failed across all strategies (npm + tarball fallback).")
		util.L.Sub("Each attempt was logged above. Common causes: old Node, no build tools, or a registry mirror.")
		return false, nil
	}
	opts.Reportf("ready", 1)
	util.L.Sub(util.C.Dim("context-mode @" + v + " installed"))
	return true, nil
}

func ctxHasOpenCode() bool {
	for _, a := range core.ListAgents() {
		if (a.ID == "opencode" || a.ID == "opencode2") && a.Detect().Installed {
			return true
		}
	}
	return false
}

// --- Claude ---

func ctxWireClaude(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would: claude mcp add context-mode -- context-mode")
		return true, nil
	}
	spawn := util.ContextModeSpawnFor("claude")
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
	mcpConfigure("claude", "context-mode")
	agents.AllowClaudeMcpToolProjectLocal("context-mode")
	WriteOwner("claude", "context-mode")
	util.L.Sub(util.C.Dim("tip: to enable slash commands, type inside Claude Code: /plugin marketplace add mksglu/context-mode && /plugin install context-mode@context-mode"))
	return ctxVerifyClaude(), nil
}

// --- OpenCode ---

const ctxPluginPkg = "context-mode"

func ctxOpenCodeAgentID(v2 bool) string {
	if v2 {
		return "opencode2"
	}
	return "opencode"
}

// ctxPluginSpec returns the latest pinned spec, else the one already configured.
func ctxPluginSpec(v2 bool) (string, error) {
	ver, err := util.ContextModeLatestVersion()
	if err == nil {
		return ctxPluginPkg + "@" + ver, nil
	}
	for _, s := range agents.OpenCodePluginSpecs(v2, ctxPluginPkg) {
		if strings.HasPrefix(s, ctxPluginPkg+"@") {
			return s, nil
		}
	}
	return "", err
}

func ctxWireOpenCodeShape(opts core.RunOpts, v2 bool) (bool, error) {
	key := agents.OpenCodePluginKey(v2)
	if opts.DryRun {
		util.L.Sub("[dry-run] would add context-mode@<latest> to OpenCode " + key)
		return true, nil
	}
	spec, err := ctxPluginSpec(v2)
	if err != nil {
		util.L.Err("couldn't resolve context-mode version: " + err.Error())
		return false, err
	}
	if _, err := agents.SetOpenCodePluginHiding(v2, ctxPluginPkg, spec, agents.OpenCodeHiddenCtxTools); err != nil {
		util.L.Err("couldn't write context-mode plugin to OpenCode config: " + err.Error())
		return false, err
	}
	WriteOwner(ctxOpenCodeAgentID(v2), "context-mode")
	return ctxVerifyOpenCodeShape(v2), nil
}

func ctxWireOpenCode(opts core.RunOpts) (bool, error) { return ctxWireOpenCodeShape(opts, false) }

func ctxWireOpenCodeV2(opts core.RunOpts) (bool, error) { return ctxWireOpenCodeShape(opts, true) }

func ctxWireKilo(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	if !mcpConfigure("kilo", "context-mode") {
		return false, nil
	}
	kiloWriteOwner("context-mode")
	return mcpHas("kilo", "context-mode", false) && kiloHasOwner("context-mode"), nil
}

func ctxUnwireKilo(core.RunOpts) (bool, error) {
	if !mcpRemove("kilo", "context-mode") {
		return false, nil
	}
	kiloRemoveOwner("context-mode")
	return true, nil
}

func ctxWireCline(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	if !mcpConfigure("cline", "context-mode") {
		return false, nil
	}
	WriteOwner("cline", "context-mode")
	return mcpHas("cline", "context-mode", false) && HasOwner("cline", "context-mode"), nil
}

func ctxUnwireCline(core.RunOpts) (bool, error) {
	if !mcpRemove("cline", "context-mode") {
		return false, nil
	}
	RemoveOwner("cline", "context-mode")
	return true, nil
}

// --- Codex ---

func ctxWireCodex(opts core.RunOpts) (bool, error) {
	return wireCodexManual(), nil
}

func wireCodexManual() bool {
	cx := util.CodexPathsResolved()
	_ = util.EnsureDir(cx.Dir)
	if raw, ok := util.ReadFileSafe(cx.Config); ok {
		if next := util.RemoveBlock(raw, "mcp_servers.context-mode"); next != raw {
			_ = util.WriteFile(cx.Config, next)
		}
	}
	if !mcpConfigure("codex", "context-mode") {
		return false
	}

	cleanupCodexContextModeHooks()
	cleanupWorkspaceCodexContextModeMcp(cx.Dir)
	writeCodexAgentsMd()

	return mcpHas("codex", "context-mode", false)
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
	mcpRemove("claude", "context-mode")
	RemoveOwner("claude", "context-mode")
	return true, nil
}

func ctxUnwireOpenCodeShape(opts core.RunOpts, v2 bool) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	id := ctxOpenCodeAgentID(v2)
	if !HasOwner(id, "context-mode") {
		_, err := agents.RemoveOpenCodeToklessMcp(v2, ctxPluginPkg)
		return err == nil, err
	}
	_, err := agents.RemoveOpenCodePluginHiding(v2, ctxPluginPkg, agents.OpenCodeHiddenCtxTools)
	if err != nil {
		return false, err
	}
	RemoveOwner(id, "context-mode")
	return true, nil
}

func ctxUnwireOpenCode(opts core.RunOpts) (bool, error) { return ctxUnwireOpenCodeShape(opts, false) }

func ctxUnwireOpenCodeV2(opts core.RunOpts) (bool, error) {
	return ctxUnwireOpenCodeShape(opts, true)
}

func ctxUnwireCodex(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	mcpRemove("codex", "context-mode")
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
	if !mcpConfigure("grok", "context-mode") {
		return false, nil
	}
	WriteOwner("grok", "context-mode")
	if !HasOwner("grok", "context-mode") {
		mcpRemove("grok", "context-mode")
		return false, nil
	}
	return ctxVerifyGrok(), nil
}

func ctxUnwireGrok(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	if !mcpRemove("grok", "context-mode") {
		return false, nil
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
	mcpConfigure("droid", "context-mode")
	agents.RemoveDroidCtxModePreToolUse()
	WriteOwner("droid", "context-mode")
	return ctxVerifyDroid(), nil
}

func ctxUnwireDroid(opts core.RunOpts) (bool, error) {
	mcpRemove("droid", "context-mode")
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
	return agents.CopilotMcpBounded("context-mode", false) && agents.CopilotMcpBounded("context-mode", true) && !agents.HasCopilotContextModeHook() && !agents.HasCopilotIdeContextModeHook()
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

func ctxWiredAnyOpenCodeShape(v2 bool) bool {
	owned := HasOwner(ctxOpenCodeAgentID(v2), "context-mode") && len(agents.OpenCodePluginSpecs(v2, ctxPluginPkg)) > 0
	return owned || agents.OpenCodeToklessMcpHas(v2, ctxPluginPkg)
}

func ctxWiredAnyOpenCode() bool { return ctxWiredAnyOpenCodeShape(false) }

func ctxWiredAnyOpenCodeV2() bool { return ctxWiredAnyOpenCodeShape(true) }

// ctxWiredAnyCodex reports whether any context-mode MCP block exists (hyphen or underscore).
func ctxWiredAnyCodex() bool {
	raw, ok := util.ReadFileSafe(util.CodexPathsResolved().Config)
	if !ok {
		return false
	}
	return strings.Contains(raw, "[mcp_servers.context_mode]") || strings.Contains(raw, "[mcp_servers.context-mode]")
}

const ctxGeminiMarker = "context-mode — MANDATORY routing rules"

func ctxWireAntigravity(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would add context-mode MCP and GEMINI.md for antigravity")
		return true, nil
	}
	mcpConfigure("antigravity", "context-mode")
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
	mcpRemove("antigravity", "context-mode")
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

// --- Copilot (MCP + copilot-instructions.md) ---

func ctxWireCopilot(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would add context-mode MCP + copilot-instructions.md for copilot")
		return true, nil
	}
	err := withCopilotTransaction(func() error {
		if reply := mcpAdapter("copilot").ConfigureMcp("context-mode"); reply.Err != nil {
			return reply.Err
		}
		agents.MigrateCopilotContextModeHooks()
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
		if reply := mcpAdapter("copilot").RemoveMcp("context-mode"); reply.Err != nil {
			return reply.Err
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

func ctxVerifyCodex() bool {
	cx := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(cx.Config)
	if !ok {
		return false
	}
	if strings.Contains(raw, "[mcp_servers.context-mode]") {
		return false
	}
	if !mcpHas("codex", "context-mode", false) {
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

// ctxVerifyOpenCodeShape: a pinned plugin spec exists and no tokless MCP entry duplicates it.
func ctxVerifyOpenCodeShape(v2 bool) bool {
	pinned := false
	for _, s := range agents.OpenCodePluginSpecs(v2, ctxPluginPkg) {
		if strings.HasPrefix(s, ctxPluginPkg+"@") && len(s) > len(ctxPluginPkg)+1 {
			pinned = true
		}
	}
	return pinned && !agents.OpenCodeToklessMcpHas(v2, ctxPluginPkg)
}

func ctxVerifyOpenCode() bool { return ctxVerifyOpenCodeShape(false) }

func ctxVerifyOpenCodeV2() bool { return ctxVerifyOpenCodeShape(true) }

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
	ID:          "context-mode",
	Label:       "Context-Mode",
	Description: "Routes long context off-thread to a sandbox, keeping the agent's window small.",
	Homepage:    "https://github.com/mksglu/context-mode",
	InstallHint: "npm i -g context-mode",
	Channel:     core.ChannelNpm,
	Install:     ctxEnsureInstalled,
	WireFor: ctxGateCLI(map[string]core.AgentFn{
		"claude":    ctxWireClaude,
		"opencode":  ctxWireOpenCode,
		"opencode2": ctxWireOpenCodeV2,
		"codex":     ctxWireCodex,
		"cursor": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			if !mcpConfigure("cursor", "context-mode") {
				return false, nil
			}
			return mcpHas("cursor", "context-mode", false), nil
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
			if !mcpConfigure("pi", "context-mode") {
				return false, nil
			}
			WriteOwner("pi", "context-mode")
			return ctxVerifyPi(), nil
		},
		"omp": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would add Context-Mode MCP to ~/.omp/agent/mcp.json")
				return true, nil
			}
			if !mcpConfigure("omp", "context-mode") {
				return false, nil
			}
			WriteOwner("omp", "context-mode")
			return HasOwner("omp", "context-mode"), nil
		},
		"kilo":  ctxWireKilo,
		"cline": ctxWireCline,
		"amp":   ctxWireHarness("amp"), "kimi": ctxWireHarness("kimi"), "cmd": ctxWireHarness("cmd"), "dsh": ctxWireHarness("dsh"), "zcode": ctxWireHarness("zcode"),
	}),
	UnwireFor: map[string]core.AgentFn{
		"claude":    ctxUnwireClaude,
		"opencode":  ctxUnwireOpenCode,
		"opencode2": ctxUnwireOpenCodeV2,
		"codex":     ctxUnwireCodex,
		"cursor": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			if !mcpRemove("cursor", "context-mode") {
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
			mcpRemove("pi", "context-mode")
			RemoveOwner("pi", "context-mode")
			return true, nil
		},
		"omp": func(core.RunOpts) (bool, error) {
			if !mcpRemove("omp", "context-mode") {
				return false, nil
			}
			RemoveOwner("omp", "context-mode")
			return true, nil
		},
		"kilo":  ctxUnwireKilo,
		"cline": ctxUnwireCline,
		"amp":   ctxUnwireHarness("amp"), "kimi": ctxUnwireHarness("kimi"), "cmd": ctxUnwireHarness("cmd"), "dsh": ctxUnwireHarness("dsh"), "zcode": ctxUnwireHarness("zcode"),
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":    func() *bool { return core.BoolPtr(ctxVerifyClaude()) },
		"opencode":  func() *bool { return core.BoolPtr(ctxVerifyOpenCode()) },
		"opencode2": func() *bool { return core.BoolPtr(ctxVerifyOpenCodeV2()) },
		"codex":     func() *bool { return core.BoolPtr(ctxVerifyCodex()) },
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
			spawn := util.ContextModeSpawnFor("kilo")
			expected := append([]string{spawn.Command}, spawn.Args...)
			return core.BoolPtr(agents.KiloMcpMatches("context-mode", expected) && kiloHasOwner("context-mode"))
		},
		"cline": func() *bool {
			spawn := util.ContextModeSpawnFor("cline")
			expected := append([]string{spawn.Command}, spawn.Args...)
			return core.BoolPtr(agents.ClineMcpMatches("context-mode", expected) && HasOwner("cline", "context-mode"))
		},
		"amp": func() *bool {
			return core.BoolPtr(mcpHas("amp", "context-mode", false) && HasOwner("amp", "context-mode"))
		}, "kimi": func() *bool {
			return core.BoolPtr(mcpHas("kimi", "context-mode", false) && HasOwner("kimi", "context-mode"))
		}, "cmd": func() *bool {
			return core.BoolPtr(mcpHas("cmd", "context-mode", false) && HasOwner("cmd", "context-mode"))
		}, "dsh": func() *bool {
			return core.BoolPtr(mcpHas("dsh", "context-mode", false) && HasOwner("dsh", "context-mode"))
		}, "zcode": func() *bool {
			return core.BoolPtr(mcpHas("zcode", "context-mode", false) && HasOwner("zcode", "context-mode"))
		},
	},
	WiredAnyFor: map[string]core.VerifyFn{
		"claude":    func() *bool { return core.BoolPtr(ctxWiredAnyClaude()) },
		"opencode":  func() *bool { return core.BoolPtr(ctxWiredAnyOpenCode()) },
		"opencode2": func() *bool { return core.BoolPtr(ctxWiredAnyOpenCodeV2()) },
		"codex":     func() *bool { return core.BoolPtr(ctxWiredAnyCodex()) },
		"cursor":    func() *bool { return core.BoolPtr(mcpHas("cursor", "context-mode", true)) }, "grok": func() *bool { return core.BoolPtr(mcpHas("grok", "context-mode", true)) }, "omp": func() *bool { return core.BoolPtr(mcpHas("omp", "context-mode", true)) }, "kilo": func() *bool { return core.BoolPtr(mcpHas("kilo", "context-mode", true)) }, "cline": func() *bool { return core.BoolPtr(mcpHas("cline", "context-mode", true)) },
		"droid":       func() *bool { return core.BoolPtr(agents.DroidMcpHas("context-mode")) },
		"pi":          func() *bool { return core.BoolPtr(agents.PiMcpHas("context-mode")) },
		"antigravity": func() *bool { return core.BoolPtr(agents.AntigravityMcpHas("context-mode")) },
		"copilot":     func() *bool { return core.BoolPtr(agents.CopilotMcpHas("context-mode")) },
		"amp":         func() *bool { return core.BoolPtr(mcpHas("amp", "context-mode", true)) }, "kimi": func() *bool { return core.BoolPtr(mcpHas("kimi", "context-mode", true)) }, "cmd": func() *bool { return core.BoolPtr(mcpHas("cmd", "context-mode", true)) }, "dsh": func() *bool { return core.BoolPtr(mcpHas("dsh", "context-mode", true)) }, "zcode": func() *bool { return core.BoolPtr(mcpHas("zcode", "context-mode", true)) },
	},
}

func ctxWireHarness(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		if !mcpConfigure(agent, "context-mode") {
			return false, nil
		}
		WriteOwner(agent, "context-mode")
		return mcpHas(agent, "context-mode", false) && HasOwner(agent, "context-mode"), nil
	}
}
func ctxUnwireHarness(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		mcpRemove(agent, "context-mode")
		RemoveOwner(agent, "context-mode")
		return true, nil
	}
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
	core.RegisterTool(principles)
	WireHeadroomTools()
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
