package commands

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	toolindex "github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

type agentReport struct {
	id        string
	label     string
	installed bool
	wired     bool
	missing   []string
	runtime   []runtimeIssue
}

func toolConfigLocation(agentID, toolID string) (path, key string) {
	switch agentID {
	case "claude":
		path = util.ClaudeCodePaths().GlobalJSON
		key = "mcpServers." + toolID
	case "opencode":
		path = util.OpenCodePathsResolved().Config
		key = "mcp." + toolID
	case "opencode2":
		path = util.OpenCodePathsV2().Config
		key = "mcp.servers." + toolID
	case "codex":
		path = util.CodexPathsResolved().Config
		key = "mcp_servers." + toolID
	case "cursor":
		path = util.CursorGlobalMcpPath()
		key = "mcpServers." + toolID
	case "antigravity":
		path = util.AntigravityPathsResolved().McpConfig
		key = "mcpServers." + toolID
	case "copilot":
		path = util.CopilotPathsResolved().McpConfig
		key = "mcpServers." + toolID
	case "droid":
		path = filepath.Join(util.Home(), ".factory", "mcp.json")
		key = "mcpServers." + toolID
	case "grok":
		path = filepath.Join(util.Home(), ".grok", "config.toml")
		key = "mcp_servers." + toolID
	case "pi":
		path = filepath.Join(agents.PiAgentDirResolved(), "mcp.json")
		key = "mcpServers." + toolID
	case "omp":
		path = filepath.Join(agents.OmpAgentDirResolved(), "mcp.json")
		key = "mcpServers." + toolID
	case "kilo":
		path = util.KiloPathsResolved().Config
		key = "mcp." + toolID
	case "cline":
		path = util.ClinePathsResolved().McpConfig
		key = "mcpServers." + toolID
	case "amp":
		path = util.AmpPathsResolved().Config
		key = "amp.mcpServers." + toolID
	case "kimi":
		path = util.KimiPathsResolved().McpConfig
		key = "mcpServers." + toolID
	case "zcode":
		path = util.ZcodePathsResolved().McpConfig
		key = "mcp.servers." + toolID
	case "dsh":
		path = filepath.Join(util.Home(), ".dsh", "cordis.patch.yml")
		key = "mcp_servers." + toolID
	}
	if toolID == "projectmem" {
		switch agentID {
		case "claude", "cursor", "antigravity", "copilot", "droid", "pi", "omp", "cline", "kimi":
			key = "mcpServers.projectmem"
		case "opencode", "kilo":
			key = "mcp.projectmem"
		case "codex", "grok", "dsh":
			key = "mcp_servers.projectmem"
		case "amp":
			key = "amp.mcpServers.projectmem"
		case "zcode":
			key = "mcp.servers.projectmem"
		}
	}
	return path, key
}

