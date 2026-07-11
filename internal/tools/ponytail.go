package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

const ponytailRepo = "DietrichGebert/ponytail"
const ponytailOpencodePkg = "@dietrichgebert/ponytail"

func ponytailExec(bin string, args []string, opts core.RunOpts, dryHint string, env ...string) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would run: " + dryHint)
		return true, nil
	}
	if isTest() {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	r := util.Run(bin, args, util.RunOptions{Capture: true, Env: env, Ctx: ctx})
	if r.Code != 0 {
		util.L.Err("ponytail command failed: " + clip(r.Stderr))
		return false, nil
	}
	return true, nil
}

func stampPonytailVersion() {
	if v := util.LatestVersionFor("ponytail"); v != nil {
		util.StampPonytailVersion(*v)
	}
}

// registerPonytailOpencode adds the upstream ponytail plugin to opencode.json.
func registerPonytailOpencode() {
	op := util.OpenCodePathsResolved()
	raw, _ := util.ReadFileSafe(op.Config)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	var plugins []any
	if pv, ok := cfg.Get("plugin"); ok {
		if arr, ok := pv.([]any); ok {
			plugins = arr
		}
	}
	if ponytailAlreadyInOpencode(plugins) {
		return
	}
	plugins = insertBeforeOpencodeContextMode(plugins, ponytailOpencodePkg)
	cfg.Set("plugin", plugins)
	_ = util.WriteFile(op.Config, util.StringifyJSON(cfg))
}

func ponytailAlreadyInOpencode(plugins []any) bool {
	for _, p := range plugins {
		if s, ok := p.(string); ok && strings.EqualFold(s, ponytailOpencodePkg) {
			return true
		}
	}
	return false
}

func insertBeforeOpencodeContextMode(plugins []any, entry string) []any {
	for i, p := range plugins {
		if s, ok := p.(string); ok && pluginIsContextMode(s) {
			out := make([]any, 0, len(plugins)+1)
			out = append(out, plugins[:i]...)
			out = append(out, entry)
			out = append(out, plugins[i:]...)
			return out
		}
	}
	return append(plugins, entry)
}

// unregisterPonytailOpencode removes the ponytail plugin entry from opencode.json.
func unregisterPonytailOpencode() {
	op := util.OpenCodePathsResolved()
	if raw, ok := util.ReadFileSafe(op.Config); ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			if pv, ok := cfg.Get("plugin"); ok {
				if arr, ok := pv.([]any); ok {
					var kept []any
					for _, p := range arr {
						if s, ok := p.(string); ok && strings.EqualFold(s, ponytailOpencodePkg) {
							continue
						}
						kept = append(kept, p)
					}
					cfg.Set("plugin", kept)
					_ = util.WriteFile(op.Config, util.StringifyJSON(cfg))
				}
			}
		}
	}
}

func writePonytailAgentsMd(ocDir string) {
	writeOwnerAtPath(filepath.Join(ocDir, "AGENTS.md"), "ponytail")
}
func removePonytailAgentsMd(ocDir string) {
	removeOwnerAtPath(filepath.Join(ocDir, "AGENTS.md"), "ponytail")
}

func removePonytailModeState() {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		_ = os.RemoveAll(filepath.Join(xdg, "ponytail"))
	}
	_ = os.RemoveAll(filepath.Join(util.Home(), ".config", "ponytail"))
	if appData := os.Getenv("APPDATA"); appData != "" {
		_ = os.RemoveAll(filepath.Join(appData, "ponytail"))
	}
}

func ponytailOpencodeInstalled() bool {
	op := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(op.Config)
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
				if s, ok := p.(string); ok && strings.EqualFold(s, ponytailOpencodePkg) {
					return true
				}
			}
		}
	}
	return false
}

func ponytailOpencodeFilesPresent() bool {
	if util.Exists(filepath.Join(util.OpenCodePathsResolved().Dir, "plugins", "ponytail", "plugin.mjs")) ||
		util.Exists(filepath.Join(util.OpenCodePathsResolved().Dir, "plugins", "ponytail", "plugin.js")) {
		return true
	}
	if util.Which("npm") == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := util.Run(util.ResolveNpmBinary(), []string{"root", "-g"}, util.RunOptions{Capture: true, Ctx: ctx})
	if root.Code != 0 {
		return false
	}
	return util.Exists(filepath.Join(strings.TrimSpace(root.Stdout), ponytailOpencodePkg, "package.json"))
}

