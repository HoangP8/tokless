package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/HoangP8/tokless/internal/core"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

func RunMcp(argv []string) int {
	agent := ""
	workspace := ""
	contextMode := false
	tool := ""
	for len(argv) > 0 {
		switch argv[0] {
		case "--agent", "--workspace", "--tool":
			if len(argv) < 2 || argv[1] == "" || strings.HasPrefix(argv[1], "--") {
				return 1
			}
			switch argv[0] {
			case "--agent":
				agent = argv[1]
			case "--workspace":
				workspace = argv[1]
			case "--tool":
				tool = argv[1]
			}
			argv = argv[2:]
		case "--context-mode":
			contextMode = true
			argv = argv[1:]
		default:
			goto command
		}
	}

command:
	if contextMode && tool == "" {
		tool = "context-mode"
	}
	if tool == "headroom" {
		return 2
	}
	if len(argv) == 0 {
		return 1
	}
	util.EnsureProcessPath()
	if strings.Contains(argv[0], string(filepath.Separator)) {
		util.PrependProcessPath(filepath.Dir(argv[0]))
	}
	codegraphPath := codegraphMcpCommand(argv)
	if codegraphPath != "" && isCodegraphCommand(argv[0]) && !strings.Contains(argv[0], string(filepath.Separator)) && !util.CodegraphBinaryHealthy(argv[0]) {
		if p := util.ResolveCodegraphBin(); p != "" {
			argv[0] = p
		}
	}
	if codegraphPath != "" {
		if root := codegraphWorkspace(workspace); root != "" {
			if ok, err := tools.EnsureCodegraphIndex(root, core.RunOpts{}); err != nil || !ok {
				if err != nil {
					util.L.Err("CodeGraph index: " + err.Error())
				}
				return 1
			}
		}
		argv = injectCodegraphPath(argv, workspace)
	}
	var projectmemEnv []string
	if tool == "projectmem" {
		if util.Which("pjm-mcp") == "" {
			util.L.Err("projectmem: pjm-mcp not found; install with: uv tool install projectmem")
			return 1
		}
		root := projectmemRoot(workspace)
		if err := tools.EnsureProjectmemProject(root); err != nil {
			util.L.Err("projectmem init: " + err.Error())
		}
		projectmemEnv = []string{"PROJECTMEM_ROOT=" + root}
		tools.RefreshProjectmemRuleFiles(root)
		defer tools.RefreshProjectmemRuleFiles(root)
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-stop
			tools.RefreshProjectmemRuleFiles(root)
			os.Exit(0)
		}()
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return 1
	}
	if err := ensureProxyForAgent(agent); err != nil {
		util.L.Err("headroom proxy: " + err.Error())
		return 1
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "PROJECTMEM_ROOT=") {
			env = append(env, e)
		}
	}
	env = append(env, projectmemEnv...)
	return runMcpProxyAfterStart(agent, path, argv, env, tool, nil)
}

// projectmemRoot pins the projectmem project root for the MCP child.
func projectmemRoot(workspace string) string {
	if root := codegraphWorkspace(workspace); root != "" {
		return root
	}
	if workspace != "" && workspace != "${workspaceFolder}" {
		return workspace
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// ensureProxyForAgent silently starts the headroom daemon when an MCP server is
// being booted for an agent tokless wires to the proxy.
var ensureProxyUp = headroompkg.EnsureProxyUp

func ensureProxyForAgent(agent string) error {
	if agent == "" || proxyInstructions(agent) != nil {
		return nil
	}
	for _, id := range ProxyAgentIDs() {
		if agent == id {
			return ensureProxyUp()
		}
	}
	return nil
}

func codegraphWorkspace(workspace string) string {
	if workspace == "${workspaceFolder}" {
		workspace = ""
	}
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	root := findProjectDir(workspace)
	if !looksLikeProject(root) {
		return ""
	}
	return root
}

// codegraphMcpCommand returns CodeGraph executable for direct and cmd /c forms.
func codegraphMcpCommand(argv []string) string {
	if len(argv) > 0 && isCodegraphCommand(argv[0]) {
		return argv[0]
	}
	if isCodegraphNpxCommand(argv) {
		return argv[0]
	}
	if len(argv) < 3 {
		return ""
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(argv[0], "\\", "/")))
	if (base == "cmd" || base == "cmd.exe") && strings.EqualFold(argv[1], "/c") && (isCodegraphCommand(argv[2]) || isCodegraphNpxCommand(argv[2:])) {
		return argv[2]
	}
	return ""
}

func isCodegraphNpxCommand(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(argv[0], "\\", "/")))
	return (base == "npx" || base == "npx.cmd" || base == "npx.exe") && argv[1] == "--no-install" && argv[2] == "@colbymchenry/codegraph"
}

// injectCodegraphPath pins the CodeGraph project root with --path when the MCP
// transport's cwd is not the project.
func injectCodegraphPath(argv []string, workspace ...string) []string {
	for _, a := range argv {
		if a == "--path" || strings.HasPrefix(a, "--path=") || a == "-p" || strings.HasPrefix(a, "-p/") {
			return argv
		}
	}
	dir := ""
	if len(workspace) > 0 {
		dir = workspace[0]
	}
	if dir == "${workspaceFolder}" {
		dir = ""
	}
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return argv
		}
	}
	dir = findProjectDir(dir)
	if !looksLikeProject(dir) {
		return argv
	}
	out := make([]string, 0, len(argv)+2)
	out = append(out, argv...)
	return append(out, "--path", dir)
}

func isCodegraphCommand(p string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(p, "\\", "/")))
	return base == "codegraph" || base == "codegraph.cmd" || base == "codegraph.exe" || base == "codegraph.bat"
}

// RunProjectmemHook prints session-start project state in each agent's hook format.
func RunProjectmemHook(format string) int {
	input, _ := io.ReadAll(os.Stdin)
	text := tools.ProjectmemSessionText(resolveHookProjectDirFromInput(input))
	if text == "" {
		return 0
	}
	switch format {
	case "claude":
		out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "SessionStart", "additionalContext": text,
		}})
		fmt.Println(string(out))
	case "agy":
		out, _ := json.Marshal(map[string]any{"injectSteps": []any{map[string]any{
			"systemMessage": map[string]any{"systemMessage": text},
		}}})
		fmt.Println(string(out))
	case "flat":
		out, _ := json.Marshal(map[string]any{"additionalContext": text})
		fmt.Println(string(out))
	default:
		fmt.Println(text)
	}
	return 0
}
