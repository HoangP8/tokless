package commands

import (
	"os"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// InitOptions carries flags shared across init/update.
type InitOptions struct {
	Agents  []string
	Tools   []string
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

	if !opts.DryRun {
		util.EnsureNodeForTools()
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

	toolBar := util.NewProgress("")
	toolBar.Start(len(tools))
	for _, tool := range tools {
		toolBar.Begin(tool.Label)
		err := util.WithSilencedLogs(func() error {
			_, e := tool.Install(core.RunOpts{DryRun: opts.DryRun, Upgrade: opts.Upgrade})
			return e
		})
		if err != nil {
			toolBar.Fail(firstLine(err.Error()))
		} else {
			toolBar.Complete("")
		}
	}
	toolBar.Done("")

	if !opts.DryRun {
		util.SelfHealPath()
	}

	allAgents := core.ListAgents()
	installedIDs := map[string]bool{}
	for _, a := range allAgents {
		if a.Detect().Installed {
			installedIDs[a.ID] = true
		}
	}

	var requested []string
	if len(opts.Agents) > 0 {
		requested = opts.Agents
	} else {
		util.L.Raw("")
		var optsList []util.MultiSelectOption
		for _, a := range allAgents {
			hint := util.C.Gray("not installed")
			if installedIDs[a.ID] {
				hint = util.C.Gray("installed")
			}
			optsList = append(optsList, util.MultiSelectOption{
				Value: a.ID, Label: a.Label, Hint: hint, Selected: installedIDs[a.ID],
			})
		}
		requested = util.MultiSelect("Which AI agent(s) to wire up?", optsList)
	}

	var wireIDs, skipped []string
	for _, id := range requested {
		if installedIDs[id] {
			wireIDs = append(wireIDs, id)
		} else {
			skipped = append(skipped, id)
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
	wireBar := util.NewProgress("")
	wireBar.Start(len(wireIDs))
	for _, agentID := range wireIDs {
		agent := core.GetAgent(agentID)
		wireBar.Begin(agent.Label)
		var failed []string
		_ = util.WithSilencedLogs(func() error {
			for _, tool := range tools {
				fn, ok := tool.WireFor[agentID]
				if !ok {
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
		}
	}
	wireBar.Done("")
	util.SetQuiet(false)

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
	}
	util.L.Raw("")
	if len(failures) > 0 {
		return 1
	}
	return 0
}
