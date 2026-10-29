package commands

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	toolsPkg "github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

// InitOptions carries flags shared across init/update.
type InitOptions struct {
	Agents    []string
	Tools     []string
	Agent     string
	DryRun    bool
	Yes       bool
	Verbose   bool
	Upgrade   bool
	Uninstall bool
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

// resolveToolSelection applies explicit, interactive, or persisted tool selection.
func resolveToolSelection(opts InitOptions, allTools []*core.ToolManifest) ([]*core.ToolManifest, string) {
	var selectedIDs []string
	var mode string // "full" or "selected"
	if opts.Tools != nil {
		selectedIDs = opts.Tools
		mode = "selected"
	} else if util.IsInteractive() && !opts.DryRun {
		modeChoice := util.SelectOne("Install tools", []util.SelectOption{
			{Value: "full", Label: "Full (Default)", Selected: true},
			{Value: "selected", Label: "Selected"},
		})
		if modeChoice == "selected" {
			checklist := make([]util.MultiSelectOption, len(allTools))
			for i, t := range allTools {
				checklist[i] = util.MultiSelectOption{Value: t.ID, Label: t.Label, Hint: t.Homepage, Selected: true}
			}
			selectedIDs = util.MultiSelectWithoutStatus("Select tools to install", checklist)
			mode = "selected"
			if selectedIDs == nil {
				selectedIDs = []string{}
			}
		} else {
			mode = "full"
			selectedIDs = nil // signal full set
		}

	} else {
		saved := util.LoadSelection()
		if saved == nil || saved.Mode == "full" {
			mode = "full"
			selectedIDs = nil
		} else {
			mode = "selected"
			selectedIDs = saved.Tools
		}
	}

	var filtered []*core.ToolManifest
	if mode == "full" || selectedIDs == nil {
		filtered = allTools
	} else {
		for _, t := range allTools {
			if contains(selectedIDs, t.ID) {
				filtered = append(filtered, t)
			}
		}
	}
	return filtered, mode
}

// runExcludedToolsUnwire removes excluded tools' managed config. A reported
// no-op fails only when a presence signal still confirms managed config.
func runExcludedToolsUnwire(agentID string, allTools, selected []*core.ToolManifest, opts InitOptions) []string {
	var failed []string
	selectedSet := map[string]bool{}
	for _, t := range selected {
		selectedSet[t.ID] = true
	}
	wired := func(tool *core.ToolManifest) bool {
		if verify, ok := tool.WiredAnyFor[agentID]; ok {
			if current := verify(); current != nil {
				return *current
			}
			return false
		}
		if verify, ok := tool.VerifyFor[agentID]; ok {
			if current := verify(); current != nil {
				return *current
			}
		}
		return false
	}
	stillConfigured := func(tool *core.ToolManifest) bool {
		verify, ok := tool.WiredAnyFor[agentID]
		if !ok {
			return false
		}
		if current := verify(); current != nil {
			return *current
		}
		return false
	}
	for _, tool := range allTools {
		if selectedSet[tool.ID] {
			continue
		}
		unwire, ok := tool.UnwireFor[agentID]
		if !ok {
			continue
		}
		if !wired(tool) {
			continue
		}
		now, err := unwire(core.RunOpts{DryRun: opts.DryRun})
		if err != nil || (!now && stillConfigured(tool)) {
			if !opts.DryRun {
				util.L.Err(tool.Label + " unwire failed for " + agentID)
				failed = append(failed, tool.Label)
				if err != nil {
					util.L.Sub(err.Error())
				}
			}
		}
	}
	return failed
}

// SaveSelectionIfNeeded persists the selection unless dry-run is enabled.
func saveSelectionIfNeeded(mode string, tools []*core.ToolManifest, dryRun bool) string {
	if dryRun {
		return mode
	}
	var toolIDs []string
	if mode == "full" || len(tools) == len(core.ListTools()) {
		toolIDs = []string{}
		mode = "full"
	} else {
		for _, t := range tools {
			toolIDs = append(toolIDs, t.ID)
		}
		mode = "selected"
	}
	if err := util.SaveSelection(mode, toolIDs); err != nil {
		util.L.Err("cannot persist tool selection: " + err.Error())
	}
	return mode
}

// detectAgents probes all agents concurrently.
func detectAgents(allAgents []*core.AgentManifest) (map[string]bool, map[string]string) {
	results := make([]core.Detection, len(allAgents))
	var wg sync.WaitGroup
	for i, a := range allAgents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = a.Detect()
		}()
	}
	wg.Wait()

	installedIDs := make(map[string]bool, len(allAgents))
	detectedSource := make(map[string]string, len(allAgents))
	for i, a := range allAgents {
		if results[i].Installed {
			installedIDs[a.ID] = true
			detectedSource[a.ID] = results[i].Source
		}
	}
	return installedIDs, detectedSource
}