func claudePonytailInstalled() bool {
	home := util.Home()
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		home = dir
	} else {
		home = filepath.Join(home, ".claude")
	}
	if util.Exists(filepath.Join(home, "plugins", "marketplaces", "ponytail")) {
		return true
	}
	if raw, ok := util.ReadFileSafe(filepath.Join(home, "settings.json")); ok {
		if strings.Contains(strings.ToLower(raw), "ponytail") {
			return true
		}
	}
	if raw, ok := util.ReadFileSafe(filepath.Join(home, ".ponytail-active")); ok && strings.TrimSpace(raw) != "" {
		return true
	}
	return false
}

func codexPonytailInstalled() bool {
	root := util.Home()
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		root = dir
	} else {
		root = filepath.Join(root, ".codex")
	}
	if HasOwner("codex", "ponytail") {
		return true
	}
	if cwd, err := os.Getwd(); err == nil && util.Exists(filepath.Join(cwd, ".agents", "skills", "ponytail")) {
		return true
	}
	return util.Exists(filepath.Join(util.Home(), ".agents", "skills", "ponytail")) ||
		util.Exists(filepath.Join(root, "skills", "ponytail"))
}

func antigravityPonytailInstalled() bool {
	if HasOwner("antigravity", "ponytail") {
		return true
	}
	return util.Exists(filepath.Join(util.Home(), ".gemini", "config", "skills", "ponytail")) ||
		util.Exists(filepath.Join(util.Home(), ".gemini", "antigravity", "skills", "ponytail"))
}

func copilotPonytailInstalled() bool {
	return util.Exists(filepath.Join(util.CopilotPathsResolved().SkillsDir, "ponytail")) ||
		util.Exists(filepath.Join(util.Home(), ".agents", "skills", "ponytail"))
}

func claudePluginListHasPonytail() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := util.Run("claude", []string{"plugin", "list"}, util.RunOptions{Capture: true, Ctx: ctx})
	return r.Code == 0 && strings.Contains(strings.ToLower(r.Stdout), "ponytail")
}

// ponytailWireClaude installs the Claude plugin. AGENTS.md block is the floor.
func ponytailWireClaude(opts core.RunOpts) (bool, error) {
	if !opts.Upgrade && claudePonytailInstalled() {
		WriteOwner("claude", "ponytail")
		return true, nil
	}
	if !opts.DryRun && !isTest() && util.Which("claude") == "" {
		util.L.Err("ponytail needs the claude CLI (`claude plugin …`); Claude Desktop alone is not enough — install the CLI and re-run")
		return false, nil
	}
	ran, err := ponytailExec("claude",
		[]string{"plugin", "marketplace", "add", ponytailRepo},
		opts, "claude plugin marketplace add "+ponytailRepo+" && claude plugin install ponytail@ponytail")
	if ran && err == nil && !opts.DryRun && !isTest() {
		ran, err = ponytailExec("claude",
			[]string{"plugin", "install", "ponytail@ponytail"}, opts, "")
	}
	if !opts.DryRun && !isTest() {
		stampPonytailVersion()
	}
	WriteOwner("claude", "ponytail")
	return ran, err
}

func ponytailUnwireClaude(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would run: claude plugin uninstall ponytail@ponytail")
		RemoveOwner("claude", "ponytail")
		return true, nil
	}
	if claudePluginListHasPonytail() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if r := util.Run("claude", []string{"plugin", "uninstall", "ponytail@ponytail"}, util.RunOptions{Capture: true, Ctx: ctx}); r.Code != 0 {
			util.L.Err("claude plugin uninstall failed: " + clip(r.Stderr))
			cancel()
			return false, nil
		}
		cancel()
	}
	RemoveOwner("claude", "ponytail")
	return true, nil
}

