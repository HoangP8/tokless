package commands

import (
	"fmt"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"

	toolsPkg "github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

func updateToolFlow(id string, info util.VersionInfo, install func() (bool, bool, error)) (needsInstall, verified bool, err error) {
	if id == "projectmem" || id == "headroom" {
		needsInstall = !info.Present
	} else if id == "context-mode" && !toolsPkg.CtxNeedsGlobalCLI() {
		needsInstall = false
	} else {
		needsInstall = info.Installed == nil && info.Latest != nil
	}
	if install != nil {
		installed, present, installErr := install()
		return needsInstall, installed && ((id != "projectmem" && id != "headroom") || present), installErr
	}
	return needsInstall, false, nil
}

func updateScope(opts InitOptions) ([]*core.ToolManifest, error) {
	all := core.ListTools()
	if len(opts.Tools) == 0 {
		return all, nil
	}
	var bad []string
	for _, id := range opts.Tools {
		if core.GetTool(id) == nil {
			bad = append(bad, id)
		}
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("Invalid value(s): %s. Allowed: %s", joinComma(bad), joinComma(core.ToolIDs()))
	}
	scope, _ := resolveToolSelection(opts, all)
	return scope, nil
}

func RunUpdate(opts InitOptions) int {
	scope, err := updateScope(opts)
	if err != nil {
		util.L.Err(err.Error())
		return 2
	}
	scoped := len(opts.Tools) > 0
	if !scoped {
		MaybeSelfUpdate(opts)
	}

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
	versions := util.GatherVersionsForceFor(opts.Tools)
	if stdoutTTY() {
		fmt.Print(util.EraseStyledLine(probingLine))
	} else {
		util.L.Raw("")
	}

	var changed []string
	util.TreeTop("Versions")
	for _, t := range scope {
		if t.InstructionOnly {
			continue
		}
		info, has := versions[t.ID]
		needsInstall, _, _ := updateToolFlow(t.ID, info, nil)
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

		// OpenCode pins a plugin: compare the pin, not the CLI.
		pinStale, hideStale, migrateStale, pluginOnly, permStale := false, false, false, false, false
		if t.ID == "context-mode" {
			pluginOnly = !toolsPkg.CtxNeedsGlobalCLI()
			permStale = toolsPkg.CtxToolPermsStale()
			hideStale = toolsPkg.CtxOpenCodeHideStale()
			migrateStale = toolsPkg.CtxOpenCodeMigrateStale()
			if lv, err := util.ContextModeLatestVersion(); err == nil {
				pinStale = toolsPkg.CtxOpenCodePinStale(lv)
				if pluginOnly {
					latest = paintVer(padEnd("v"+lv, 10))
				}
			}
			if pluginOnly {
				installed = util.C.Dim(padEnd("plugin", 10))
			}
		}

		mark := util.C.Gray(util.Sym.Bullet)
		suffix := util.C.Dim(" (latest unknown)")

		switch {
		case pinStale || hideStale || migrateStale || permStale:
			mark = util.C.Yellow("↑")
			suffix = util.C.Yellow(" → " + openCodeResyncNote(pinStale, hideStale, migrateStale))
			changed = append(changed, t.ID)
		case pluginOnly:
			mark = util.C.Green(util.Sym.Check)
			suffix = util.C.Green(" (up to date)")
		case has && info.Installed != nil && info.Latest != nil && util.SemverCompare(info.Installed, info.Latest) < 0:
			mark = util.C.Yellow("↑")
			latest = util.C.Bold(util.C.Green(padEnd("v"+*info.Latest, 10)))
			suffix = util.C.Yellow(" → upgrade")
			changed = append(changed, t.ID)
		case has && needsInstall:
			mark = util.C.Yellow("+")
			if info.Latest != nil {
				latest = util.C.Bold(util.C.Green(padEnd("v"+*info.Latest, 10)))
			}
			suffix = util.C.Yellow(" → install")
			changed = append(changed, t.ID)
		case has && info.Installed != nil && info.Latest != nil:
			mark = util.C.Green(util.Sym.Check)
			suffix = util.C.Green(" (up to date)")
		case has && info.Present && info.Latest != nil:
			mark = util.C.Green(util.Sym.Check)
			suffix = util.C.Green(" (up to date)")
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
		toolsPkg.ConfigureInstructionConflicts(true)
		resyncInstructionWiring(opts.Tools...)
		toolsPkg.ConfigureInstructionConflicts(false)
		if !scoped {
			agents.PiUpdatePackages()
		}
		treeStatus(statusOK("Everything up to date."))
		printRepoFooter(true)
		util.L.Raw("")
		return 0
	}
	if !scoped && !opts.Yes && util.IsInteractive() {
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

	nodeOK := true
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
		var gitOK bool
		nodeOK, gitOK = util.EnsureDeps(needNode, needGit, minNode)
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
	allowNode := false
	if !opts.DryRun {
		allowNode = nodeInstallNotice(tools, true, nodeOK)
	}
	bar := util.NewSectionProgress("Upgrading " + joinComma(changed))
	bar.Start(len(tools))
	var failed, updated []string
	for _, tool := range tools {
		bar.Begin(tool.Label)
		report := func(phase string, frac float64) { bar.Step(phase, frac) }
		installed := false
		_, verified, err := updateToolFlow(tool.ID, versions[tool.ID], func() (bool, bool, error) {
			var installErr error
			installErr = util.WithSilencedLogs(func() error {
				ok, e := tool.Install(core.RunOpts{DryRun: opts.DryRun, Upgrade: true, AllowNodeInstall: allowNode, Report: report})
				installed = ok
				return e
			})
			present := true
			switch tool.ID {
			case "projectmem":
				present = util.ProjectmemInstalled()
			case "headroom":
				present = util.HeadroomInstalled()
			}
			return installed, present, installErr
		})
		switch {
		case err != nil:
			bar.Fail(firstLine(err.Error()))
			failed = append(failed, tool.ID)
		case !installed:
			bar.Fail("install failed")
			failed = append(failed, tool.ID)
		case !verified:
			bar.Fail("install verification failed")
			failed = append(failed, tool.ID)
		default:
			bar.Complete("")
			updated = append(updated, tool.ID)
		}
	}
	bar.Done("")

	// Re-pin upgraded tools' per-agent config to the freshly installed version.
	if !opts.DryRun {
		toolsPkg.ConfigureInstructionConflicts(true)
		for id, e := range resyncWiringOpts(tools, core.RunOpts{Upgrade: true}) {
			treeStatus(statusWarn(id + " re-pin failed: " + firstLine(e.Error())))
			updated = removeString(updated, id)
			if !contains(failed, id) {
				failed = append(failed, id)
			}
		}
		resyncInstructionWiring(opts.Tools...)
		toolsPkg.ConfigureInstructionConflicts(false)
		if !scoped {
			agents.PiUpdatePackages()
		}
	}

	// Upgrade mutated installed versions; drop cached latest so next read is fresh.
	if !opts.DryRun {
		util.BustVersionCache()
	}
	if len(failed) > 0 {
		msg := joinComma(failed) + " failed"
		if len(updated) > 0 {
			msg = "Updated " + joinComma(updated) + "; " + msg
		}
		treeStatus(statusWarn(msg + "."))
		printRepoFooter(true)
		util.L.Raw("")
		return 1
	}
	treeStatus(statusOK("Updated " + joinComma(changed) + "."))
	printRepoFooter(true)
	util.L.Raw("")
	return 0
}

// resyncWiring re-runs WireFor for each upgraded tool only on agents where it is
// already wired (gated by VerifyFor), syncing version pins without newly wiring.
func resyncWiring(tools []*core.ToolManifest) {
	resyncWiringOpts(tools, core.RunOpts{Upgrade: true})
}

func resyncWiringOpts(tools []*core.ToolManifest, ro core.RunOpts) map[string]error {
	errs := map[string]error{}
	agents := core.ListAgents()
	for _, tool := range tools {
		for _, agent := range agents {
			wire, ok := tool.WireFor[agent.ID]
			if !ok || !agent.Detect().Installed {
				continue
			}
			if verify, vok := tool.WiredAnyFor[agent.ID]; vok {
				if r := verify(); r == nil || !*r {
					continue
				}
			} else if verify, vok := tool.VerifyFor[agent.ID]; vok {
				if r := verify(); r == nil || !*r {
					continue
				}
			}
			_ = util.WithSilencedLogs(func() error {
				_, e := wire(ro)
				if e != nil && errs[tool.ID] == nil {
					errs[tool.ID] = e
				}
				return nil
			})
		}
	}
	return errs
}

func removeString(ss []string, s string) []string {
	out := ss[:0:0]
	for _, v := range ss {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// resyncInstructionWiring refreshes already-wired static instructions on update.
func resyncInstructionWiring(ids ...string) {
	var tools []*core.ToolManifest
	for _, tool := range core.ListTools() {
		if tool.InstructionOnly && (len(ids) == 0 || contains(ids, tool.ID)) {
			tools = append(tools, tool)
		}
	}
	resyncWiring(tools)
}

func openCodeResyncNote(pin, hide, migrate bool) string {
	switch {
	case migrate:
		return "migrate to plugin"
	case pin && hide:
		return "re-pin OpenCode plugin, hide ctx tools"
	case pin:
		return "re-pin OpenCode plugin"
	case hide:
		return "hide ctx tools"
	}
	return "add tool permissions"
}