func RunDoctor(offline, fix bool) int {
	cmdHeader("doctor", "health check")

	toolManifests := core.ListTools()
	// UI + status: only agents present on this machine (Detect is OS-aware).
	var reports []agentReport
	for _, agent := range core.ListAgents() {
		if !agent.Detect().Installed {
			continue
		}
		missing := doctorMissingTools(toolManifests, agent.ID)
		runtime := probeAgentRuntime(agent.ID)
		reports = append(reports, agentReport{
			id:        agent.ID,
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
			util.TreeLeaf(agentTreeRow(r))
			rows := doctorAgentRows(r)
			for i, row := range rows {
				if strings.HasPrefix(row, "  ├─ ") || strings.HasPrefix(row, "  └─ ") {
					if strings.HasPrefix(row, "  └─ ") {
						util.TreeSubChildLast(strings.TrimPrefix(row, "  └─ "))
					} else {
						util.TreeSubChildMid(strings.TrimPrefix(row, "  ├─ "))
					}
				} else if i == len(rows)-1 {
					util.TreeChildLast(row)
				} else {
					util.TreeChildMid(row)
				}
			}
		}
	}
	util.TreeClose()

	proxyRunningNow := proxyRunning()
	util.TreeCorner("Headroom Proxy")
	ports := doctorProxyPorts(reports, proxyRunningNow)
	for i, port := range ports {
		portHasSibling := i < len(ports)-1
		port.label = paintProxyLabel(port.label, port.url)
		if portHasSibling {
			util.TreeChildMid(port.label)
		} else {
			util.TreeChildLast(port.label)
		}
		printSubRow := func(row string, lastRow bool) {
			switch {
			case !portHasSibling && lastRow:
				util.TreeSubChildLastLast(row)
			case !portHasSibling:
				util.TreeSubChildMidLast(row)
			case lastRow:
				util.TreeSubChildLast(row)
			default:
				util.TreeSubChildMid(row)
			}
		}
		for j, row := range port.direct {
			printSubRow(row, j == len(port.direct)-1)
		}
	}
	util.TreeClose()

	projectDir, isProject, projectErr := cursorProjectDir(false)
	util.TreeCorner("Stats")
	toolStats := doctorToolStats(projectDirOrCwd(), isProject)
	toolIDs := []string{"headroom", "projectmem", "rtk", "codegraph", "context-mode"}
	printed := 0
	for _, id := range toolIDs {
		rows, ok := toolStats[id]
		if !ok {
			continue
		}
		printed++
		label := statsToolLabel(id)
		if printed == len(toolStats) {
			util.TreeChildLast(label)
		} else {
			util.TreeChildMid(label)
		}
		lastTool := printed == len(toolStats)
		for j, row := range rows {
			row = paintStatsRow(row)
			rowLast := j == len(rows)-1
			switch {
			case lastTool && rowLast:
				util.TreeSubChildLastLast(row)
			case lastTool:
				util.TreeSubChildMidLast(row)
			case rowLast:
				util.TreeSubChildLast(row)
			default:
				util.TreeSubChildMid(row)
			}
		}
	}
	if printed == 0 {
		util.TreeLeaf(util.C.Dim("no installed tools with local stats"))
	}
	util.TreeClose()

	util.TreeCorner("Project Init (created automatically per project)")
	switch {
	case projectErr != nil:
		util.TreeLeaf(statusWarn("cannot resolve current directory: " + projectErr.Error()))
	case !isProject:
		util.TreeLeaf(util.C.Dim("not in a project"))
	default:
		if toolindex.HasCodegraphIndex(projectDir) {
			util.TreeLeaf(statusOK("CodeGraph index present — run tokless index to refresh"))
		} else {
			util.TreeLeaf(statusWarn("CodeGraph index missing — run tokless index"))
		}
		if projectmemProjectStatus(projectDir) {
			util.TreeLeaf(statusOK("projectmem .projectmem/ marker present"))
		} else {
			util.TreeLeaf(statusWarn("projectmem .projectmem/ marker missing"))
		}
	}
	util.TreeClose()

	if fix && util.IsInteractive() {
		if !proxyRunningNow && util.Confirm("Start proxy now?", false) {
			RunProxyUp(InitOptions{})
		}
		if isProject && projectErr == nil && !toolindex.HasCodegraphIndex(projectDir) && util.Confirm("Build project index now?", false) {
			RunIndex(InitOptions{}, false)
		}
	}

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
		listToolVersions(toolManifests, v, true)
		util.TreeClose()
	}

	treeStatusLast(doctorStatusLines(outdated, broken, installedAgents)...)
	printRepoFooter(true)
	util.L.Raw("")
	return 0
}

type doctorPort struct {
	label, url string
	direct     []string
}

func doctorProxyPorts(reports []agentReport, mainRunning bool) []doctorPort {
	main := doctorPort{label: fmt.Sprintf("8787 Main proxy — %s", util.HeadroomProxyURL()), url: util.HeadroomProxyURL()}
	if mainRunning {
		main.direct = append(main.direct, statusOK("process running"))
	} else {
		main.direct = append(main.direct, statusWarn("process stopped"))
	}
	ports := []doctorPort{main}
	hasRoutes := false
	for _, report := range reports {
		if len(util.BYOKRoutesFor(report.id)) > 0 {
			hasRoutes = true
			break
		}
	}
	if len(util.BYOKRoutesFor("codex")) > 0 || hasRoutes {
		gatewayURL := util.BYOKGatewayEndpoint()
		ports = append(ports, doctorPort{label: fmt.Sprintf("%d BYOK gateway — %s", util.BYOKGatewayPort(), gatewayURL), url: gatewayURL, direct: []string{doctorHTTPHealth(gatewayURL + "/livez")}})
	}
	for _, r := range reports {
		if r.id == "copilot" {
			cliURL := "http://127.0.0.1:" + strconv.Itoa(agents.CopilotCLIProxyPort())
			vscodeURL := "http://127.0.0.1:" + strconv.Itoa(agents.CopilotProxyPort())
			ports = append(ports, doctorPort{label: fmt.Sprintf("%d Copilot CLI — %s", agents.CopilotCLIProxyPort(), cliURL), url: cliURL, direct: []string{wiringStatus("CLI wrapper", agents.CopilotCLIProxyWired())}})
			ports = append(ports, doctorPort{label: fmt.Sprintf("%d Copilot VS Code — %s", agents.CopilotProxyPort(), vscodeURL), url: vscodeURL, direct: []string{wiringStatus("VS Code wrapper", agents.CopilotVSCodeProxyWired())}})
		}
		if r.id == "grok" {
			wired := agents.GrokOAuthProxyWired()
			grokURL := "http://127.0.0.1:" + strconv.Itoa(util.GrokOAuthProxyPort()) + "/v1"
			ports = append(ports, doctorPort{label: fmt.Sprintf("%d Grok OAuth — %s", util.GrokOAuthProxyPort(), grokURL), url: grokURL, direct: []string{wiringStatus("OAuth wrapper", wired)}})
		}
	}
	return ports
}

