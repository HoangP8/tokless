package tools

import (
	"os"
	"path/filepath"

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

func opencodePluginInstalled() bool {
	return util.Exists(filepath.Join(util.OpenCodePathsResolved().Dir, "plugins", "caveman", "plugin.js"))
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
			return cavemanExec("sh",
				[]string{"-c", "claude plugin marketplace add JuliusBrussee/caveman && claude plugin install caveman@caveman"},
				opts, "claude plugin marketplace add JuliusBrussee/caveman && claude plugin install caveman@caveman")
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
			return ran || opencodePluginInstalled(), err
		},
		"codex": func(opts core.RunOpts) (bool, error) {
			return cavemanExec("npx",
				[]string{"-y", "skills", "add", "JuliusBrussee/caveman", "-a", "codex", "-y"},
				opts, "npx -y skills add JuliusBrussee/caveman -a codex -y")
		},
	},
	VerifyFor: map[string]core.VerifyFn{
		"opencode": func() *bool { return core.BoolPtr(opencodePluginInstalled()) },
	},
	UnwireFor: map[string]core.AgentFn{
		"claude": func(opts core.RunOpts) (bool, error) {
			return cavemanExec("npx",
				[]string{"-y", "github:JuliusBrussee/caveman", "--", "--uninstall", "--only", "claude"},
				opts, "npx -y github:JuliusBrussee/caveman -- --uninstall --only claude")
		},
		"opencode": func(opts core.RunOpts) (bool, error) {
			return cavemanExec("npx",
				[]string{"-y", "github:JuliusBrussee/caveman", "--", "--uninstall", "--only", "opencode"},
				opts, "npx -y github:JuliusBrussee/caveman -- --uninstall --only opencode")
		},
		"codex": func(opts core.RunOpts) (bool, error) {
			return cavemanExec("npx", []string{"skills", "remove", "caveman"}, opts, "npx skills remove caveman")
		},
	},
}
