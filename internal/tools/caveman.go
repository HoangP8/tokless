package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

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
	r := util.Run(bin, args, util.RunOptions{Capture: true, Env: env})
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

func ensureOpencodeCommandsDir() {
	_ = os.MkdirAll(filepath.Join(util.OpenCodePathsResolved().Dir, "commands"), 0o755)
}

func cavemanSkillsAddArgs(agent string) []string {
	return []string{"-y", "skills", "add", "JuliusBrussee/caveman", "-a", agent, "-s", "*", "--yes", "-g"}
}

func cavemanSkillsRemoveArgs(agent string) []string {
	args := append([]string{"-y", "skills", "remove"}, cavemanSkillNames...)
	return append(args, "-a", agent, "-y", "-g")
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

const (
	cavemanAgentsBegin = "<!-- caveman-begin -->"
	cavemanAgentsEnd   = "<!-- caveman-end -->"
	cavemanRuleBody    = `Respond terse like smart caveman. All technical substance stay. Only fluff die.

Rules:
- Drop: articles (a/an/the), filler (just/really/basically), pleasantries, hedging
- Fragments OK. Short synonyms. Technical terms exact. Code unchanged.
- Pattern: [thing] [action] [reason]. [next step].
- Not: "Sure! I'd be happy to help you with that."
- Yes: "Bug in auth middleware. Fix:"

Switch level: /caveman lite|full|ultra|wenyan
Stop: "stop caveman" or "normal mode"

Auto-Clarity: drop caveman for security warnings, irreversible actions, user confused. Resume after.

Boundaries: code/commits/PRs written normal.
`
)

// writeCavemanAgentsMd appends caveman's fenced ruleset to opencode's AGENTS.md.
func writeCavemanAgentsMd(ocDir string) {
	writeCavemanRuleset(filepath.Join(ocDir, "AGENTS.md"))
}

func writeCavemanRuleset(p string) {
	_ = util.EnsureDir(filepath.Dir(p))
	fenced := cavemanAgentsBegin + "\n" + cavemanRuleBody + cavemanAgentsEnd + "\n"
	existing, ok := util.ReadFileSafe(p)
	if !ok {
		_ = util.WriteFile(p, fenced)
		return
	}
	if strings.Contains(existing, cavemanAgentsBegin) && strings.Contains(existing, cavemanAgentsEnd) {
		return
	}
	sep := "\n\n"
	if strings.HasSuffix(existing, "\n\n") {
		sep = ""
	} else if strings.HasSuffix(existing, "\n") {
		sep = "\n"
	}
	_ = util.WriteFile(p, existing+sep+fenced)
}

// removeCavemanRuleset strips the fenced block from a global instructions file,
// preserving everything else. Removes the file if it becomes empty.
func removeCavemanRuleset(p string) {
	existing, ok := util.ReadFileSafe(p)
	if !ok {
		return
	}
	bi := strings.Index(existing, cavemanAgentsBegin)
	ei := strings.Index(existing, cavemanAgentsEnd)
	if bi < 0 || ei < 0 || ei < bi {
		return
	}
	ei += len(cavemanAgentsEnd)
	for ei < len(existing) && existing[ei] == '\n' {
		ei++
	}
	next := strings.TrimRight(existing[:bi], "\n")
	tail := existing[ei:]
	if next != "" && tail != "" {
		next += "\n\n"
	}
	next += tail
	if strings.TrimSpace(next) == "" {
		_ = os.Remove(p)
		return
	}
	_ = util.WriteFile(p, next)
}

// caveman global instructions file per agent (always loaded every session).
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
		return strings.Contains(raw, cavemanAgentsBegin)
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

func geminiCavemanMd() string { return filepath.Join(util.Home(), ".gemini", "GEMINI.md") }

func writeCavemanGeminiMd() { writeCavemanRuleset(geminiCavemanMd()) }
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

func removeCavemanOpencodeFiles() {
	dir := util.OpenCodePathsResolved().Dir
	_ = os.RemoveAll(filepath.Join(dir, "plugins", "caveman"))
	for _, f := range cavemanOpencodeCommandFiles {
		_ = os.Remove(filepath.Join(dir, "commands", f))
	}
	for _, f := range cavemanOpencodeAgentFiles {
		_ = os.Remove(filepath.Join(dir, "agents", f))
	}
	for _, n := range cavemanSkillNames {
		_ = os.RemoveAll(filepath.Join(dir, "skills", n))
	}
	_ = os.Remove(filepath.Join(dir, ".caveman-active"))
}

func claudePluginListHasCaveman() bool {
	r := util.Run("claude", []string{"plugin", "list"}, util.RunOptions{Capture: true})
	return r.Code == 0 && strings.Contains(strings.ToLower(r.Stdout), "caveman")
}

var caveman = &core.ToolManifest{
	ID:           "caveman",
	Label:        "Caveman",
	Description:  "Skill that compresses agent prompts using primitive English.",
	Homepage:     "https://github.com/JuliusBrussee/caveman",
	InstallHint:  "Installed per-agent by Caveman's own CLI.",
	NeedsGit:     true,
	Channel:      core.ChannelGitHub,
	NotTrackable: true,
	Install: func(opts core.RunOpts) (bool, error) {
		opts.Reportf("installed per agent", 1)
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
			return ran, err
		},
		"opencode": func(opts core.RunOpts) (bool, error) {
			if !opts.DryRun && !isTest() {
				ensureOpencodeCommandsDir()
			}
			args := []string{"-y", "github:JuliusBrussee/caveman", "--", "--only", "opencode", "--no-mcp-shrink"}
			if opts.Upgrade {
				args = append(args, "--force")
			}
			ran, err := cavemanExec("npx", args, opts, "npx -y github:JuliusBrussee/caveman -- --only opencode --no-mcp-shrink"+func() string {
				if opts.Upgrade {
					return " --force"
				}
				return ""
			}(), cavemanOpencodeInstallEnv()...)
			if opts.DryRun || isTest() {
				return ran, err
			}
			if opencodePluginFilesPresent() {
				registerCavemanOpencode()

				op := util.OpenCodePathsResolved()
				pkgPath := filepath.Join(op.Dir, "plugins", "caveman", "package.json")
				if raw, ok := util.ReadFileSafe(pkgPath); ok {
					var pkg map[string]interface{}
					if json.Unmarshal([]byte(raw), &pkg) == nil {
						if latest := util.LatestVersionFor("caveman"); latest != nil {
							pkg["version"] = *latest
						}
						if pkg["name"] == nil || pkg["name"] == "" {
							pkg["name"] = "caveman-opencode-plugin"
						}
						if b, err := json.MarshalIndent(pkg, "", "  "); err == nil {
							_ = util.WriteFile(pkgPath, string(b))
						}
					}
				}
				stampCavemanVersion()
			}
			return opencodePluginInstalled(), err
		},
		"codex": func(opts core.RunOpts) (bool, error) {
			args := cavemanSkillsAddArgs("codex")
			ran, err := cavemanExec("npx", args, opts, "npx "+strings.Join(args, " "))
			if opts.DryRun || isTest() {
				return ran, err
			}
			relocateCavemanSkills(codexSkillsDir())
			stampCavemanVersion()
			return codexCavemanInstalled(), err
		},
		"antigravity": func(opts core.RunOpts) (bool, error) {
			args := cavemanSkillsAddArgs("antigravity")
			ran, err := cavemanExec("npx", args, opts, "npx "+strings.Join(args, " "))
			if opts.DryRun || isTest() {
				return ran, err
			}
			relocateCavemanSkills(util.AntigravityPathsResolved().SkillsDir)
			writeCavemanGeminiMd()
			stampCavemanVersion()
			return antigravityCavemanInstalled(), err
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
	},

	UnwireFor: map[string]core.AgentFn{
		"claude": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would run: claude plugin uninstall caveman@caveman && claude mcp remove caveman-shrink")
				return true, nil
			}
			if !isTest() {
				if claudePluginListHasCaveman() {
					if r := util.Run("claude", []string{"plugin", "uninstall", "caveman@caveman"}, util.RunOptions{Capture: true}); r.Code != 0 {
						util.L.Err("claude plugin uninstall failed: " + clip(r.Stderr))
						return false, nil
					}
				}
				_ = util.Run("claude", []string{"mcp", "remove", "caveman-shrink"}, util.RunOptions{Capture: true})
				removeCavemanRuleset(claudeCavemanMemory())
			}
			return true, nil
		},
		"opencode": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would remove opencode caveman plugin dir, commands, agents, skills, config entries, AGENTS.md block")
				return true, nil
			}
			unregisterCavemanOpencode()
			removeCavemanOpencodeFiles()
			return true, nil
		},
		"codex": func(opts core.RunOpts) (bool, error) {
			ran, err := cavemanExec("npx", cavemanSkillsRemoveArgs("codex"), opts,
				"npx -y skills remove <7 caveman skills> -a codex -y -g")
			if !opts.DryRun && !isTest() {
				removeCavemanSkillCopies(codexSkillsDir())
				removeCavemanRuleset(codexCavemanMemory())
			}
			return ran, err
		},
		"antigravity": func(opts core.RunOpts) (bool, error) {
			ran, err := cavemanExec("npx", cavemanSkillsRemoveArgs("antigravity"), opts,
				"npx -y skills remove <7 caveman skills> -a antigravity -y -g")
			if !opts.DryRun && !isTest() {
				removeCavemanSkillCopies(util.AntigravityPathsResolved().SkillsDir)
				removeCavemanGeminiMd()
			}
			return ran, err
		},
	},
}
