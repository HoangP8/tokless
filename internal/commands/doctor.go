package commands

import (
	"fmt"
	"os"

	"github.com/HoangP8/tokless/internal/agents"
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
		runtime := probeAgentRuntime(agent.ID)
		reports = append(reports, agentReport{
			label:     agent.Label,
			installed: true,
			wired:     len(missing) == 0 && len(runtime) == 0,
			missing:   missing,
			runtime:   runtime,
		})
	}

	for _, r := range reports {
		doctorSummary(r)
	}

	if !offline && os.Getenv("TOKLESS_TEST") != "1" {
		statusLine := "  " + util.C.Gray("checking for updates…")
		if stdoutTTY() {
			fmt.Print(statusLine)
		} else {
			util.L.Raw(statusLine)
		}
		v := util.GatherVersions()
		outdated := util.CountOutdated(v)
		if stdoutTTY() {
			fmt.Print(util.EraseStyledLine(statusLine))
		} else {
			util.L.Raw("")
		}
		listToolVersions(tools, v, false)
		listPiPackageVersions()
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
	printRepoFooter(false)
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
	case len(r.runtime) > 0 && len(r.missing) == 0:
		mark = util.C.Yellow(util.Sym.Warn)
		status = util.C.Yellow("runtime: " + formatRuntimeIssues(r.runtime))
	case len(r.runtime) > 0:
		mark = util.C.Yellow(util.Sym.Warn)
		status = util.C.Yellow("missing: " + joinComma(r.missing) + "; runtime: " + formatRuntimeIssues(r.runtime))
	default:
		mark = util.C.Yellow(util.Sym.Warn)
		status = util.C.Yellow("missing: " + joinComma(r.missing))
	}
	util.L.Raw("  " + mark + " " + padEnd(r.label, 14) + " " + status)
}

func toolVersionOutdated(tool *core.ToolManifest, info util.VersionInfo) bool {
	if tool.InstructionOnly || tool.NotTrackable {
		return false
	}
	return info.Installed != nil && info.Latest != nil && util.SemverCompare(info.Installed, info.Latest) < 0
}

func toolVersionDisplayLine(tool *core.ToolManifest, info util.VersionInfo) string {
	switch {
	case tool.InstructionOnly:
		return ""
	case tool.NotTrackable && info.Installed != nil:
		return util.C.Green(util.Sym.Check) + " " + util.C.Gray(padEnd(tool.ID, 14)+"v"+*info.Installed)
	case tool.NotTrackable && info.Present:
		return util.C.Green(util.Sym.Check) + " " + util.C.Gray(padEnd(tool.ID, 14)+"installed")
	case tool.NotTrackable:
		return util.C.Gray(util.Sym.Bullet + " " + padEnd(tool.ID, 14) + "not installed")
	case toolVersionOutdated(tool, info):
		return util.C.Yellow("↑") + " " + util.C.Gray(padEnd(tool.ID, 14)+padEnd("v"+*info.Installed, 10)+"→ ") + util.C.Green("v"+*info.Latest)
	case info.Installed != nil:
		row := padEnd(tool.ID, 14) + padEnd("v"+*info.Installed, 10)
		if info.Latest != nil {
			row += "→ v" + *info.Latest
		}
		return util.C.Green(util.Sym.Check) + " " + util.C.Gray(row)
	default:
		return util.C.Gray("• " + padEnd(tool.ID, 14) + "not installed")
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

// listPiPackageVersions lists tokless-managed Pi sources present in settings.
func listPiPackageVersions() {
	if util.Which("pi") == "" {
		return
	}
	any := false
	for _, src := range agents.PiPackageList() {
		if !agents.PiSourceHas(src) {
			continue
		}
		if !any {
			util.L.Raw("")
			util.L.Raw("  " + util.C.Bold("Pi packages") + util.C.Gray("  (MCP bridge; tokless update)"))
			any = true
		}
		exact := agents.PiPackageSettingsSource(src)
		if exact == "" {
			exact = src
		}
		util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray(exact))
	}
}

func stdoutTTY() bool { return util.StdoutANSI() && !util.RunningInsideClaudeCode() }
