package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/commands"
	"github.com/HoangP8/tokless/internal/core"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

type parsedArgs struct {
	cmd   string
	flags map[string]string
	bools map[string]bool
}

var (
	registerAgents    = agents.Register
	registerTools     = tools.Register
	ensureProcessPath = util.EnsureProcessPath
)

// ensureSessionBoot is the hook-path readiness gate. Swapped in tests.
var ensureSessionBoot = headroompkg.EnsureProxyUp

// isSessionBootArg reports whether argv names an agent-session entry point
// (run-mcp, rtk-hook variants, codex-perm, codegraph-index hooks, cursor
// project-rules) that may carry agent traffic through the headroom proxy.
func isSessionBootArg(args []string) bool {
	switch {
	case len(args) >= 2 && args[0] == "run-mcp",
		len(args) >= 2 && args[0] == "rtk-hook",
		len(args) >= 3 && args[0] == "rtk" && args[1] == "hook",
		len(args) >= 2 && args[0] == "codex-perm",
		len(args) >= 2 && (args[0] == "agy-hook" || args[0] == "cursor-hook" || args[0] == "copilot-hook"):
		return true
	}
	return false
}

// parseArgs mirrors the TS parser: --k=v, --k v, --k, -x short bool.
func parseArgs(argv []string) parsedArgs {
	p := parsedArgs{flags: map[string]string{}, bools: map[string]bool{}}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if strings.HasPrefix(a, "--") {
			if eq := strings.IndexByte(a, '='); eq != -1 {
				p.flags[a[2:eq]] = a[eq+1:]
			} else if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				p.flags[a[2:]] = argv[i+1]
				i++
			} else {
				if a[2:] == "agents" || a[2:] == "tools" || a[2:] == "agent" {
					p.flags[a[2:]] = ""
				} else {
					p.bools[a[2:]] = true
				}
			}
		} else if strings.HasPrefix(a, "-") && len(a) == 2 {
			p.bools[a[1:]] = true
		} else if p.cmd == "" {
			p.cmd = a
		}
	}
	return p
}

func helpText() string {
	cy := util.C.Cyan
	return util.C.Bold(util.C.Cyan("tokless")) + " — token-saving for AI coding agents (Claude Code, OpenCode, Codex, Antigravity, GitHub Copilot, Factory Droid, Grok, Pi, Oh My Pi, Cline)\n\n" +
		util.C.Bold("Usage:") + "\n" +
		"  " + cy("tokless") + "              Install + wire everything (default; safe to re-run)\n" +
		"  " + cy("tokless update") + "       Update the tokless CLI, then show version diff and upgrade tools\n" +
		"  " + cy("tokless doctor") + "       Show what's wired up; warn about anything broken\n" +
		"  " + cy("tokless info") + "         Show how tokless was installed, plus paths and config locations\n" +
		"  " + cy("tokless index") + "        Build per-project indexes (codegraph) in the current dir\n" +
		"  " + cy("tokless uninstall") + "    Remove everything tokless ever touched\n" +
		"  " + cy("tokless proxy") + "      Manage the headroom HTTP proxy: up|down|status\n\n" +
		util.C.Bold("Flags:") + "\n" +
		"  --agents <list>     Limit to a subset: claude,opencode,codex,antigravity,copilot,droid,grok,pi,omp,kilo,cline,cursor\n" +
		"  --tools <list>      Limit to a subset: rtk,caveman,ponytail,codegraph,context-mode,headroom\n" +
		"  --dry-run           Show what would change without writing anything\n" +
		"  --verbose           Show every step\n\n" +
		util.C.Gray("Docs: https://github.com/HoangP8/tokless")
}

func parseList(raw string, ok bool, allowed []string) ([]string, error) {
	if !ok {
		return nil, nil
	}
	var items []string
	var invalid []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		items = append(items, s)
		found := false
		for _, a := range allowed {
			if a == s {
				found = true
				break
			}
		}
		if !found {
			invalid = append(invalid, s)
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("Invalid value(s): %s. Allowed: %s", strings.Join(invalid, ", "), strings.Join(allowed, ", "))
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("value must not be empty")
	}
	return items, nil
}

// proxyHelpText prints usage for `tokless proxy <subcommand>`.
func proxyHelpText() string {
	cy := util.C.Cyan
	return util.C.Bold(util.C.Cyan("tokless proxy")) + " — headroom HTTP-proxy daemon (cache mode)\n\n" +
		util.C.Bold("Usage:") + "\n" +
		"  " + cy("tokless proxy up") + "       Start the proxy daemon and point agents at it\n" +
		"  " + cy("tokless proxy down") + "     Unwire agents and stop the proxy daemon\n" +
		"  " + cy("tokless proxy status") + "   Show daemon + per-agent wiring state\n\n" +
		util.C.Bold("Flags:") + "\n" +
		"  --agent <list>     Limit to: claude,codex,opencode,omp,kilo,pi,droid,grok,copilot,cline,cursor,antigravity (default: all)\n" +
		"  --help             Show this help\n"
}