// ponytailWireOpencode installs the upstream npm plugin entry. OpenCode resolves
// it from global node_modules. Test mode only writes the config entry.
func ponytailWireOpencode(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would run: npm i -g " + ponytailOpencodePkg + " && add plugin entry")
		registerPonytailOpencode()
		WriteOwner("opencode", "ponytail")
		return ponytailOpencodeInstalled(), nil
	}
	if isTest() {
		registerPonytailOpencode()
		WriteOwner("opencode", "ponytail")
		return ponytailOpencodeInstalled(), nil
	}
	if !opts.Upgrade && ponytailOpencodeInstalled() && ponytailOpencodeFilesPresent() {
		WriteOwner("opencode", "ponytail")
		return true, nil
	}
	if util.Which("npm") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		r := util.Run("npm", []string{"install", "-g", ponytailOpencodePkg}, util.RunOptions{Capture: true, Ctx: ctx})
		if r.Code != 0 {
			util.L.Err("ponytail npm install failed: " + clip(r.Stderr))
			return false, nil
		}
		util.EnsureNpmGlobalBinOnPath()
	} else {
		util.L.Err("ponytail needs npm for OpenCode plugin install")
		return false, nil
	}
	registerPonytailOpencode()
	stampPonytailVersion()
	WriteOwner("opencode", "ponytail")
	return ponytailOpencodeInstalled() && ponytailOpencodeFilesPresent(), nil
}

func ponytailUnwireOpencode(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would remove opencode ponytail plugin entry")
		unregisterPonytailOpencode()
		RemoveOwner("opencode", "ponytail")
		return true, nil
	}
	unregisterPonytailOpencode()
	dir := util.OpenCodePathsResolved().Dir
	_ = os.RemoveAll(filepath.Join(dir, "plugins", "ponytail"))
	_ = os.Remove(filepath.Join(dir, ".ponytail-active"))
	removePonytailModeState()
	RemoveOwner("opencode", "ponytail")
	return true, nil
}

func codexSkillsDirLocal() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return filepath.Join(dir, "skills")
	}
	return filepath.Join(util.Home(), ".codex", "skills")
}

func removePonytailCodexSkillCopies() {
	_ = os.RemoveAll(filepath.Join(util.Home(), ".agents", "skills", "ponytail"))
	_ = os.RemoveAll(filepath.Join(codexSkillsDirLocal(), "ponytail"))
}

func ponytailSkillsAddArgs(agent string) []string {
	return []string{"-y", "skills", "add", "DietrichGebert/ponytail", "-a", skillsAgentID(agent), "-s", "*", "--yes", "-g"}
}

func ponytailSkillsRemoveArgs(agent string) []string {
	return []string{"-y", "skills", "remove", "ponytail", "-a", skillsAgentID(agent), "-y", "-g"}
}

// ponytailWireCodex adds the marketplace when possible and writes the baseline.
// Final plugin install/trust is interactive in Codex (/plugins + /hooks).
func ponytailWireCodex(opts core.RunOpts) (bool, error) {
	if !opts.Upgrade && codexPonytailInstalled() {
		WriteOwner("codex", "ponytail")
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would run: codex plugin marketplace add " + ponytailRepo + "; then write AGENTS.md baseline; finish in /plugins + /hooks")
		WriteOwner("codex", "ponytail")
		return true, nil
	}
	if !isTest() && util.Which("codex") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		_ = util.Run("codex", []string{"plugin", "marketplace", "add", ponytailRepo}, util.RunOptions{Capture: true, Ctx: ctx})
		cancel()
		stampPonytailVersion()
	}
	WriteOwner("codex", "ponytail")
	return codexPonytailInstalled(), nil
}

func ponytailUnwireCodex(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would remove codex AGENTS.md Ponytail baseline")
		RemoveOwner("codex", "ponytail")
		return true, nil
	}
	removePonytailCodexSkillCopies()
	RemoveOwner("codex", "ponytail")
	return true, nil
}

