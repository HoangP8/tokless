package commands

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

// proxyLaneRow renders a per-agent proxy lane row; unwired lanes stay
func proxyLaneRow(label string, wired bool, addr string) string {
	if wired {
		return label + " " + util.C.Gray(":") + paintPath(addr)
	}
	return label + util.C.Dim(" (unwired "+addr+")")
}

// RunInfo prints install provenance and paths.
func RunInfo() int {
	cmdHeader("info", "install + paths")

	rec, exact := util.InstallInfo()
	util.TreeTop("Tokless")
	util.TreeLeaf(paintKey(padEnd("version", 10)) + util.C.Bold(paintVer(util.ToklessVersion())))
	util.TreeLeaf(paintKey(padEnd("binary", 10)) + paintPath(tildePath(util.ToklessAbs())))
	util.TreeLeaf(paintKey(padEnd("install", 10)) + formatInstallMethod(rec, exact))
	util.TreeLeaf(paintKey(padEnd("platform", 10)) + util.C.Blue(runtime.GOOS+"/"+runtime.GOARCH))
	util.TreeClose()

	util.TreeCorner("Agents")
	nAgents := 0
	for _, agent := range core.ListAgents() {
		if !agent.Detect().Installed {
			continue
		}
		nAgents++
		util.TreeLeaf(util.C.Green(util.Sym.Check) + " " + paintName(agent.Label))
		var rows []string
		if ep := agents.ProxyEndpointFor(agent.ID); ep != "" {
			rows = append(rows, "headroom "+util.C.Gray("→ ")+paintPath(ep))
		}
		if routes := util.BYOKRoutesFor(agent.ID); len(routes) > 0 {
			var ep string
			if headroomEp := agents.ProxyEndpointFor(agent.ID); headroomEp == "" {
				switch agent.ID {
				case "claude", "antigravity":
					ep = util.HeadroomProxyURL()
				default:
					ep = util.HeadroomProxyOpenAIURL()
				}
			} else {
				ep = headroomEp
			}
			var providerNames []string
			for _, r := range routes {
				if parts := strings.SplitN(r.ID, ":", 2); len(parts) == 2 {
					providerNames = append(providerNames, parts[1])
				}
			}
			var providerDisplay string
			if len(providerNames) > 4 {
				providerDisplay = strings.Join(providerNames[:3], ", ") + fmt.Sprintf(" +%d more", len(providerNames)-3)
			} else {
				providerDisplay = strings.Join(providerNames, ", ")
			}
			rows = append(rows, "byok "+util.C.Gray("→ ")+paintPath(ep)+util.C.Gray("  ")+util.C.Dim(providerDisplay))
		}
		switch agent.ID {
		case "claude":
			rows = append(rows, proxyLaneRow("claude oauth", agents.ClaudeOAuthWired(), "anthropic-native"))
		case "antigravity":
			rows = append(rows, proxyLaneRow("antigravity oauth", agents.AntigravityOAuthWired(), "gemini-native"))
		case "codex":
			rows = append(rows, proxyLaneRow("codex oauth", agents.CodexUsesChatGPTAuth(), "chatgpt"))
		case "copilot":
			rows = append(rows, proxyLaneRow("cli wrapper", agents.CopilotCLIProxyWired(), "127.0.0.1:"+strconv.Itoa(agents.CopilotCLIProxyPort())))
			rows = append(rows, proxyLaneRow("vscode wrapper", agents.CopilotVSCodeProxyWired(), "127.0.0.1:"+strconv.Itoa(agents.CopilotProxyPort())))
		case "grok":
			rows = append(rows, proxyLaneRow("grok oauth", agents.GrokOAuthProxyWired(), "127.0.0.1:"+strconv.Itoa(util.GrokOAuthProxyPort())+"/v1"))
		}
		if cfg := agentConfigFile(agent.ID); cfg != "" {
			rows = append(rows, "config "+util.C.Gray("→ ")+paintPath(tildePath(cfg)))
		}
		if ins := tools.InstructionPath(agent.ID); ins != "" {
			rows = append(rows, "instructions "+util.C.Gray("→ ")+paintPath(tildePath(ins)))
		}
		for i, row := range rows {
			if i == len(rows)-1 {
				util.TreeChildLast(row)
			} else {
				util.TreeChildMid(row)
			}
		}
	}
	if nAgents == 0 {
		util.TreeLeaf(util.C.Dim("none detected on this machine"))
	}
	util.TreeClose()

	util.TreeCorner("Tools")
	for _, tool := range core.ListTools() {
		if tool.InstructionOnly {
			continue
		}
		if v := util.InstalledVersionFor(tool.ID); v != nil {
			row := paintName(padEnd(tool.ID, 14)) + paintVer(padEnd("v"+*v, 10))
			if p := util.InstalledPathFor(tool.ID); p != "" {
				row += paintPath(tildePath(p))
			}
			util.TreeLeaf(util.C.Green(util.Sym.Check) + " " + row)
		} else {
			util.TreeLeaf(util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(padEnd(tool.ID, 14)) + util.C.Gray("not installed"))
		}
	}
	util.TreeClose()

	util.TreeCorner("State")
	for _, item := range []struct {
		label string
		path  string
	}{
		{"install marker", util.InstallMarkerPath()},
		{"version cache", util.VersionCachePath()},
	} {
		if util.Exists(item.path) {
			util.TreeLeaf(util.C.Green(util.Sym.Check) + " " + paintName(padEnd(item.label, 16)) + paintPath(tildePath(item.path)))
		} else {
			util.TreeLeaf(util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(padEnd(item.label, 16)) + paintPath(tildePath(item.path)) + util.C.Yellow(" missing"))
		}
	}
	util.TreeClose()

	treeStatus(statusKV(
		util.C.Cyan(util.Sym.Info),
		"tip",
		util.C.Gray("run ")+paintCmd("tokless doctor")+util.C.Gray(" for health + updates"),
	))
	printRepoFooter(true)
	util.L.Raw("")
	return 0
}

