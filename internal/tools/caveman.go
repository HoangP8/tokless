package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func cavemanExec(bin string, args []string, opts core.RunOpts, dryHint string, env ...string) (bool, error) {
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
		util.L.Err("caveman command failed: " + clip(r.Stderr))
		return false, nil
	}
	return true, nil
}

func cavemanOpencodeInstallEnv() []string {
	dir := util.OpenCodePathsResolved().Dir
	if filepath.Base(dir) != "opencode" {
		return nil
	}
	return []string{"XDG_CONFIG_HOME=" + filepath.Dir(dir)}
}

// resolveSkillsBin returns ("skills", args[1:]) if skills binary is available,
// otherwise ("npx", original npx args).
func resolveSkillsBin(npxArgs []string) (string, []string) {
	util.EnsureNpmGlobalBinOnPath()
	if util.Which("skills") != "" {
		return "skills", npxArgs[2:] // strip "-y skills" prefix
	}
	return "npx", npxArgs
}

// resolveCavemanBin returns ("caveman", cavemanArgs) if global caveman binary
// is available, else ("npx", npxArgs).
func resolveCavemanBin(agent string, upgrade bool) (string, []string) {
	util.EnsureNpmGlobalBinOnPath()
	if util.Which("caveman") != "" {
		args := []string{"--only", agent, "--no-mcp-shrink"}
		if upgrade {
			args = append(args, "--force")
		}
		return "caveman", args
	}
	args := []string{"-y", "github:JuliusBrussee/caveman", "--", "--only", agent, "--no-mcp-shrink"}
	if upgrade {
		args = append(args, "--force")
	}
	return "npx", args
}

func ensureOpencodeCommandsDir() {
	_ = os.MkdirAll(filepath.Join(util.OpenCodePathsResolved().Dir, "commands"), 0o755)
}

// skillsAgentID maps tokless agent IDs to the upstream `skills` CLI's
// supported agent names.
func skillsAgentID(agent string) string {
	switch agent {
	case "copilot":
		return "github-copilot"
	}
	return agent
}

func cavemanSkillsAddArgs(agent string) []string {
	return []string{"-y", "skills", "add", "JuliusBrussee/caveman", "-a", skillsAgentID(agent), "-s", "*", "--yes", "-g"}
}

func cavemanSkillsRemoveArgs(agent string) []string {
	args := append([]string{"-y", "skills", "remove"}, cavemanSkillNames...)
	return append(args, "-a", skillsAgentID(agent), "-y", "-g")
}

func relocateCavemanSkills(dstDir string) {
	src := filepath.Join(util.Home(), ".agents", "skills")
	for _, name := range cavemanSkillNames {
		s := filepath.Join(src, name)
		if !util.Exists(s) {
			continue
		}
		if util.CopyDirMerge(s, filepath.Join(dstDir, name)) == nil {
			_ = os.RemoveAll(s)
		}
	}
}

func removeCavemanSkillCopies(dir string) {
	for _, name := range cavemanSkillNames {
		_ = os.RemoveAll(filepath.Join(dir, name))
	}
}

func codexSkillsDir() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return filepath.Join(dir, "skills")
	}
	return filepath.Join(util.Home(), ".codex", "skills")
}

func stampCavemanVersion() {
	if v := util.LatestVersionFor("caveman"); v != nil {
		util.StampCavemanVersion(*v)
	}
}

const cavemanOpencodePluginRel = "./plugins/caveman/plugin.js"

func registerCavemanOpencode() {
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
	has := false
	for _, p := range plugins {
		if s, ok := p.(string); ok && strings.Contains(strings.ToLower(s), "caveman") {
			has = true
			break
		}
	}
	if mv, ok := cfg.Get("mcp"); ok {
		if mm, ok := mv.(*util.OrderedMap); ok {
			if _, has := mm.Get("caveman-shrink"); has {
				mm.Delete("caveman-shrink")
				if mm.Len() == 0 {
					cfg.Delete("mcp")
				}
			}
		}
	}

	if !has {
		plugins = append(plugins, cavemanOpencodePluginRel)
	}
	cfg.Set("plugin", plugins)

	_ = util.WriteFile(op.Config, util.StringifyJSON(cfg))
	writeCavemanAgentsMd(op.Dir)
}

