package commands

import (
	"fmt"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"

	toolsPkg "github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

func RunUpdate(opts InitOptions) int {
	// Before the header: a successful self-update re-execs with the same args,
	// so the new binary prints the header and runs the tool update itself.
	MaybeSelfUpdate(opts)

	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless update")) + util.C.Gray("  refresh tools to latest"))
	util.L.Raw("")

	if opts.DryRun {
		util.L.Info("Dry run — would probe registries and reinstall changed tools only.")
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
	for _, t := range core.ListTools() {
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
		suffix := util.C.Gray(" (latest unknown)")

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
		if !opts.DryRun {
			agents.PiUpdatePackages()
		} else {
			util.L.Sub("[dry-run] would run: pi update <tokless-managed sources only>")
		}
		util.L.Ok("Everything up to date.")
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
			util.L.Raw("")
			util.L.Info("No tools selected.")
			util.L.Raw("")
			return 0
		}
	}

	util.L.Raw("  " + util.C.Bold("Upgrading: "+joinComma(changed)))
	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless")) + util.C.Gray("  global token-saver for AI agents"))

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
			util.L.Raw("")
			util.L.Err("Missing dependencies; nothing safe to update.")
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
	util.L.Raw("")
	util.L.Ok("Updated " + joinComma(changed) + ".")
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
