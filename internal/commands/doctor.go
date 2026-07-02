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
}

func RunDoctor(offline bool) int {
	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless doctor")) + util.C.Gray("  quick health check"))
	util.L.Raw("")

	tools := core.ListTools()
	var reports []agentReport
	for _, agent := range core.ListAgents() {
		det := agent.Detect()
		if !det.Installed {
			reports = append(reports, agentReport{label: agent.Label})
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
		reports = append(reports, agentReport{label: agent.Label, installed: true, wired: len(missing) == 0, missing: missing})
	}

	for _, r := range reports {
		doctorSummary(r)
	}

	if !offline && os.Getenv("TOKLESS_TEST") != "1" {
		if stdoutTTY() {
			fmt.Print("  " + util.C.Gray("checking for updates…"))
		} else {
			util.L.Raw("  " + util.C.Gray("checking for updates…"))
		}
		v := util.GatherVersions()
		outdated := util.CountOutdated(v)
		if stdoutTTY() {
			fmt.Print("\r\x1b[2K")
		} else {
			util.L.Raw("")
		}
		listToolVersions(tools, v)
		util.L.Raw("")
		if outdated > 0 {
			util.L.Warn(plural(outdated) + " available — run " + util.C.Cyan("tokless update"))
		} else {
			util.L.Ok("All up to date.")
		}
	}

	broken := 0
	for _, r := range reports {
		if r.installed && !r.wired {
			broken++
		}
	}
	if broken > 0 {
		util.L.Raw("")
		util.L.Info("Run " + util.C.Cyan("tokless") + " to fix.")
	}
	printRepoFooter()
	util.L.Raw("")
	return 0
}

func doctorSummary(r agentReport) {
	var mark, status string
	switch {
	case !r.installed:
		mark = util.C.Gray(util.Sym.Bullet)
		status = util.C.Gray("not installed")
	case r.wired:
		mark = util.C.Green(util.Sym.Check)
		status = util.C.Gray("all tools wired")
	default:
		mark = util.C.Yellow(util.Sym.Warn)
		status = util.C.Yellow("missing: " + joinComma(r.missing))
	}
	util.L.Raw("  " + mark + " " + padEnd(r.label, 14) + " " + status)
}

// listToolVersions prints one row per tool.
func listToolVersions(tools []*core.ToolManifest, v map[string]util.VersionInfo) {
	for _, tool := range tools {
		if tool.InstructionOnly {
			continue
		}
		info := v[tool.ID]
		switch {
		case tool.NotTrackable && info.Installed != nil:
			util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray(padEnd(tool.ID, 14)+"v"+*info.Installed))
		case tool.NotTrackable && info.Present:
			util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray(padEnd(tool.ID, 14)+"installed"))
		case tool.NotTrackable:
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" "+padEnd(tool.ID, 14)+"not installed"))
		case info.Installed != nil && info.Latest != nil && util.SemverCompare(info.Installed, info.Latest) < 0:
			util.L.Raw("  " + util.C.Yellow("↑") + " " + util.C.Gray(padEnd(tool.ID, 14)+padEnd("v"+*info.Installed, 10)+"→ ") + util.C.Green("v"+*info.Latest))
		case info.Installed != nil:
			row := padEnd(tool.ID, 14) + padEnd("v"+*info.Installed, 10)
			if info.Latest != nil {
				row += "→ v" + *info.Latest
			}
			util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray(row))
		default:
			util.L.Raw("  " + util.C.Gray("• "+padEnd(tool.ID, 14)+"not installed"))
		}
	}
}

func stdoutTTY() bool { return util.StdoutANSI() }