// unregisterCavemanOpencode removes caveman's plugin entry.
func unregisterCavemanOpencode() {
	op := util.OpenCodePathsResolved()
	if raw, ok := util.ReadFileSafe(op.Config); ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			if pv, ok := cfg.Get("plugin"); ok {
				if arr, ok := pv.([]any); ok {
					kept := make([]any, 0, len(arr))
					for _, p := range arr {
						if s, ok := p.(string); ok && s == cavemanOpencodePluginRel {
							continue
						}
						kept = append(kept, p)
					}
					cfg.Set("plugin", kept)
				}
			}
			if mv, ok := cfg.Get("mcp"); ok {
				if mm, ok := mv.(*util.OrderedMap); ok {
					mm.Delete("caveman-shrink")
					if mm.Len() == 0 {
						cfg.Delete("mcp")
					}
				}
			}
			_ = util.WriteFile(op.Config, util.StringifyJSON(cfg))
		}
	}
	removeCavemanRuleset(filepath.Join(op.Dir, "AGENTS.md"))
}

// writeCavemanAgentsMd appends the caveman section to the unified body.
func writeCavemanAgentsMd(ocDir string) {
	writeOwnerAtPath(filepath.Join(ocDir, "AGENTS.md"), "caveman")
}

func writeCavemanRuleset(p string) { writeOwnerAtPath(p, "caveman") }

// removeCavemanRuleset removes the caveman section from the unified body.
func removeCavemanRuleset(p string) { removeOwnerAtPath(p, "caveman") }

func claudeCavemanMemory() string {
	home := util.Home()
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "CLAUDE.md")
	}
	return filepath.Join(home, ".claude", "CLAUDE.md")
}

func codexCavemanMemory() string {
	root := util.Home()
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		root = dir
	} else {
		root = filepath.Join(root, ".codex")
	}
	return filepath.Join(root, "AGENTS.md")
}

func cavemanRulesetActive(p string) bool {
	if raw, ok := util.ReadFileSafe(p); ok {
		return strings.Contains(raw, util.SectionsByOwner["caveman"])
	}
	return false
}

func opencodePluginFilesPresent() bool {
	return util.Exists(filepath.Join(util.OpenCodePathsResolved().Dir, "plugins", "caveman", "plugin.js"))
}

func opencodePluginInstalled() bool {
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
				if s, ok := p.(string); ok && strings.Contains(strings.ToLower(s), "caveman") {
					return true
				}
			}
		}
	}
	return false
}

func claudeCavemanInstalled() bool {
	home := util.Home()
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		home = dir
	} else {
		home = filepath.Join(home, ".claude")
	}
	if util.Exists(filepath.Join(home, ".caveman-active")) {
		return true
	}
	if raw, ok := util.ReadFileSafe(filepath.Join(home, "settings.json")); ok {
		if strings.Contains(strings.ToLower(raw), "caveman") {
			return true
		}
	}
	return false
}

func codexCavemanInstalled() bool {
	root := util.Home()
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		root = dir
	} else {
		root = filepath.Join(root, ".codex")
	}
	if cwd, err := os.Getwd(); err == nil && util.Exists(filepath.Join(cwd, ".agents", "skills", "caveman")) {
		return true
	}
	return util.Exists(filepath.Join(util.Home(), ".agents", "skills", "caveman")) ||
		util.Exists(filepath.Join(root, "skills", "caveman"))
}

// antigravityCavemanInstalled checks the global skills roots antigravity reads.
func antigravityCavemanInstalled() bool {
	return util.Exists(filepath.Join(util.Home(), ".gemini", "config", "skills", "caveman")) ||
		util.Exists(filepath.Join(util.Home(), ".gemini", "antigravity", "skills", "caveman"))
}

func copilotCavemanInstalled() bool {
	return util.Exists(filepath.Join(util.CopilotPathsResolved().SkillsDir, "caveman")) ||
		util.Exists(filepath.Join(util.Home(), ".agents", "skills", "caveman"))
}

func geminiCavemanMd() string { return filepath.Join(util.Home(), ".gemini", "GEMINI.md") }

func writeCavemanGeminiMd()  { writeCavemanRuleset(geminiCavemanMd()) }
func removeCavemanGeminiMd() { removeCavemanRuleset(geminiCavemanMd()) }

var cavemanSkillNames = []string{
	"caveman", "caveman-commit", "caveman-compress", "caveman-help",
	"caveman-review", "caveman-stats", "cavecrew",
}

// Upstream's opencode file manifests.
var cavemanOpencodeCommandFiles = []string{
	"caveman.md", "caveman-commit.md", "caveman-review.md",
	"caveman-compress.md", "caveman-stats.md", "caveman-help.md",
}

var cavemanOpencodeAgentFiles = []string{
	"cavecrew-investigator.md", "cavecrew-builder.md", "cavecrew-reviewer.md",
}

func removeCavemanOpencodeAgentFiles() {
	dir := util.OpenCodePathsResolved().Dir
	for _, f := range cavemanOpencodeAgentFiles {
		_ = os.Remove(filepath.Join(dir, "agents", f))
	}
}

