package commands

import (
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func RunDisable(opts InitOptions) int {
	return disableImpl(opts, false, "Disabled")
}

func RunUninstall(opts InitOptions) int {
	return disableImpl(opts, true, "Uninstalled")
}

// purgeBinaries prompts the user and removes the binaries /
// npm globals tokless installed.
func purgeBinaries(opts InitOptions) int {
	if opts.DryRun {
		util.L.Sub("[dry-run] would purge tokless-installed binaries + npm globals")
		return 0
	}
	if os.Getenv("TOKLESS_TEST") == "1" {
		return 0
	}
	doPurge := opts.Yes
	if !doPurge && util.IsInteractive() {
		doPurge = util.Confirm("Also remove binaries/packages tokless installed (rtk, npm globals)?", false)
	}
	if !doPurge {
		return 0
	}
	return runPurge()
}

// purgeRtk asks rtk to clean up after itself, then deletes it. rtk refuses to
// uninstall without --global and exits non-zero, so its answer isn't a reason
// to keep the binary.
func purgeRtk(bin string) bool {
	_ = util.Run(bin, []string{"init", "--uninstall", "--global"}, util.RunOptions{Capture: true})
	return os.Remove(bin) == nil
}

// Best-effort: keep going if one removal fails.
func runPurge() int {
	n := 0
	if p := util.ResolveRtkBin(); p != "" && util.Exists(p) {
		if purgeRtk(p) {
			n++
		}
	}
	npm := util.ResolveNpmBinary()
	for _, t := range core.ListTools() {
		switch t.Channel {
		case core.ChannelNpm:
			if npm == "" || util.NpmInstalledVersionExported(t.Pkg) == nil {
				continue
			}
			if util.Run(npm, []string{"uninstall", "-g", t.Pkg}, util.RunOptions{Capture: true}).Code == 0 {
				n++
			}
		case core.ChannelPyPI:
			if t.ID == "headroom" {
				headroomProxyTeardown()
			}
			if util.PyGlobalUninstall(t.Pkg, t.Bin) {
				n++
			}
		}
	}
	// Skill files live outside agent configs, so unwiring leaves them behind.
	if util.Exists(util.SkillsRoot()) {
		if os.RemoveAll(util.SkillsRoot()) == nil {
			n++
		}
	}
	if util.Which("pi") != "" {
		for _, src := range agents.PiPackageList() {
			if agents.PiSourceHas(src) {
				if agents.PiRemoveSource(src) {
					n++
				}
			}
		}
	}
	return n
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
	if len(detected) == 0 {
		util.L.Raw("  " + util.C.Gray("nothing wired."))
		util.L.Raw("")
		return 0
	}

	// Stage 1: which agents to remove from.
	agentIDs := pickAgents(opts, detected, verb)
	if len(agentIDs) == 0 {
		util.L.Raw("  " + util.C.Gray("Nothing selected."))
		util.L.Raw("")
		return 0
	}

	// Stage 2: which tools to remove (default: all → complete removal).
	allTools := core.ListTools()
	tools := pickTools(opts, allTools, verb)
	if len(tools) == 0 {
		util.L.Raw("  " + util.C.Gray("Nothing selected."))
		util.L.Raw("")
		return 0
	}

	bar := util.NewProgress("")
	bar.Start(len(agentIDs))
	for _, id := range agentIDs {
		agent := core.GetAgent(id)
		bar.Begin(agent.Label)
		_ = util.WithSilencedLogs(func() error {
			for _, tool := range tools {
				if unwire, ok := tool.UnwireFor[id]; ok && !opts.DryRun {
					_, _ = unwire(core.RunOpts{DryRun: opts.DryRun})
				}
			}
			return nil
		})
		bar.Complete("")
	}
	bar.Done("")

	full := removeTools && !opts.DryRun && len(tools) == len(allTools) && len(agentIDs) == len(detected)
	if full {
		_ = purgeBinaries(opts)
		cacheDir := filepath.Join(util.Home(), ".cache", "tokless")
		if util.Exists(cacheDir) {
			_ = os.RemoveAll(cacheDir)
		}
		_ = os.Remove(util.InstallMarkerPath())
	}

	labels := make([]string, len(agentIDs))
	for i, id := range agentIDs {
		labels[i] = core.GetAgent(id).Label
	}
	toolLabels := make([]string, len(tools))
	for i, t := range tools {
		toolLabels[i] = t.Label
	}
	util.L.Raw("")
	util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + verb + " " + util.C.Bold(joinComma(toolLabels)) +
		util.C.Gray(" from ") + util.C.Bold(joinComma(labels)) + ".")

	if full {
		removeToklessItself(opts)
	}
	util.L.Raw("")
	return 0
}

