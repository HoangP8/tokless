package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	if len(argv) == 0 {
		return 1
	}
	util.EnsureProcessPath()
	if strings.Contains(argv[0], string(filepath.Separator)) {
		util.PrependProcessPath(filepath.Dir(argv[0]))
	}
	codegraphPath := codegraphMcpCommand(argv)
	if codegraphPath != "" && codegraphPath == argv[0] && !strings.Contains(argv[0], string(filepath.Separator)) && !util.CodegraphBinaryHealthy(argv[0]) {
		if p := util.ResolveCodegraphBin(); p != "" {
			argv[0] = p
		}
	}
	if codegraphPath != "" {
		argv = injectCodegraphPath(argv, workspace)
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		path = argv[0]
	}
	var afterStart func()
	if codegraphPath != "" {
		afterStart = func() {
			go func() { _ = RunCodegraphMcpBootstrap(workspace) }()
		}
	}
	return runMcpProxyAfterStart(agent, path, argv, os.Environ(), tool, afterStart)
}

// codegraphMcpCommand returns CodeGraph executable for direct and cmd /c forms.
func codegraphMcpCommand(argv []string) string {
	if len(argv) > 0 && isCodegraphCommand(argv[0]) {
		return argv[0]
	}
	if len(argv) < 3 {
		return ""
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(argv[0], "\\", "/")))
	if (base == "cmd" || base == "cmd.exe") && strings.EqualFold(argv[1], "/c") && isCodegraphCommand(argv[2]) {
		return argv[2]
	}
	return ""
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
