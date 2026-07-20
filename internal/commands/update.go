package commands

import (
	"fmt"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"

	toolsPkg "github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

func RunUpdate(opts InitOptions) int {
	MaybeSelfUpdate(opts)

	cmdHeader("update", "refresh tools to latest")

	if opts.DryRun {
		util.L.Raw("  " + util.C.Cyan(util.Sym.Info) + " Dry run — probe only, no installs.")
		util.L.Raw("")
	}

	probingLine := "  " + util.C.Gray("probing upstream…")
	if stdoutTTY() {
		fmt.Print(probingLine)
	} else {
		util.L.Raw(probingLine)
	}
	versions := util.GatherVersionsForce()
	if stdoutTTY() {
		fmt.Print(util.EraseStyledLine(probingLine))
	} else {
		util.L.Raw("")
	}

	var changed []string
	util.TreeTop("Versions")
	for _, t := range core.ListTools() {
		if t.InstructionOnly {
			continue
		}
		info, has := versions[t.ID]
		name := paintName(padEnd(t.ID, 14))

		installed := util.C.Dim("not on PATH")
		if has && info.Installed != nil {
			installed = paintVer(padEnd("v"+*info.Installed, 10))
		} else {
			installed = util.C.Dim(padEnd("not on PATH", 10))
		}

		latest := util.C.Dim(padEnd("?", 10))
		if has && info.Latest != nil {
			latest = paintVer(padEnd("v"+*info.Latest, 10))
		}

		mark := util.C.Gray(util.Sym.Bullet)
		suffix := util.C.Dim(" (latest unknown)")

		switch {
		case has && info.Installed != nil && info.Latest != nil && util.SemverCompare(info.Installed, info.Latest) < 0:
			mark = util.C.Yellow("↑")
			latest = util.C.Bold(util.C.Green(padEnd("v"+*info.Latest, 10)))
			suffix = util.C.Yellow(" → upgrade")
			changed = append(changed, t.ID)
		case has && info.Installed == nil && info.Latest != nil:
			mark = util.C.Yellow("+")
			latest = util.C.Bold(util.C.Green(padEnd("v"+*info.Latest, 10)))
			suffix = util.C.Yellow(" → install")
			changed = append(changed, t.ID)
		case has && info.Installed != nil && info.Latest != nil:
			mark = util.C.Green(util.Sym.Check)
			suffix = util.C.Green(" (up to date)")
		}

		util.TreeLeaf(mark + " " + name + " " + installed + " " + paintArrow() + " " + latest + suffix)
	}
	util.TreeClose()

	if opts.DryRun {
		if len(changed) > 0 {
			treeStatus(statusInfo(util.C.Gray("Would upgrade: ") + util.C.Bold(paintVer(joinComma(changed)))))
		} else {
			treeStatus(statusOK("Everything up to date."))
		}
		printRepoFooter(true)
		util.L.Raw("")
		return 0
	}

	if len(changed) == 0 {
		agents.PiUpdatePackages()
		treeStatus(statusOK("Everything up to date."))
		printRepoFooter(true)
		util.L.Raw("")
		return 0
	}
	if !opts.Yes && util.IsInteractive() {
		var pick []util.MultiSelectOption
		for _, t := range core.ListTools() {
			if !contains(changed, t.ID) {
				continue
			}
			info := versions[t.ID]
			installed := "not on PATH"
			if info.Installed != nil {
				installed = "v" + *info.Installed
			}
			latest := "?"
			if info.Latest != nil {
				latest = "v" + *info.Latest
			}
			hint := "install"
			if info.Installed != nil {
				hint = "upgrade"
			}
			pick = append(pick, util.MultiSelectOption{Value: t.ID, Label: padEnd(t.ID, 14) + installed + " → " + latest, Hint: hint, Selected: true})
		}
		changed = util.MultiSelect("Select tools to update", pick)
		if len(changed) == 0 {
			treeStatus(statusInfo(util.C.Dim("No tools selected.")))
			printRepoFooter(true)
			util.L.Raw("")
			return 0
		}
	}

	if !opts.DryRun {
		needNode, needGit, minNode := false, false, 0
		for _, t := range core.ListTools() {
			if contains(changed, t.ID) {
				needNode = needNode || toolNeedsNode(t)
				needGit = needGit || t.NeedsGit
				if t.MinNodeMajor > minNode {
					minNode = t.MinNodeMajor
				}
			}
		}
		nodeOK, gitOK := util.EnsureDeps(needNode, needGit, minNode)
		if !nodeOK || !gitOK {
			var keep []string
			for _, id := range changed {
				tool := core.GetTool(id)
				if tool == nil {
					continue
				}
				if toolNeedsNode(tool) && !nodeOK {
					continue
				}
				if tool.NeedsGit && !gitOK {
					continue
				}
				keep = append(keep, id)
			}
			changed = keep
		}
		if len(changed) == 0 {
			treeStatus(util.C.Red(util.Sym.Cross) + " " + util.C.Gray("Missing dependencies; nothing safe to update."))
			printRepoFooter(true)
			util.L.Raw("")
			return 1
		}
	}
	allTools := core.ListTools()
	var tools []*core.ToolManifest
	for _, t := range allTools {
		if contains(changed, t.ID) {
			tools = append(tools, t)
		}
	}
	bar := util.NewSectionProgress("Upgrading " + joinComma(changed))
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

	// Re-pin upgraded tools' per-agent config to the freshly installed version.
	if !opts.DryRun {
		toolsPkg.ConfigureInstructionConflicts(true)
		resyncWiring(tools)
		toolsPkg.ConfigureInstructionConflicts(false)
		agents.PiUpdatePackages()
	}

	// Upgrade mutated installed versions; drop cached latest so next read is fresh.
	if !opts.DryRun {
		util.BustVersionCache()
	}
	treeStatus(statusOK("Updated " + joinComma(changed) + "."))
	printRepoFooter(true)
	util.L.Raw("")
	return 0
}

// resyncWiring re-runs WireFor for each upgraded tool only on agents where it is
// already wired (gated by VerifyFor), syncing version pins without newly wiring.
func resyncWiring(tools []*core.ToolManifest) {
	agents := core.ListAgents()
	for _, tool := range tools {
		for _, agent := range agents {
			wire, ok := tool.WireFor[agent.ID]
			if !ok || !agent.Detect().Installed {
				continue
			}
			if verify, vok := tool.VerifyFor[agent.ID]; vok {
				if r := verify(); r == nil || !*r {
					continue
				}
			}
			_ = util.WithSilencedLogs(func() error {
				_, e := wire(core.RunOpts{Upgrade: true})
				return e
			})
		}
	}
}