// ponytailWireAntigravity installs the Antigravity extension.
func ponytailWireAntigravity(opts core.RunOpts) (bool, error) {
	if !opts.Upgrade && antigravityPonytailInstalled() {
		WriteOwner("antigravity", "ponytail")
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would run: agy plugin install https://github.com/" + ponytailRepo)
		WriteOwner("antigravity", "ponytail")
		return true, nil
	}
	if isTest() {
		WriteOwner("antigravity", "ponytail")
		return true, nil
	}
	ran := true
	if util.Which("agy") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if r := util.Run("agy", []string{"plugin", "install", "https://github.com/" + ponytailRepo}, util.RunOptions{Capture: true, Ctx: ctx}); r.Code != 0 {
			ran = false
		}
		cancel()
	} else {
		ran = false
	}
	if !ran && util.Which("gemini") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if r := util.Run("gemini", []string{"extensions", "install", "https://github.com/" + ponytailRepo}, util.RunOptions{Capture: true, Ctx: ctx}); r.Code == 0 {
			ran = true
		}
		cancel()
	}
	stampPonytailVersion()
	WriteOwner("antigravity", "ponytail")
	if !ran {
		return false, nil
	}
	return antigravityPonytailInstalled(), nil
}

func ponytailUnwireAntigravity(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would remove antigravity ponytail skills and GEMINI.md block")
		RemoveOwner("antigravity", "ponytail")
		return true, nil
	}
	if util.Which("agy") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		_ = util.Run("agy", []string{"plugin", "uninstall", "https://github.com/" + ponytailRepo}, util.RunOptions{Capture: true, Ctx: ctx})
		cancel()
	}
	if util.Which("gemini") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		_ = util.Run("gemini", []string{"extensions", "uninstall", "ponytail"}, util.RunOptions{Capture: true, Ctx: ctx})
		cancel()
	}
	_ = os.RemoveAll(filepath.Join(util.Home(), ".gemini", "config", "skills", "ponytail"))
	_ = os.RemoveAll(filepath.Join(util.Home(), ".gemini", "antigravity", "skills", "ponytail"))
	removePonytailModeState()
	RemoveOwner("antigravity", "ponytail")
	return true, nil
}

var ponytail = &core.ToolManifest{
	ID:           "ponytail",
	Label:        "Ponytail",
	Description:  "Lazy senior-dev mode: smallest diff that works, after tracing the real flow. Brings /ponytail commands + skills.",
	Homepage:     "https://github.com/DietrichGebert/ponytail",
	InstallHint:  "Installed per-agent from " + ponytailRepo + ".",
	Channel:      core.ChannelGitHub,
	NotTrackable: true,
	Install: func(opts core.RunOpts) (bool, error) {
		opts.Reportf("installed per agent", 1)
		return true, nil
	},
	WireFor: map[string]core.AgentFn{
		"claude":      ponytailWireClaude,
		"opencode":    ponytailWireOpencode,
		"codex":       ponytailWireCodex,
		"antigravity": ponytailWireAntigravity,
		"copilot": func(opts core.RunOpts) (bool, error) {
			if !opts.Upgrade && copilotPonytailInstalled() {
				WriteOwner("copilot", "ponytail")
				agents.SyncCopilotIdeInstructions()
				return true, nil
			}
			bin, args := resolveSkillsBin(ponytailSkillsAddArgs("copilot"))
			_ = util.Run(bin, args, util.RunOptions{Capture: true})
			WriteOwner("copilot", "ponytail")
			agents.SyncCopilotIdeInstructions()
			return copilotPonytailInstalled(), nil
		},
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":      ponytailUnwireClaude,
		"opencode":    ponytailUnwireOpencode,
		"codex":       ponytailUnwireCodex,
		"antigravity": ponytailUnwireAntigravity,
		"copilot": func(opts core.RunOpts) (bool, error) {
			_ = os.RemoveAll(filepath.Join(util.CopilotPathsResolved().SkillsDir, "ponytail"))
			removePonytailModeState()
			RemoveOwner("copilot", "ponytail")
			return true, nil
		},
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude": func() *bool {
			if !isTest() && claudePluginListHasPonytail() {
				return core.BoolPtr(true)
			}
			return core.BoolPtr(claudePonytailInstalled())
		},
		"opencode": func() *bool {
			if isTest() {
				return core.BoolPtr(ponytailOpencodeInstalled())
			}
			return core.BoolPtr(ponytailOpencodeInstalled() && ponytailOpencodeFilesPresent())
		},
		"codex":       func() *bool { return core.BoolPtr(codexPonytailInstalled()) },
		"antigravity": func() *bool { return core.BoolPtr(antigravityPonytailInstalled()) },
		"copilot":     func() *bool { return core.BoolPtr(copilotPonytailInstalled()) },
	},
}
