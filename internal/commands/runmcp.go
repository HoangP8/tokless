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
	if len(argv) >= 2 && argv[0] == "--agent" {
		agent = argv[1]
		argv = argv[2:]
	}
	contextMode := false
	if len(argv) > 0 && argv[0] == "--context-mode" {
		contextMode = true
		argv = argv[1:]
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
		_ = RunCodegraphAutoIndex()
		argv = injectCodegraphPath(argv)
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		path = argv[0]
	}
	return runMcpProxy(agent, path, argv, os.Environ(), contextMode)
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
func injectCodegraphPath(argv []string) []string {
	for _, a := range argv {
		if a == "--path" || strings.HasPrefix(a, "--path=") || a == "-p" || strings.HasPrefix(a, "-p/") {
			return argv
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return argv
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
