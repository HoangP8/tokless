package commands

import (
	"fmt"
	"os"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

type agentReport struct {
	label     string
	installed bool
	wired     bool
	missing   []string
	runtime   []runtimeIssue
}

func RunDoctor(offline bool) int {
	cmdHeader("doctor", "quick health check")

	tools := core.ListTools()
	// UI + status: only agents present on this machine (Detect is OS-aware).
	var reports []agentReport
	for _, agent := range core.ListAgents() {
		if !agent.Detect().Installed {
			continue
		}
		var missing []string
		for _, tool := range tools {
			verify, ok := tool.VerifyFor[agent.ID]
			if !ok {
				continue
			}
			if r := verify(); r != nil && !*r {
				missing = append(missing, tool.Label)
			}
		}
		runtime := probeAgentRuntime(agent.ID)
		reports = append(reports, agentReport{
			label:     agent.Label,
			installed: true,
			wired:     len(missing) == 0 && len(runtime) == 0,
			missing:   missing,
			runtime:   runtime,
		})
	}

	util.TreeTop("Agents")
	if len(reports) == 0 {
		util.TreeLeaf(util.C.Dim("none detected on this machine"))
	} else {
		for _, r := range reports {
			util.TreeLeaf(doctorSummaryLine(r))
		}
	}
	util.TreeClose()

	broken := 0
	installedAgents := len(reports)
	for _, r := range reports {
		if !r.wired {
			broken++
		}
	}

	outdated := -1
	if !offline && os.Getenv("TOKLESS_TEST") != "1" {
		statusLine := util.TreeStem() + util.C.Gray("checking for updates…")
		if stdoutTTY() {
			fmt.Print(statusLine)
		} else {
			util.L.Raw(statusLine)
		}
		v := util.GatherVersions()
		outdated = util.CountOutdated(v)
		if stdoutTTY() {
			fmt.Print(util.EraseStyledLine(statusLine))
		} else {
			util.L.Raw("")
		}

		util.TreeCorner("Tools")
		listToolVersions(tools, v, true)
		util.TreeClose()
	}

	treeStatus(doctorStatusLines(outdated, broken, installedAgents)...)
	printRepoFooter(true)
	util.L.Raw("")
	return 0
}

// doctorStatusLines builds labeled Status leaves.
func doctorStatusLines(outdated, broken, installedAgents int) []string {
	toolsOK := outdated == 0
	agentsOK := broken == 0
	online := outdated >= 0

	if online && toolsOK && agentsOK {
		return []string{statusOK("Everything up to date.")}
	}

	var lines []string
	if online {
		if outdated > 0 {
			lines = append(lines, statusKV(
				util.C.Yellow(util.Sym.Warn),
				"tools",
				util.C.Yellow(plural(outdated)+" available")+" — run "+paintCmd("tokless update"),
			))
		} else {
			lines = append(lines, statusKV(
				util.C.Green(util.Sym.Check),
				"tools",
				util.C.Green("all up to date"),
			))
		}
	}

	if broken > 0 {
		msg := itoa(broken) + " incomplete"
		if broken == 1 {
			msg = "1 incomplete"
		}
		lines = append(lines, statusKV(
			util.C.Yellow(util.Sym.Warn),
			"agents",
			util.C.Yellow(msg)+" — run "+paintCmd("tokless"),
		))
	} else if installedAgents > 0 {
		lines = append(lines, statusKV(
			util.C.Green(util.Sym.Check),
			"agents",
			util.C.Green("all wired"),
		))
	} else {
		lines = append(lines, statusKV(
			util.C.Gray(util.Sym.Bullet),
			"agents",
			util.C.Dim("none installed"),
		))
	}

	if !online && broken == 0 && installedAgents > 0 {
		return []string{
			statusOK("Agents look good."),
			statusInfo(util.C.Gray("Skip --offline to check tool updates.")),
		}
	}
	return lines
}

func doctorSummaryLine(r agentReport) string {
	name := paintName(padEnd(r.label, 14))
	switch {
	case !r.installed:
		return util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(padEnd(r.label, 14)) + util.C.Gray("not installed")
	case r.wired:
		return util.C.Green(util.Sym.Check) + " " + name + util.C.Green("all tools wired")
	case len(r.runtime) > 0 && len(r.missing) == 0:
		return util.C.Yellow(util.Sym.Warn) + " " + name + util.C.Yellow("runtime: "+formatRuntimeIssues(r.runtime))
	case len(r.runtime) > 0:
		return util.C.Yellow(util.Sym.Warn) + " " + name + util.C.Yellow("missing: "+joinComma(r.missing)+"; runtime: "+formatRuntimeIssues(r.runtime))
	default:
		return util.C.Yellow(util.Sym.Warn) + " " + name + util.C.Yellow("missing: "+joinComma(r.missing))
	}
}

func toolVersionOutdated(tool *core.ToolManifest, info util.VersionInfo) bool {
	if tool.InstructionOnly || tool.NotTrackable {
		return false
	}
	return info.Installed != nil && info.Latest != nil && util.SemverCompare(info.Installed, info.Latest) < 0
}

func toolVersionDisplayLine(tool *core.ToolManifest, info util.VersionInfo) string {
	name := paintName(padEnd(tool.ID, 14))
	switch {
	case tool.InstructionOnly:
		return ""
	case tool.NotTrackable && info.Installed != nil:
		return util.C.Green(util.Sym.Check) + " " + name + paintVer("v"+*info.Installed)
	case tool.NotTrackable && info.Present:
		return util.C.Green(util.Sym.Check) + " " + name + util.C.Green("installed")
	case tool.NotTrackable:
		return util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(padEnd(tool.ID, 14)) + util.C.Gray("not installed")
	case toolVersionOutdated(tool, info):
		return util.C.Yellow("↑") + " " + name + paintVer(padEnd("v"+*info.Installed, 10)) + paintArrow() + " " + util.C.Bold(util.C.Green("v"+*info.Latest))
	case info.Installed != nil:
		row := name + paintVer(padEnd("v"+*info.Installed, 10))
		if info.Latest != nil {
			row += paintArrow() + " " + paintVer("v"+*info.Latest)
		}
		return util.C.Green(util.Sym.Check) + " " + row
	default:
		return util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(padEnd(tool.ID, 14)) + util.C.Gray("not installed")
	}
}

// listToolVersions prints one row per tool.
func listToolVersions(tools []*core.ToolManifest, v map[string]util.VersionInfo, tree bool) {
	for _, tool := range tools {
		if tool.InstructionOnly {
			continue
		}
		line := toolVersionDisplayLine(tool, v[tool.ID])
		if line == "" {
			continue
		}
		if tree {
			util.TreeLeaf(line)
		} else {
			util.L.Raw("  " + line)
		}
	}
}

func stdoutTTY() bool { return util.StdoutANSI() && !util.RunningInsideClaudeCode() }