// agentConfigFile returns the agent's main config/settings file for display.
func agentConfigFile(id string) string {
	switch id {
	case "claude":
		return util.ClaudeCodePaths().Settings
	case "opencode":
		return util.OpenCodePathsResolved().Config
	case "opencode2":
		return util.OpenCodePathsV2().Config
	case "codex":
		return util.CodexPathsResolved().Config
	case "antigravity":
		return util.AntigravityPathsResolved().Settings
	case "copilot":
		return util.CopilotPathsResolved().Settings
	case "droid":
		return filepath.Join(util.Home(), ".factory", "settings.json")
	case "pi":
		return agents.PiSettingsFile()
	case "omp":
		return agents.OmpConfigFile()
	case "grok":
		return agents.GrokConfigFile()
	case "kilo":
		return util.KiloPathsResolved().Config
	case "cline":
		return util.ClinePathsResolved().McpConfig
	case "kimi":
		return util.KimiPathsResolved().Config
	case "zcode":
		return util.ZcodePathsResolved().Config
	case "dsh":
		return agents.DshPatchFile()
	case "amp":
		return util.AmpPathsResolved().Config
	case "cursor":
		return util.CursorPathsResolved().McpConfig
	}
	return ""
}

func formatInstallMethod(rec util.InstallRecord, exact bool) string {
	method := rec.Method
	if method == "" {
		method = "unknown"
	}
	if !exact {
		return util.C.Yellow(method) + util.C.Gray("  (guessed from path)")
	}
	out := util.C.Green(util.C.Bold(method))
	if rec.At != "" {
		if t, err := time.Parse(time.RFC3339, rec.At); err == nil {
			out += " " + paintVer(t.UTC().Format("2006-01-02"))
		} else if i := strings.Index(rec.At, "T"); i != -1 {
			out += " " + paintVer(rec.At[:i])
		}
	}
	return out
}

// tildePath shortens home-relative paths for display.
func tildePath(p string) string {
	if p == "" {
		return ""
	}
	display := filepath.ToSlash(p)
	home := util.Home()
	if home == "" {
		return display
	}
	homeSlash := filepath.ToSlash(home)
	if util.IsWin {
		if strings.HasPrefix(strings.ToLower(display), strings.ToLower(homeSlash)) {
			return "~" + display[len(homeSlash):]
		}
		return display
	}
	if strings.HasPrefix(display, homeSlash) {
		return "~" + display[len(homeSlash):]
	}
	return display
}
