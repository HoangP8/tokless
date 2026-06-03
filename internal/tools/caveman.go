package tools

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func cavemanExec(bin string, args []string, opts core.RunOpts, dryHint string) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would run: " + dryHint)
		return true, nil
	}
	if isTest() {
		return true, nil
	}
	r := util.Run(bin, args, util.RunOptions{Capture: true})
	if r.Code != 0 {
		util.L.Err("caveman command failed: " + clip(r.Stderr))
		return false, nil
	}
	return true, nil
}

func ensureOpencodeCommandsDir() {
	_ = os.MkdirAll(filepath.Join(util.OpenCodePathsResolved().Dir, "commands"), 0o755)
}

const cavemanOpencodePluginRel = "./plugins/caveman/plugin.js"

func registerCavemanOpencode() {
	op := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(op.Config)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
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
						if s, ok := p.(string); ok && strings.Contains(strings.ToLower(s), "caveman") {
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
	return util.Exists(filepath.Join(root, "skills", "caveman"))
}

var caveman = &core.ToolManifest{
	ID:           "caveman",
	Label:        "Caveman",
	Description:  "Skill that compresses agent prompts using primitive English.",
	Homepage:     "https://github.com/JuliusBrussee/caveman",
	InstallHint:  "Installed per-agent by Caveman's own CLI.",
	Channel:      core.ChannelGitHub,
	NotTrackable: true,
	Install: func(opts core.RunOpts) (bool, error) {
		opts.Reportf("installed per agent", 1)
		return true, nil
	},
	WireFor: map[string]core.AgentFn{
		"claude": func(opts core.RunOpts) (bool, error) {
			ran, err := cavemanExec("sh",
				[]string{"-c", "claude plugin marketplace add JuliusBrussee/caveman && claude plugin install caveman@caveman"},
				opts, "claude plugin marketplace add JuliusBrussee/caveman && claude plugin install caveman@caveman")
			if !opts.DryRun && !isTest() {
				writeCavemanRuleset(claudeCavemanMemory())
			}
			return ran, err
		},
		"opencode": func(opts core.RunOpts) (bool, error) {
			if !opts.DryRun && !isTest() {
				ensureOpencodeCommandsDir()
			}
			ran, err := cavemanExec("npx",
				[]string{"-y", "github:JuliusBrussee/caveman", "--", "--only", "opencode"},
				opts, "npx -y github:JuliusBrussee/caveman -- --only opencode")
			if opts.DryRun || isTest() {
				return ran, err
			}
			if opencodePluginFilesPresent() {
				registerCavemanOpencode()
			}
			return opencodePluginInstalled(), err
		},
		"codex": func(opts core.RunOpts) (bool, error) {
			ran, err := cavemanExec("npx",
				[]string{"-y", "skills", "add", "JuliusBrussee/caveman", "-a", "codex", "-y"},
				opts, "npx -y skills add JuliusBrussee/caveman -a codex -y")
			if !opts.DryRun && !isTest() {
				writeCavemanRuleset(codexCavemanMemory())
			}
			return ran, err
		},
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude": func() *bool {
			return core.BoolPtr(claudeCavemanInstalled() || cavemanRulesetActive(claudeCavemanMemory()))
		},
		"opencode": func() *bool { return core.BoolPtr(opencodePluginInstalled()) },
		"codex": func() *bool {
			return core.BoolPtr(codexCavemanInstalled() || cavemanRulesetActive(codexCavemanMemory()))
		},
	},
	UnwireFor: map[string]core.AgentFn{
		"claude": func(opts core.RunOpts) (bool, error) {
			ran, err := cavemanExec("npx",
				[]string{"-y", "github:JuliusBrussee/caveman", "--", "--uninstall", "--only", "claude"},
				opts, "npx -y github:JuliusBrussee/caveman -- --uninstall --only claude")
			if !opts.DryRun && !isTest() {
				removeCavemanRuleset(claudeCavemanMemory())
			}
			return ran, err
		},
		"opencode": func(opts core.RunOpts) (bool, error) {
			ran := true
			var err error
			if !isTest() {
				ran, err = cavemanExec("npx",
					[]string{"-y", "github:JuliusBrussee/caveman", "--", "--uninstall", "--only", "opencode"},
					opts, "npx -y github:JuliusBrussee/caveman -- --uninstall --only opencode")
			}
			if !opts.DryRun {
				unregisterCavemanOpencode()
			}
			return ran, err
		},
		"codex": func(opts core.RunOpts) (bool, error) {
			ran, err := cavemanExec("npx", []string{"skills", "remove", "caveman"}, opts, "npx skills remove caveman")
			if !opts.DryRun && !isTest() {
				removeCavemanRuleset(codexCavemanMemory())
			}
			return ran, err
		},
	},
}