// runProxyCli dispatches `tokless proxy up|down|status [--agent ...]`.
func runProxyCli(argv []string) int {
	p := parseArgs(argv)
	if p.bools["verbose"] {
		util.SetVerbose(true)
	}
	if p.bools["help"] || p.bools["h"] || p.cmd == "help" {
		fmt.Println(proxyHelpText())
		return 0
	}
	agentRaw, agentOK := p.flags["agent"]
	agentList, err := parseList(agentRaw, agentOK, commands.ProxyAgentIDs())
	if err != nil {
		util.L.Err(err.Error())
		return 2
	}
	opts := commands.InitOptions{Agents: agentList}
	switch p.cmd {
	case "up":
		return commands.RunProxyUp(opts)
	case "down":
		return commands.RunProxyDown(opts)
	case "status":
		return commands.RunProxyStatus(opts)
	}
	util.L.Err("Unknown proxy subcommand: " + p.cmd)
	fmt.Println(proxyHelpText())
	return 1
}

func main() {
	code := run()
	util.RestoreConsoleCP()
	os.Exit(code)
}

func run() int {
	registerAgents()
	registerTools()
	ensureProcessPath()
	if isSessionBootArg(os.Args[1:]) {
		ensureSessionBoot()
	}
	if len(os.Args) >= 3 && os.Args[1] == "run-mcp" {
		return commands.RunMcp(os.Args[2:])
	}
	if len(os.Args) >= 3 && os.Args[1] == "rtk-hook" {
		switch os.Args[2] {
		case "agy":
			return commands.RunRtkHook()
		case "codex", "claude", "omp":
			return commands.RunRtkHookCodex()
		case "copilot":
			return commands.RunRtkHookCopilot()
		case "droid":
			return commands.RunRtkHookDroid()
		case "cline":
			return commands.RunRtkHookCline()
		}
	}
	if len(os.Args) >= 4 && os.Args[1] == "rtk" && os.Args[2] == "hook" && os.Args[3] == "cursor" {
		return commands.RunRtkHookCursor()
	}
	if len(os.Args) >= 3 && os.Args[1] == "codex-perm" && os.Args[2] == "codex" {
		return commands.RunCodexPermHook()
	}
	if len(os.Args) >= 3 && os.Args[1] == "agy-hook" && os.Args[2] == "codegraph-index" {
		return commands.RunCodegraphIndexHook()
	}
	if len(os.Args) >= 3 && os.Args[1] == "cursor-hook" && os.Args[2] == "codegraph-index" {
		return commands.RunCodegraphIndexHook()
	}
	if len(os.Args) >= 3 && os.Args[1] == "cursor-hook" && os.Args[2] == "project-rules" {
		return commands.RunCursorProjectRulesHook()
	}
	if len(os.Args) >= 3 && os.Args[1] == "copilot-hook" && os.Args[2] == "codegraph-index" {
		return commands.RunCodegraphIndexHook()
	}
	if len(os.Args) >= 2 && os.Args[1] == "proxy" {
		return runProxyCli(os.Args[2:])
	}

	p := parseArgs(os.Args[1:])
	if p.bools["verbose"] {
		util.SetVerbose(true)
	}

	if p.bools["version"] || p.bools["v"] || p.cmd == "version" {
		fmt.Println(util.ToklessVersion())
		return 0
	}
	if p.bools["help"] || p.cmd == "help" {
		fmt.Println(helpText())
		return 0
	}

	agentRaw, agentOK := p.flags["agents"]
	toolRaw, toolOK := p.flags["tools"]
	agentList, err := parseList(agentRaw, agentOK, core.AgentIDs())
	if err != nil {
		util.L.Err(err.Error())
		return 2
	}
	toolList, err := parseList(toolRaw, toolOK, core.ToolIDs())
	if err != nil {
		util.L.Err(err.Error())
		return 2
	}

	command := p.cmd
	if command == "" {
		command = "init"
	}

	opts := commands.InitOptions{
		Agents:  agentList,
		Tools:   toolList,
		Agent:   strings.ToLower(strings.TrimSpace(p.flags["agent"])),
		Yes:     p.bools["yes"],
		DryRun:  p.bools["dry-run"] || p.bools["dryrun"],
		Verbose: p.bools["verbose"],
	}

	var code int
	switch command {
	case "init":
		code = commands.RunInit(opts)
	case "update":
		code = commands.RunUpdate(opts)
	case "doctor":
		code = commands.RunDoctor(p.bools["offline"])
	case "info":
		code = commands.RunInfo()
	case "index":
		code = commands.RunIndex(opts, p.bools["auto"])
	case "disable":
		code = commands.RunDisable(opts)
	case "uninstall":
		code = commands.RunUninstall(opts)
	case "self-update":
		code = commands.RunSelfUpdate()
	default:
		fmt.Println(helpText())
		util.L.Err("Unknown command: " + command)
		code = 1
	}
	return code
}
