package commands

import (
	"os"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	toolsPkg "github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

// InitOptions carries flags shared across init/update.
type InitOptions struct {
	Agents  []string
	Tools   []string
	Agent   string
	DryRun  bool
	Yes     bool
	Verbose bool
	Upgrade bool
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func hasToolForAgent(tools []*core.ToolManifest, agentID string) bool {
	for _, tool := range tools {
		if _, ok := tool.WireFor[agentID]; ok {
			return true
		}
	}
	return false
}

func RunInit(opts InitOptions) int {
	util.SetQuiet(!opts.Verbose)
	if os.Getenv("TOKLESS_INSTALLER_RUN") == "1" {
		opts.Upgrade = true
	} else {
		MaybeSelfUpdate(opts)
		util.L.Raw("")
	}

	allTools := core.ListTools()
	var tools []*core.ToolManifest
	if opts.Tools != nil {
		for _, t := range allTools {
			if contains(opts.Tools, t.ID) {
				tools = append(tools, t)
			}
		}
	} else {
		tools = allTools
	}

	needNode, needGit := false, false
	minNode := 0
	for _, t := range tools {
		needNode = needNode || toolNeedsNode(t)
		needGit = needGit || t.NeedsGit
		if t.MinNodeMajor > minNode {
			minNode = t.MinNodeMajor
		}
	}
	nodeOK, gitOK := true, true
	if !opts.DryRun {
		nodeOK, gitOK = util.EnsureDeps(needNode, needGit, minNode)
	}

	var installTools []*core.ToolManifest
	for _, tool := range tools {
		if !tool.InstructionOnly {
			installTools = append(installTools, tool)
		}
	}
	toolBar := util.NewRootSectionProgress("Tools")
	toolBar.Start(len(installTools))
	installLogs := map[string]string{}
	for _, tool := range installTools {
		toolBar.Begin(tool.Label)
		if toolNeedsNode(tool) && !nodeOK {
			toolBar.Fail("needs Node.js — https://nodejs.org/en/download")
			continue
		}
		report := func(phase string, frac float64) { toolBar.Step(phase, frac) }
		installed := false
		logs, err := util.CaptureLogs(func() error {
			ok, e := tool.Install(core.RunOpts{DryRun: opts.DryRun, Upgrade: opts.Upgrade, Report: report})
			installed = ok
			return e
		})
		switch {
		case err != nil:
			toolBar.Fail(firstLine(err.Error()))
			installLogs[tool.Label] = logs
		case !installed:
			toolBar.Fail("install failed")
			installLogs[tool.Label] = logs
		default:
			toolBar.Complete(toolVersionNote(tool))
		}
	}
	toolBar.Done("")
	printFailureDetail(installLogs)

	if !opts.DryRun {
		util.SelfHealPath()
	}

	allAgents := core.ListAgents()
	installedIDs := map[string]bool{}
	detectedSource := map[string]string{}
	for _, a := range allAgents {
		if d := a.Detect(); d.Installed {
			installedIDs[a.ID] = true
			detectedSource[a.ID] = d.Source
		}
	}

	var requested []string
	switch {
	case len(opts.Agents) > 0:
		requested = opts.Agents
	case !util.IsInteractive():
		// Non-interactive shell: wire each detected agent supported by a selected tool.
		for _, a := range allAgents {
			if installedIDs[a.ID] && hasToolForAgent(tools, a.ID) {
				requested = append(requested, a.ID)
			}
		}
		util.SetQuiet(false)
		util.L.Raw("")
		if len(requested) == 0 {
			util.L.Raw("  " + util.C.Yellow(util.Sym.Warn) + " Non-interactive shell and no compatible agents detected — nothing to wire.")
			util.L.Raw("  " + util.C.Gray("Run tokless in a terminal to pick agents, or: ") + util.C.Cyan("tokless --agents claude,opencode,codex"))
			util.L.Raw("")
			return 0
		}
		labels := make([]string, len(requested))
		for i, id := range requested {
			labels[i] = core.GetAgent(id).Label
		}
		util.L.Raw("  " + util.C.Gray("Non-interactive shell — auto-selecting installed agents: ") + util.C.Bold(joinComma(labels)))
		util.L.Raw("  " + util.C.Gray("To choose explicitly: ") + util.C.Cyan("tokless --agents <claude,opencode,codex>"))
	default:
		var optsList []util.MultiSelectOption
		for _, a := range allAgents {
			if !installedIDs[a.ID] || !hasToolForAgent(tools, a.ID) {
				continue
			}
			opt := util.MultiSelectOption{Value: a.ID, Label: a.Label}
			if s := detectedSource[a.ID]; s != "" && s != "config" {
				opt.Hint = s
			}
			optsList = append(optsList, opt)
		}
		if len(optsList) == 0 {
			util.L.Raw("")
			util.L.Raw("  " + util.C.Yellow(util.Sym.Warn) + " No compatible agents detected — nothing to wire.")
			util.L.Raw("  " + util.C.Gray("Install an agent, then re-run tokless; or select one explicitly: ") + util.C.Cyan("tokless --agents claude,opencode,codex"))
			util.L.Raw("")
			return 0
		}
		requested = util.MultiSelect("Select agents to install tokless", optsList)
	}

	var wireIDs, skipped []string
	for _, id := range requested {
		if installedIDs[id] {
			wireIDs = append(wireIDs, id)
		} else {
			wireIDs = append(wireIDs, id)
		}
	}
	for _, id := range skipped {
		a := core.GetAgent(id)
		if a == nil {
			continue
		}
		util.L.Raw("  " + util.C.Yellow(util.Sym.Warn) + " " + a.Label + " not installed — install it first: " + util.C.Cyan(a.Homepage))
	}

	failures := map[string][]string{}
	wireLogs := map[string]string{}
	if contains(requested, "cursor") {
		workspace, _, err := cursorProjectDir(true)
		if err != nil {
			failures["cursor"] = []string{"project rules"}
			wireLogs["cursor"] = err.Error()
		} else {
			ok, err := util.InstallCursorProjectRules(workspace, opts.DryRun)
			if err != nil || !ok {
				failures["cursor"] = []string{"project rules"}
				if err != nil {
					wireLogs["cursor"] = err.Error()
				}
			}
			if !opts.DryRun && !agents.InstallCursorProjectRulesHook() {
				failures["cursor"] = append(failures["cursor"], "project rules hook")
			}
		}
	}

	if len(wireIDs) == 0 {
		util.SetQuiet(false)
		for id, failed := range failures {
			util.TreeLeaf(util.C.Yellow(util.Sym.Warn) + " " + core.GetAgent(id).Label + ": " + joinComma(failed) + " not wired.")
			printFailureDetail(map[string]string{core.GetAgent(id).Label: wireLogs[id]})
		}
		if len(skipped) == 0 {
			util.L.Raw("  " + util.C.Gray("Nothing selected. Tools are installed; re-run to wire an agent."))
		}
		util.L.Raw("")
		return 0
	}

	wireBar := util.NewSectionProgress("Agents")
	wireBar.Start(len(wireIDs))
	for _, agentID := range wireIDs {
		agent := core.GetAgent(agentID)
		wireBar.Begin(agent.Label)
		var failed []string
		wireOut, _ := util.CaptureLogs(func() error {
			for ti, tool := range tools {
				fn, ok := tool.WireFor[agentID]
				if !ok {
					continue
				}
				wireBar.Step("installing "+tool.Label, float64(ti+1)/float64(len(tools)))
				if toolNeedsNode(tool) && !nodeOK {
					util.L.Err(tool.Label + " needs Node.js/npm — https://nodejs.org/en/download")
					failed = append(failed, tool.Label)
					continue
				}
				if tool.NeedsGit && !gitOK {
					util.L.Err(tool.Label + " needs git — https://git-scm.com/downloads")
					failed = append(failed, tool.Label)
					continue
				}
				okWire := false
				if res, err := fn(core.RunOpts{DryRun: opts.DryRun, Upgrade: opts.Upgrade}); err == nil {
					okWire = res
				}
				if okWire && !opts.DryRun && os.Getenv("TOKLESS_TEST") != "1" {
					if verify, ok := tool.VerifyFor[agentID]; ok {
						r := verify()
						okWire = r != nil && *r
					}
				}
				if !okWire {
					failed = append(failed, tool.Label)
				}
			}
			return nil
		})
		if len(failed) == 0 {
			wireBar.Complete("")
		} else {
			wireBar.Fail(plural(len(failed)) + " not wired")
			failures[agentID] = failed
			wireLogs[agentID] = wireOut
		}
	}
	wireBar.Done("")
	util.SetQuiet(false)
	toolsPkg.EnsureInstructionSeparators(wireIDs)

	var fullyOK []string
	for _, id := range wireIDs {
		if failures[id] == nil {
			fullyOK = append(fullyOK, id)
		}
	}
	v := util.GatherVersions()
	printEquippedAgentTree(fullyOK, tools, v)
	for id, failed := range failures {
		util.TreeLeaf(util.C.Yellow(util.Sym.Warn) + " " + core.GetAgent(id).Label + ": " +
			joinComma(failed) + " not wired. Run " + util.C.Cyan("tokless doctor") + " for details.")
		printFailureDetail(map[string]string{core.GetAgent(id).Label: wireLogs[id]})
	}
	printRepoFooter(true)
	util.L.Raw("")
	if len(failures) > 0 {
		return 1
	}
	return 0
}

// printEquippedAgentTree: one shared tool version block.
func printEquippedAgentTree(fullyOK []string, tools []*core.ToolManifest, v map[string]util.VersionInfo) {
	if len(fullyOK) == 0 {
		return
	}
	outdated := false
	var lines []string
	for _, tool := range tools {
		wired := false
		for _, agentID := range fullyOK {
			if _, ok := tool.WireFor[agentID]; ok {
				wired = true
				break
			}
		}
		if !wired {
			continue
		}
		line := toolVersionDisplayLine(tool, v[tool.ID])
		if line == "" {
			continue
		}
		if toolVersionOutdated(tool, v[tool.ID]) {
			outdated = true
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return
	}
	if outdated {
		util.TreeCornerStyled(util.C.Bold("Run ") + util.C.Cyan("tokless update"))
	} else {
		util.TreeCorner("All fully updated")
	}
	for _, line := range lines {
		util.TreeLeaf(line)
	}
}