func RunInit(opts InitOptions) int {
	util.SetQuiet(!opts.Verbose)
	if os.Getenv("TOKLESS_INSTALLER_RUN") == "1" {
		opts.Upgrade = true
	} else {
		MaybeSelfUpdate(opts)
		util.L.Raw("")
	}
	if !opts.DryRun && util.ToklessPersistedAbs() == "" {
		util.SetQuiet(false)
		util.L.Err("cannot wire agents: no stable tokless executable found; install tokless or put an absolute tokless binary on PATH")
		return 1
	}
	var failures map[string][]string

	allTools := core.ListTools()
	tools, selectionMode := resolveToolSelection(opts, allTools)

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
	toolBar := util.NewSectionProgress("Tools")
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
	installedIDs, detectedSource := detectAgents(allAgents)

	var requested []string
	switch {
	case len(opts.Agents) > 0:
		requested = opts.Agents
	case !util.IsInteractive():
		// Non-interactive shell: wire each detected agent supported by a selected tool.
		if len(tools) == 0 {
			for _, a := range allAgents {
				if installedIDs[a.ID] {
					requested = append(requested, a.ID)
				}
			}
		} else {
			for _, a := range allAgents {
				if installedIDs[a.ID] && hasToolForAgent(tools, a.ID) {
					requested = append(requested, a.ID)
				}
			}
		}
		util.SetQuiet(false)
		util.L.Raw("")
		if len(requested) == 0 && len(tools) > 0 {
			util.TreeLeaf(util.C.Yellow(util.Sym.Warn) + " Non-interactive shell and no compatible agents detected — nothing to wire.")
			util.TreeLeaf(util.C.Gray("Run tokless in a terminal to pick agents, or: ") + util.C.Cyan("tokless --agents claude,opencode,codex"))
			util.L.Raw("")
			return 0
		}
		labels := make([]string, len(requested))
		for i, id := range requested {
			agent := core.GetAgent(id)
			if agent != nil {
				labels[i] = agent.Label
			}
		}
		if len(requested) > 0 {
			util.TreeLeaf(util.C.Gray("Non-interactive shell — auto-selecting installed agents: ") + util.C.Bold(joinComma(labels)))
			util.TreeLeaf(util.C.Gray("To choose explicitly: ") + util.C.Cyan("tokless --agents <claude,opencode,codex>"))
		}
	default:
		var optsList []util.MultiSelectOption
		for _, a := range allAgents {
			if len(tools) == 0 {
				if installedIDs[a.ID] {
					optsList = append(optsList, util.MultiSelectOption{Value: a.ID, Label: a.Label})
				}
				continue
			}
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
			if len(tools) == 0 {
				requested = nil
			} else {
				util.L.Raw("")
				util.TreeLeaf(util.C.Yellow(util.Sym.Warn) + " No compatible agents detected — nothing to wire.")
				util.TreeLeaf(util.C.Gray("Install an agent, then re-run tokless; or select one explicitly: ") + util.C.Cyan("tokless --agents claude,opencode,codex"))
				util.L.Raw("")
				return 0
			}
		} else {
			requested = util.MultiSelect("Select agents to install tokless", optsList)
		}
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
		util.TreeLeaf(util.C.Yellow(util.Sym.Warn) + " " + a.Label + " not installed — install it first: " + util.C.Cyan(a.Homepage))
	}

	failures = map[string][]string{}
	wireLogs := map[string]string{}
	type wireChange struct {
		tool  *core.ToolManifest
		agent string
	}
	var wireChanges []wireChange
	var cursorRollback func()
	if contains(requested, "cursor") {
		workspace, _, err := cursorProjectDir(true)
		if err != nil {
			failures["cursor"] = []string{"project rules"}
			wireLogs["cursor"] = err.Error()
		} else {
			cursorHookSnapshot, snapshotErr := agents.SnapshotCursorProjectRulesHook()
			if snapshotErr != nil {
				failures["cursor"] = []string{"project rules hook"}
				wireLogs["cursor"] = snapshotErr.Error()
			} else {
				type cursorRuleRollback struct {
					path, content string
				}
				var newRules []cursorRuleRollback
				for _, spec := range util.CursorProjectRuleSpecs() {
					path := filepath.Join(workspace, ".cursor", "rules", spec.Filename)
					if !util.Exists(path) {
						newRules = append(newRules, cursorRuleRollback{path: path, content: util.CursorProjectRuleContent(spec)})
					}
				}
				cursorRollback = func() {
					if err := cursorHookSnapshot.Restore(); err != nil {
						treeSub("Cursor project-rules hook rollback failed: " + err.Error())
					}
					for _, rule := range newRules {
						if raw, ok := util.ReadFileSafe(rule.path); ok && raw == rule.content {
							_ = os.Remove(rule.path)
						}
					}
				}
			}
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
		projectmemSeedProject(opts, tools)
		if cursorFails, ok := failures["cursor"]; ok && (contains(cursorFails, "project rules") || contains(cursorFails, "project rules hook")) {
			if cursorRollback != nil {
				cursorRollback()
			}
		}
		util.SetQuiet(false)
		for id, failed := range failures {
			agent := core.GetAgent(id)
			if agent == nil {
				continue
			}
			util.TreeLeaf(util.C.Yellow(util.Sym.Warn) + " " + agent.Label + ": " + joinComma(failed) + " not wired.")
			printFailureDetail(map[string]string{agent.Label: wireLogs[id]})
		}
		if len(requested) == 0 && len(tools) > 0 {
			util.TreeLeaf(util.C.Gray("Nothing selected. Tools are installed; re-run to wire an agent."))
		}
		util.L.Raw("")
		if len(failures) > 0 {
			return 1
		}
		return 0
	}

	wireBar := util.NewSectionProgress("Agents")
	wireBar.Start(len(wireIDs))
	for _, agentID := range wireIDs {
		agent := core.GetAgent(agentID)
		wireBar.Begin(agent.Label)
		var failed []string
		wireOut, _ := util.CaptureLogs(func() error {
			if unwireFailed := runExcludedToolsUnwire(agentID, allTools, tools, opts); len(unwireFailed) > 0 {
				failed = append(failed, unwireFailed...)
			}
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
				wasWired := false
				if !tool.InstructionOnly {
					if verify, exists := tool.VerifyFor[agentID]; exists {
						if current := verify(); current != nil {
							wasWired = *current
						}
					}
				}
				if res, err := fn(core.RunOpts{DryRun: opts.DryRun, Upgrade: opts.Upgrade}); err == nil {
					okWire = res
				}
				if okWire && !opts.DryRun && !tool.InstructionOnly {
					if verify, ok := tool.VerifyFor[agentID]; ok {
						r := verify()
						okWire = r != nil && *r
					}
				}
				if !okWire {
					failed = append(failed, tool.Label)
					if !opts.DryRun && !wasWired {
						wireChanges = append(wireChanges, wireChange{tool: tool, agent: agentID})
					}
				} else if !opts.DryRun && !wasWired {
					wireChanges = append(wireChanges, wireChange{tool: tool, agent: agentID})
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
	if len(failures) > 0 && len(wireChanges) > 0 {
		for i := len(wireChanges) - 1; i >= 0; i-- {
			change := wireChanges[i]
			failedList, agentFailed := failures[change.agent]
			if !agentFailed || !contains(failedList, change.tool.Label) {
				continue
			}
			if unwire, ok := change.tool.UnwireFor[change.agent]; ok {
				removed, err := unwire(core.RunOpts{Agent: change.agent})
				if err != nil {
					treeSub("rollback failed: " + change.tool.Label + " from " + change.agent + ": " + err.Error())
				} else if !removed {
					treeSub("rollback failed: " + change.tool.Label + " from " + change.agent + ": configuration was not removed")
				}
			}
		}
	}
	if len(failures) > 0 && cursorRollback != nil {
		if cursorFails, ok := failures["cursor"]; ok && (contains(cursorFails, "project rules") || contains(cursorFails, "project rules hook")) {
			cursorRollback()
		}
	}
	projectmemSeedProject(opts, tools)
	if len(failures) == 0 {
		toolsPkg.EnsureInstructionSeparators(wireIDs)
	}

	var fullyOK []string
	for _, id := range wireIDs {
		if failures[id] == nil && initAgentFullyWired(id, tools) {
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
	saveSelectionIfNeeded(selectionMode, tools, opts.DryRun)
	return 0
}

// projectmemSeedProject creates .projectmem/ during init so the first agent session has state to inject.
func projectmemSeedProject(opts InitOptions, tools []*core.ToolManifest) {
	if opts.DryRun || os.Getenv("TOKLESS_TEST") == "1" {
		return
	}
	selected := false
	for _, t := range tools {
		if t.ID == "projectmem" {
			selected = true
			break
		}
	}
	if !selected {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	root := findProjectDir(cwd)
	if !looksLikeProject(root) {
		return
	}
	if err := toolsPkg.EnsureProjectmemProject(root); err != nil {
		treeSub("projectmem: " + err.Error())
	}
	toolsPkg.RefreshProjectmemRuleFiles(root)
}

func initAgentFullyWired(agentID string, tools []*core.ToolManifest) bool {
	for _, tool := range tools {
		if tool.InstructionOnly {
			continue
		}
		verify, ok := tool.VerifyFor[agentID]
		if !ok {
			continue
		}
		wired := verify()
		if wired == nil || !*wired {
			return false
		}
	}
	return true
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