// removeToklessItself deletes the CLI, its data dir and its PATH entry.
func removeToklessItself(opts InitOptions) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return
	}
	self := util.ToklessSelfPath()
	if self == "" {
		return
	}
	if !opts.Yes && util.IsInteractive() {
		if !util.Confirm("Also remove the tokless CLI itself ("+self+")?", true) {
			util.L.Raw("  " + util.C.Gray("Kept the tokless CLI. Remove later with: ") + util.C.Cyan("rm "+self))
			return
		}
	}
	for _, rc := range util.RemoveToklessPathBlock() {
		util.L.Raw("  " + util.C.Gray("cleaned PATH entry in ") + util.C.Gray(rc))
	}
	util.RemoveToklessDataDir()
	if util.RemoveSelf() {
		msg := "  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray("Removed the tokless CLI (") + util.C.Gray(self) + util.C.Gray(").")
		if util.IsWin {
			msg = "  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray("tokless CLI will be deleted once this process exits.")
		}
		util.L.Raw(msg)
		util.L.Raw("  " + util.C.Gray("Open a new shell to drop it from PATH."))
		return
	}
	util.L.Raw("  " + util.C.Yellow(util.Sym.Warn) + " " + util.C.Gray("Couldn't remove the CLI. Run: ") + util.C.Cyan("rm "+self))
}

// pickAgents resolves which agents to act on: --agents flag, else interactive
// multiselect (all detected pre-selected), else all detected.
func pickAgents(opts InitOptions, detected []string, verb string) []string {
	if opts.Agents != nil {
		var out []string
		for _, id := range opts.Agents {
			if contains(detected, id) {
				out = append(out, id)
			}
		}
		return out
	}
	if !util.IsInteractive() {
		return detected
	}
	util.L.Raw("")
	var optsList []util.MultiSelectOption
	for _, id := range detected {
		optsList = append(optsList, util.MultiSelectOption{Value: id, Label: core.GetAgent(id).Label, Selected: true})
	}
	return util.MultiSelect("Select agents to "+lower(verb)+" tokless from", optsList)
}

// pickTools resolves which tools to remove: --tools flag, else interactive
// multiselect (all pre-selected → default complete removal), else all tools.
func pickTools(opts InitOptions, allTools []*core.ToolManifest, verb string) []*core.ToolManifest {
	if opts.Tools != nil {
		var out []*core.ToolManifest
		for _, t := range allTools {
			if contains(opts.Tools, t.ID) {
				out = append(out, t)
			}
		}
		return out
	}
	if !util.IsInteractive() {
		return allTools
	}
	util.L.Raw("")
	var optsList []util.MultiSelectOption
	for _, t := range allTools {
		optsList = append(optsList, util.MultiSelectOption{Value: t.ID, Label: t.Label, Selected: true})
	}
	picked := util.MultiSelect("Select tools to "+lower(verb), optsList)
	var out []*core.ToolManifest
	for _, t := range allTools {
		if contains(picked, t.ID) {
			out = append(out, t)
		}
	}
	return out
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