func doctorHTTPHealth(url string) string {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return statusWarn("HTTP check failed: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return statusOK(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	return statusWarn(fmt.Sprintf("HTTP %d", resp.StatusCode))
}

func wiringStatus(label string, wired bool) string {
	if wired {
		return statusOK(label + " configured")
	}
	return statusWarn(label + " not configured")
}

func projectDirOrCwd() string { dir, _ := os.Getwd(); return dir }

func statsToolLabel(id string) string {
	return id
}

func paintStatsRow(row string) string {
	return row
}

func doctorProxyDetails(reports []agentReport) []string {
	var rows []string
	gatewayShown := false
	shownRoutes := make(map[string]bool)
	for _, r := range reports {
		if routes := util.BYOKRoutesFor(r.id); len(routes) > 0 {
			if !gatewayShown {
				rows = append(rows, statusOK("BYOK gateway "+util.BYOKGatewayEndpoint()))
				gatewayShown = true
			}
			for _, route := range routes {
				id := route.ID
				if parts := strings.SplitN(id, ":", 2); len(parts) == 2 {
					id = parts[1]
				}
				if shownRoutes[id] {
					continue
				}
				shownRoutes[id] = true
				rows = append(rows, statusOK("BYOK "+id+" → "+route.Upstream+" (token set)"))
			}
		}
		if r.id == "copilot" {
			if agents.CopilotCLIProxyWired() {
				rows = append(rows, statusInfo("Copilot CLI wrapper wired 127.0.0.1:"+strconv.Itoa(agents.CopilotCLIProxyPort())))
			} else {
				rows = append(rows, statusInfo("Copilot CLI wrapper unwired"))
			}
			if agents.CopilotVSCodeProxyWired() {
				rows = append(rows, statusInfo("Copilot VS Code wrapper wired 127.0.0.1:"+strconv.Itoa(agents.CopilotProxyPort())))
			} else {
				rows = append(rows, statusInfo("Copilot VS Code wrapper unwired"))
			}
		}
		if r.id == "grok" {
			if agents.GrokOAuthProxyWired() {
				rows = append(rows, statusInfo("Grok OAuth wired 127.0.0.1:"+strconv.Itoa(util.GrokOAuthProxyPort())+"/v1"))
			} else {
				rows = append(rows, statusInfo("Grok OAuth unwired"))
			}
		}
		switch r.id {
		case "claude":
			if agents.ClaudeOAuthWired() {
				rows = append(rows, statusOK("OAuth anthropic-native"))
			}
		case "antigravity":
			if agents.AntigravityOAuthWired() {
				rows = append(rows, statusOK("OAuth gemini-native"))
			}
		case "codex":
			if agents.CodexUsesChatGPTAuth() {
				rows = append(rows, statusOK("OAuth ChatGPT"))
			}
		}
	}
	return rows
}

func projectmemProjectStatus(projectDir string) bool {
	return projectDir != "" && util.Exists(filepath.Join(projectDir, ".projectmem"))
}

func doctorMissingTools(tools []*core.ToolManifest, agentID string) []string {
	var missing []string
	for _, tool := range tools {
		if tool.InstructionOnly {
			continue
		}
		verify, ok := tool.VerifyFor[agentID]
		if !ok {
			if tool.ID == "codegraph" || tool.ID == "context-mode" || tool.ID == "projectmem" {
				missing = append(missing, tool.Label)
			}
			continue
		}
		if r := verify(); r != nil && !*r {
			missing = append(missing, tool.Label)
		}
	}
	return missing
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

// agentLabelWidth returns the longest agent label length plus one
// separator space, so every status column aligns across all rows.
func agentLabelWidth() int {
	max := 0
	for _, a := range core.ListAgents() {
		if n := len(a.Label); n > max {
			max = n
		}
	}
	return max + 1
}

func agentTreeRow(r agentReport) string {
	w := agentLabelWidth()
	name := paintName(padEnd(r.label, w))
	return util.C.Green(util.Sym.Check) + " " + name
}

func paintProxyLabel(label, url string) string {
	label = strings.Replace(label, url, util.C.Gray(url), 1)
	if i := strings.IndexByte(label, ' '); i >= 0 {
		return util.C.Cyan(label[:i]) + label[i:]
	}
	return util.C.Cyan(label)
}

func doctorAgentRows(r agentReport) []string {
	w := agentLabelWidth()
	var rows []string

	if !r.installed {
		rows = append(rows, util.C.Gray(util.Sym.Bullet+" ")+util.C.Dim(padEnd(r.label, w))+"not installed")
		return rows
	}

	for _, tool := range core.ListTools() {
		if tool.InstructionOnly {
			continue
		}
		verify, ok := tool.VerifyFor[r.id]
		if !ok {
			if tool.ID == "rtk" {
				rows = append(rows, util.C.Gray("○ ")+tool.Label+util.C.Dim(" — not wired for this harness"))
				continue
			}
			if tool.ID == "codegraph" || tool.ID == "context-mode" || tool.ID == "projectmem" {
				rows = append(rows, util.C.Red("✖")+" "+tool.Label)
			}
			continue
		}
		passed := verify()
		mark := util.C.Red("✖")
		if passed != nil && *passed {
			mark = util.C.Green(util.Sym.Check)
		}
		if tool.ID == "headroom" {
			configured := agents.ProxyAgentWired(r.id)
			providers := agents.BYOKProvidersFor(r.id)
			stale := len(providers) > 0
			nonStale := false
			for _, provider := range providers {
				if !provider.Stale {
					stale = false
					nonStale = true
				}
			}
			if stale {
				rows = append(rows, util.C.Yellow(util.Sym.Warn)+" "+tool.Label+util.C.Yellow(" — BYOK registered but not applied"))
			} else if configured && agents.ProxyReadinessFor(r.id) == agents.ProxyReady && agents.ProxyAgentWired(r.id) || nonStale {
				endpoint := agents.ProxyEndpointFor(r.id)
				if endpoint == "" {
					endpoint = util.HeadroomProxyOpenAIURL()
				}
				port := endpointPort(endpoint)
				label := util.C.Green(util.Sym.Check) + " " + tool.Label
				if port != "" {
					label += util.C.Gray(" → http://127.0.0.1:" + port)
				}
				rows = append(rows, label)
			} else {
				spec, hasSpec := agents.ProxyCapabilities()[r.id]
				if r.id == "grok" {
					rows = append(rows, util.C.Gray("○ ")+tool.Label+util.C.Dim(" — served by its own OAuth lane (28787)"))
				} else if hasSpec && spec.WireKind == agents.ProxyWireManual {
					rows = append(rows, util.C.Gray("○ ")+tool.Label+util.C.Dim(" not supported"))
				} else {
					rows = append(rows, util.C.Gray("○ ")+tool.Label+util.C.Dim(" not configured"))
				}
			}
			continue
		}
		if tool.ID == "rtk" {
			path, label := toolindex.RTKLocationFor(r.id)
			if path == "" {
				rows = append(rows, util.C.Gray("○ ")+tool.Label+util.C.Dim(" — not wired for this harness"))
			} else {
				rows = append(rows, mark+" "+tool.Label+" → "+util.C.Gray(tildePath(path))+util.C.Dim(" ("+label+")"))
			}
			continue
		}

		if tool.ID == "projectmem" {
			if cfgPath, key := toolConfigLocation(r.id, tool.ID); cfgPath != "" && key != "" {
				rows = append(rows, mark+" "+tool.Label+" → "+util.C.Gray(tildePath(cfgPath))+util.C.Dim(" ("+key+")"))
			} else {
				rows = append(rows, mark+" "+tool.Label+" → "+util.C.Dim("configuration location unknown"))
			}
			continue
		}
		if cfgPath, key := toolConfigLocation(r.id, tool.ID); cfgPath != "" && key != "" {
			rows = append(rows, mark+" "+tool.Label+" → "+util.C.Gray(tildePath(cfgPath))+util.C.Dim(" ("+key+")"))
		} else {
			rows = append(rows, mark+" "+tool.Label+" → "+util.C.Dim("configuration location unknown"))
		}
	}

	if cfg := agentConfigFile(r.id); cfg != "" {
		rows = append(rows, util.C.Green(util.Sym.Check)+" config → "+util.C.Gray(tildePath(cfg)))
	}

	if ins := toolindex.InstructionPath(r.id); ins != "" {
		rows = append(rows, util.C.Green(util.Sym.Check)+" instructions → "+util.C.Gray(tildePath(ins)))
	}

	if len(r.runtime) > 0 {
		runtimeBranch := util.C.Yellow("runtime: " + formatRuntimeIssues(r.runtime))
		if len(r.missing) > 0 {
			runtimeBranch += "; " + util.C.Yellow("missing: "+joinComma(r.missing))
		}
		rows = append(rows, runtimeBranch)
	}

	return rows
}

func endpointPort(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return u.Port()
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
	case info.Present:
		return util.C.Green(util.Sym.Check) + " " + name + util.C.Green("installed")
	default:
		return util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(padEnd(tool.ID, 14)) + util.C.Gray("not installed")
	}
}

func listToolVersionLine(tool *core.ToolManifest, info util.VersionInfo, widths ...int) string {
	line := toolVersionDisplayLine(tool, info)
	if line == "" {
		return ""
	}
	path := tildePath(util.InstalledPathFor(tool.ID))
	deps := strings.Join(doctorToolDependencies(tool), ", ")
	if deps == "" {
		deps = "none"
	}
	if len(widths) == 2 {
		line += "  " + padEnd(path, widths[0])
		line += util.C.Dim("  [dependencies: " + padEnd(deps, widths[1]) + "]")
		return line
	}
	if path != "" {
		line += util.C.Gray("  " + path)
	}
	return line + util.C.Dim("  [dependencies: "+deps+"]")
}

func doctorToolDependencies(tool *core.ToolManifest) []string {
	var deps []string
	if toolNeedsNode(tool) {
		if node := util.ResolveNodeBinary(); node == "" {
			deps = append(deps, "Node.js missing")
		} else if tool.MinNodeMajor > 0 && util.NodeMajor() < tool.MinNodeMajor {
			deps = append(deps, fmt.Sprintf("Node.js v%d required", tool.MinNodeMajor))
		} else {
			deps = append(deps, "Node.js OK")
		}
	}
	if tool.NeedsGit {
		if util.Which("git") == "" {
			deps = append(deps, "git missing")
		} else {
			deps = append(deps, "git OK")
		}
	}
	if tool.Channel == core.ChannelUV {
		if util.Which("python3") == "" {
			deps = append(deps, "Python missing")
		} else {
			deps = append(deps, "Python OK")
		}
		if util.Which("uv") == "" {
			deps = append(deps, "uv missing")
		} else {
			deps = append(deps, "uv OK")
		}
	}
	return deps
}

// listToolVersions prints one row per tool.
func listToolVersions(tools []*core.ToolManifest, v map[string]util.VersionInfo, tree bool) {
	nameWidth, versionWidth, pathWidth, depsWidth := 0, 0, 0, 0
	for _, tool := range tools {
		if tool.InstructionOnly || toolVersionDisplayLine(tool, v[tool.ID]) == "" {
			continue
		}
		if len(tool.ID) > nameWidth {
			nameWidth = len(tool.ID)
		}
		info := v[tool.ID]
		version := ""
		if info.Installed != nil {
			version = "v" + *info.Installed
			if info.Latest != nil {
				version += " → v" + *info.Latest
			}
		}
		if len(version) > versionWidth {
			versionWidth = len(version)
		}
		if path := util.InstalledPathFor(tool.ID); len(tildePath(path)) > pathWidth {
			pathWidth = len(tildePath(path))
		}
		if width := len(strings.Join(doctorToolDependencies(tool), ", ")); width > depsWidth {
			depsWidth = width
		}
	}
	for _, tool := range tools {
		if tool.InstructionOnly {
			continue
		}
		line := listToolVersionLine(tool, v[tool.ID], nameWidth, versionWidth, pathWidth, depsWidth)
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
