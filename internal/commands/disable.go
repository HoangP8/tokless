package commands

import (
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func RunDisable(opts InitOptions) int {
	return disableImpl(opts, false, "Disabled")
}

func RunUninstall(opts InitOptions) int {
	return disableImpl(opts, true, "Uninstalled")
}

func disableImpl(opts InitOptions, removeTools bool, verb string) int {
	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless")) + util.C.Gray("  "+lower(verb)))

	var detected []string
	for _, a := range core.ListAgents() {
		if a.Detect().Installed {
			detected = append(detected, a.ID)
		}
	}
	var agentIDs []string
	if opts.Agents != nil {
		for _, id := range opts.Agents {
			if contains(detected, id) {
				agentIDs = append(agentIDs, id)
			}
		}
	} else {
		agentIDs = detected
	}
	if len(agentIDs) == 0 {
		util.L.Raw("  " + util.C.Gray("nothing wired."))
		util.L.Raw("")
		return 0
	}

	tools := core.ListTools()
	bar := util.NewProgress("")
	bar.Start(len(agentIDs))
	for _, id := range agentIDs {
		agent := core.GetAgent(id)
		bar.Begin(agent.Label)
		for _, tool := range tools {
			if unwire, ok := tool.UnwireFor[id]; ok && !opts.DryRun {
				_, _ = unwire(core.RunOpts{DryRun: opts.DryRun})
			}
		}
		bar.Complete("")
	}
	bar.Done("")

	if removeTools && !opts.DryRun {
		cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "tokless")
		if util.Exists(cacheDir) {
			_ = os.RemoveAll(cacheDir)
		}
	}

	labels := make([]string, len(agentIDs))
	for i, id := range agentIDs {
		labels[i] = core.GetAgent(id).Label
	}
	util.L.Raw("")
	util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + verb + " for " + util.C.Bold(joinComma(labels)) + ".")
	util.L.Raw("")
	return 0
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}
