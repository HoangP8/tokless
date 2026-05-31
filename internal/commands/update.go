package commands

import (
	"fmt"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func RunUpdate(opts InitOptions) int {
	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless update")) + util.C.Gray("  refresh tools to latest"))
	util.L.Raw("")

	if opts.DryRun {
		util.L.Info("Dry run — would probe registries and reinstall changed tools only.")
	}

	util.BustVersionCache()
	if stdoutTTY() {
		fmt.Print("  " + util.C.Gray("probing upstream…"))
	} else {
		util.L.Raw("  " + util.C.Gray("probing upstream…"))
	}
	versions := util.GatherVersions()
	if stdoutTTY() {
		fmt.Print("\r\x1b[2K")
	} else {
		util.L.Raw("")
	}

	var changed []string
	for _, t := range core.ListTools() {
		if t.NotTrackable {
			ver := "skill"
			if info, ok := versions[t.ID]; ok && info.Latest != nil {
				ver = "v" + *info.Latest
			}
			util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray(padEnd(t.ID, 14)+" "+ver))
			continue
		}
		info, has := versions[t.ID]
		installed := util.C.Gray("not on PATH")
		if has && info.Installed != nil {
			installed = "v" + *info.Installed
		}
		latest := util.C.Gray("?")
		if has && info.Latest != nil {
			latest = "v" + *info.Latest
		}
		mark := util.C.Gray(util.Sym.Bullet)
		suffix := util.C.Gray(" (pinned)")
		switch {
		case has && info.Installed != nil && info.Latest != nil && util.SemverCompare(info.Installed, info.Latest) < 0:
			mark = util.C.Yellow("↑")
			suffix = util.C.Yellow(" → upgrade")
			changed = append(changed, t.ID)
		case has && info.Installed == nil && info.Latest != nil:
			mark = util.C.Yellow("+")
			suffix = util.C.Yellow(" → install")
			changed = append(changed, t.ID)
		case has && info.Installed != nil && info.Latest != nil:
			mark = util.C.Green(util.Sym.Check)
			suffix = util.C.Gray(" (up to date)")
		}
		util.L.Raw("  " + mark + " " + padEnd(t.ID, 14) + " " + padEnd(installed, 10) + " → " + padEnd(latest, 10) + suffix)
	}
	util.L.Raw("")

	if opts.DryRun {
		if len(changed) > 0 {
			util.L.Info("Would upgrade: " + joinComma(changed))
		} else {
			util.L.Info("Everything up to date.")
		}
		util.L.Raw("")
		return 0
	}

	if len(changed) == 0 {
		util.L.Ok("Everything up to date.")
		util.L.Raw("")
		return 0
	}

	util.L.Raw("  " + util.C.Bold("Upgrading: "+joinComma(changed)))
	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless")) + util.C.Gray("  global token-saver for AI agents"))

	if !opts.DryRun {
		util.EnsureNodeForTools()
	}
	allTools := core.ListTools()
	var tools []*core.ToolManifest
	for _, t := range allTools {
		if contains(changed, t.ID) {
			tools = append(tools, t)
		}
	}
	bar := util.NewProgress("")
	bar.Start(len(tools))
	for _, tool := range tools {
		bar.Begin(tool.Label)
		report := func(phase string, frac float64) { bar.Step(phase, frac) }
		err := util.WithSilencedLogs(func() error {
			_, e := tool.Install(core.RunOpts{DryRun: opts.DryRun, Upgrade: true, Report: report})
			return e
		})
		if err != nil {
			bar.Fail(firstLine(err.Error()))
		} else {
			bar.Complete("")
		}
	}
	bar.Done("")

	// Upgrade mutated installed versions; drop cached latest so next read is fresh.
	if !opts.DryRun {
		util.BustVersionCache()
	}
	util.L.Raw("")
	util.L.Ok("Updated " + joinComma(changed) + ".")
	util.L.Raw("")
	return 0
}
