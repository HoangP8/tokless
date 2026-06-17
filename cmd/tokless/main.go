package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/commands"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

type parsedArgs struct {
	cmd   string
	flags map[string]string
	bools map[string]bool
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
				p.bools[a[2:]] = true
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
	return util.C.Bold(util.C.Cyan("tokless")) + " — token-saving for AI coding agents (Claude Code, OpenCode, Codex)\n\n" +
		util.C.Bold("Usage:") + "\n" +
		"  " + cy("tokless") + "              Install + wire everything (default; safe to re-run)\n" +
		"  " + cy("tokless update") + "       Show version diff and upgrade the 4 tools\n" +
		"  " + cy("tokless doctor") + "       Show what's wired up; warn about anything broken\n" +
		"  " + cy("tokless index") + "        Build per-project indexes (codegraph) in the current dir\n" +
		"  " + cy("tokless uninstall") + "    Remove everything tokless ever touched\n\n" +
		util.C.Bold("Flags:") + "\n" +
		"  --agents <list>     Limit to a subset: claude,opencode,codex\n" +
		"  --tools <list>      Limit to a subset: rtk,caveman,codegraph,context-mode\n" +
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
	return items, nil
}

func main() {
	code := run()
	util.RestoreConsoleCP()
	os.Exit(code)
}

func run() int {
	agents.Register()
	tools.Register()
	util.EnsureProcessPath()

	if len(os.Args) >= 3 && os.Args[1] == "run-mcp" {
		return commands.RunMcp(os.Args[2:])
	}
	if len(os.Args) >= 3 && os.Args[1] == "rtk-hook" {
		switch os.Args[2] {
		case "agy":
			return commands.RunRtkHook()
		case "codex":
			return commands.RunRtkHookCodex()
		}
	}
	if len(os.Args) >= 4 && os.Args[1] == "context-mode-hook" && os.Args[2] == "agy" && os.Args[3] == "preinvocation" {
		return commands.RunContextModePreInvocationAgy()
	}
	if len(os.Args) >= 4 && os.Args[1] == "context-mode-hook" && os.Args[2] == "agy" && os.Args[3] == "pretooluse" {
		return commands.RunContextModePreToolUseAgy()
	}
	if len(os.Args) >= 3 && os.Args[1] == "agy-hook" && os.Args[2] == "codegraph-index" {
		return commands.RunCodegraphIndexHook()
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

	command := p.cmd
	if command == "" {
		command = "init"
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
