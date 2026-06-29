package commands

import (
	"os"

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

func RunInit(opts InitOptions) int {
	util.SetQuiet(!opts.Verbose)

	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless")) + util.C.Gray("  global token-saver for AI agents"))

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
		needNode = needNode || t.Channel == core.ChannelNpm
		needGit = needGit || t.NeedsGit
		if t.MinNodeMajor > minNode {
			minNode = t.MinNodeMajor
		}
	}
	nodeOK, gitOK := true, true
	if !opts.DryRun {
		nodeOK, gitOK = util.EnsureDeps(needNode, needGit, minNode)
	}

	toolBar := util.NewProgress("")
	toolBar.Start(len(tools))
	installLogs := map[string]string{}
	for _, tool := range tools {
		toolBar.Begin(tool.Label)
		if tool.Channel == core.ChannelNpm && !nodeOK {
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
		// Non-interactive shell: can't prompt, so wire every installed agent
		// and say so explicitly (otherwise it looks like nothing happened).
		for _, a := range allAgents {
			if installedIDs[a.ID] {
				requested = append(requested, a.ID)
			}
		}
		util.SetQuiet(false)
		util.L.Raw("")
		if len(requested) == 0 {
			util.L.Raw("  " + util.C.Yellow(util.Sym.Warn) + " Non-interactive shell and no agents detected — nothing to wire.")
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
		util.L.Raw("")
		var optsList []util.MultiSelectOption
		for _, a := range allAgents {
			opt := util.MultiSelectOption{Value: a.ID, Label: a.Label}
			if !installedIDs[a.ID] {
				opt.Disabled = true
				opt.DisabledReason = "not installed"
				opt.Hint = a.Homepage
			} else if s := detectedSource[a.ID]; s != "" && s != "config" {
				opt.Hint = s
			}
			optsList = append(optsList, opt)
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

	if len(wireIDs) == 0 {
		util.SetQuiet(false)
		if len(skipped) == 0 {
			util.L.Raw("  " + util.C.Gray("Nothing selected. Tools are installed; re-run to wire an agent."))
		}
		util.L.Raw("")
		return 0
	}

	failures := map[string][]string{}
	wireLogs := map[string]string{}
	wireBar := util.NewProgress("")
	wireBar.Start(len(wireIDs))
	for _, agentID := range wireIDs {
		agent := core.GetAgent(agentID)
		wireBar.Begin(agent.Label)
		var failed []string
		wireOut, _ := util.CaptureLogs(func() error {
			for _, tool := range tools {
				fn, ok := tool.WireFor[agentID]
				if !ok {
					continue
				}
				if tool.NeedsGit && !gitOK {
					util.L.Err(tool.Label + " needs git — https://git-scm.com/downloads")
					failed = append(failed, tool.Label)
					continue
				}
				okWire := false
				if res, err := fn(core.RunOpts{DryRun: opts.DryRun}); err == nil {
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

	util.L.Raw("")
	var fullyOK []string
	for _, id := range wireIDs {
		if failures[id] == nil {
			fullyOK = append(fullyOK, id)
		}
	}
	if len(fullyOK) > 0 {
		labels := make([]string, len(fullyOK))
		for i, id := range fullyOK {
			labels[i] = core.GetAgent(id).Label
		}
		util.L.Raw("  " + util.C.Green(util.Sym.Check) + " Equipped " + util.C.Bold(joinComma(labels)) + ".")
	}
	for id, failed := range failures {
		util.L.Raw("  " + util.C.Yellow(util.Sym.Warn) + " " + core.GetAgent(id).Label + ": " +
			joinComma(failed) + " not wired. Run " + util.C.Cyan("tokless doctor") + " for details.")
		printFailureDetail(map[string]string{core.GetAgent(id).Label: wireLogs[id]})
	}
	notifyOutdated(opts)
	util.L.Raw("")
	if len(failures) > 0 {
		return 1
	}
	return 0
}

// notifyOutdated prints each tool's installed → latest version.
func notifyOutdated(opts InitOptions) {
	if opts.DryRun || os.Getenv("TOKLESS_TEST") == "1" {
		return
	}
	v := util.GatherVersions()
	util.L.Raw("")
	listToolVersions(core.ListTools(), v)
	if n := util.CountOutdated(v); n > 0 {
		util.L.Raw("")
		util.L.Warn(plural(n) + " available — run " + util.C.Cyan("tokless update"))
	}
}