func removeCavemanOpencodeFiles() {
	dir := util.OpenCodePathsResolved().Dir
	_ = os.RemoveAll(filepath.Join(dir, "plugins", "caveman"))
	for _, f := range cavemanOpencodeCommandFiles {
		_ = os.Remove(filepath.Join(dir, "commands", f))
	}
	removeCavemanOpencodeAgentFiles()
	for _, n := range cavemanSkillNames {
		_ = os.RemoveAll(filepath.Join(dir, "skills", n))
	}
	_ = os.Remove(filepath.Join(dir, ".caveman-active"))
}

func claudePluginListHasCaveman() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := util.Run("claude", []string{"plugin", "list"}, util.RunOptions{Capture: true, Ctx: ctx})
	return r.Code == 0 && strings.Contains(strings.ToLower(r.Stdout), "caveman")
}

var caveman = &core.ToolManifest{
	ID:           "caveman",
	Label:        "Caveman",
	Description:  "Skill that compresses agent prompts using primitive English.",
	Homepage:     "https://github.com/JuliusBrussee/caveman",
	InstallHint:  "Installed per-agent by Caveman's own CLI.",
	NeedsNode:    true,
	NeedsGit:     true,
	Channel:      core.ChannelGitHub,
	NotTrackable: true,
	Install: func(opts core.RunOpts) (bool, error) {
		if !opts.DryRun && !isTest() && !opts.Upgrade && util.Which("caveman") != "" {
			opts.Reportf("already installed", 1)
			return true, nil
		}
		opts.Reportf("installing from GitHub", 0.3)
		if !opts.DryRun && !isTest() {
			if util.Which("npm") == "" {
				util.L.Err("caveman needs npm for global install")
				return false, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			r := util.Run("npm", []string{"install", "-g", "github:JuliusBrussee/caveman"},
				util.RunOptions{Capture: true, Ctx: ctx})
			cancel()
			util.EnsureNpmGlobalBinOnPath()
			if r.Code != 0 && util.Which("caveman") == "" {
				util.L.Err("caveman npm install failed: " + clip(r.Stderr))
				return false, nil
			}
		}
		opts.Reportf("ready", 1)
		return true, nil
	},
	WireFor: map[string]core.AgentFn{
		"claude": func(opts core.RunOpts) (bool, error) {
			if !opts.DryRun && !isTest() && util.Which("claude") == "" {
				util.L.Err("caveman needs the claude CLI (`claude plugin …`); Claude Desktop alone is not enough — install the CLI and re-run")
				return false, nil
			}
			ran, err := cavemanExec("claude",
				[]string{"plugin", "marketplace", "add", "JuliusBrussee/caveman"},
				opts, "claude plugin marketplace add JuliusBrussee/caveman && claude plugin install caveman@caveman")
			if ran && err == nil && !opts.DryRun && !isTest() {
				ran, err = cavemanExec("claude",
					[]string{"plugin", "install", "caveman@caveman"}, opts, "")
			}
			if !opts.DryRun && !isTest() {
				stampCavemanVersion()
			}
			WriteOwner("claude", "caveman")
			return ran, err
		},
		"opencode": func(opts core.RunOpts) (bool, error) {
			if !opts.Upgrade && opencodePluginInstalled() && opencodePluginFilesPresent() {
				if !opts.DryRun {
					removeCavemanOpencodeAgentFiles()
				}
				WriteOwner("opencode", "caveman")
				return true, nil
			}
			if !opts.DryRun && !isTest() {
				ensureOpencodeCommandsDir()
			}
			bin, args := resolveCavemanBin("opencode", opts.Upgrade)
			ran, err := cavemanExec(bin, args, opts, bin+" "+strings.Join(args, " "),
				cavemanOpencodeInstallEnv()...)
			WriteOwner("opencode", "caveman")
			if opts.DryRun || isTest() {
				return ran, err
			}
			if opencodePluginFilesPresent() {
				registerCavemanOpencode()
				removeCavemanOpencodeAgentFiles()
				stampCavemanVersion()
			}
			return opencodePluginInstalled(), err
		},
		"codex": func(opts core.RunOpts) (bool, error) {
			if !opts.Upgrade && codexCavemanInstalled() {
				WriteOwner("codex", "caveman")
				return true, nil
			}
			bin, args := resolveSkillsBin(cavemanSkillsAddArgs("codex"))
			ran, err := cavemanExec(bin, args, opts, bin+" "+strings.Join(args, " "))
			WriteOwner("codex", "caveman")
			if opts.DryRun || isTest() {
				return ran, err
			}
			relocateCavemanSkills(codexSkillsDir())
			stampCavemanVersion()
			return codexCavemanInstalled(), err
		},
		"antigravity": func(opts core.RunOpts) (bool, error) {
			if !opts.Upgrade && antigravityCavemanInstalled() {
				WriteOwner("antigravity", "caveman")
				return true, nil
			}
			bin, args := resolveSkillsBin(cavemanSkillsAddArgs("antigravity"))
			ran, err := cavemanExec(bin, args, opts, bin+" "+strings.Join(args, " "))
			WriteOwner("antigravity", "caveman")
			if opts.DryRun || isTest() {
				return ran, err
			}
			relocateCavemanSkills(util.AntigravityPathsResolved().SkillsDir)
			stampCavemanVersion()
			return antigravityCavemanInstalled(), err
		},
		"copilot": func(opts core.RunOpts) (bool, error) {
			if !opts.Upgrade && copilotCavemanInstalled() {
				WriteOwner("copilot", "caveman")
				return true, nil
			}
			bin, args := resolveSkillsBin(cavemanSkillsAddArgs("copilot"))
			ran, err := cavemanExec(bin, args, opts, bin+" "+strings.Join(args, " "))
			WriteOwner("copilot", "caveman")
			if opts.DryRun || isTest() {
				return ran, err
			}
			relocateCavemanSkills(util.CopilotPathsResolved().SkillsDir)
			stampCavemanVersion()
			return copilotCavemanInstalled(), err
		},
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude": func() *bool {
			if !isTest() && claudePluginListHasCaveman() {
				return core.BoolPtr(true)
			}
			return core.BoolPtr(claudeCavemanInstalled())
		},
		"opencode":    func() *bool { return core.BoolPtr(opencodePluginInstalled() && opencodePluginFilesPresent()) },
		"codex":       func() *bool { return core.BoolPtr(codexCavemanInstalled()) },
		"antigravity": func() *bool { return core.BoolPtr(antigravityCavemanInstalled()) },
		"copilot":     func() *bool { return core.BoolPtr(copilotCavemanInstalled()) },
	},

	UnwireFor: map[string]core.AgentFn{
		"claude": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would run: claude plugin uninstall caveman@caveman && claude mcp remove caveman-shrink")
				return true, nil
			}
			if !isTest() {
				if claudePluginListHasCaveman() {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					if r := util.Run("claude", []string{"plugin", "uninstall", "caveman@caveman"}, util.RunOptions{Capture: true, Ctx: ctx}); r.Code != 0 {
						util.L.Err("claude plugin uninstall failed: " + clip(r.Stderr))
						cancel()
						return false, nil
					}
					cancel()
				}
				ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
				_ = util.Run("claude", []string{"mcp", "remove", "caveman-shrink"}, util.RunOptions{Capture: true, Ctx: ctx2})
				cancel2()
			}
			RemoveOwner("claude", "caveman")
			return true, nil
		},
		"opencode": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would remove opencode caveman plugin dir, commands, agents, skills, config entries, AGENTS.md block")
				return true, nil
			}
			unregisterCavemanOpencode()
			removeCavemanOpencodeFiles()
			RemoveOwner("opencode", "caveman")
			return true, nil
		},
		"codex": func(opts core.RunOpts) (bool, error) {
			bin, args := resolveSkillsBin(cavemanSkillsRemoveArgs("codex"))
			ran, err := cavemanExec(bin, args, opts, bin+" "+strings.Join(args, " "))
			RemoveOwner("codex", "caveman")
			if !opts.DryRun && !isTest() {
				removeCavemanSkillCopies(codexSkillsDir())
			}
			return ran, err
		},
		"antigravity": func(opts core.RunOpts) (bool, error) {
			bin, args := resolveSkillsBin(cavemanSkillsRemoveArgs("antigravity"))
			ran, err := cavemanExec(bin, args, opts, bin+" "+strings.Join(args, " "))
			RemoveOwner("antigravity", "caveman")
			if !opts.DryRun && !isTest() {
				removeCavemanSkillCopies(util.AntigravityPathsResolved().SkillsDir)
			}
			return ran, err
		},
		"copilot": func(opts core.RunOpts) (bool, error) {
			bin, args := resolveSkillsBin(cavemanSkillsRemoveArgs("copilot"))
			ran, err := cavemanExec(bin, args, opts, bin+" "+strings.Join(args, " "))
			RemoveOwner("copilot", "caveman")
			if !opts.DryRun && !isTest() {
				removeCavemanSkillCopies(util.CopilotPathsResolved().SkillsDir)
			}
			return ran, err
		},
	},
}
