package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// parseRunMcpArgv splits our flags from the command to launch. Doctor shares
// it, so what it accepts and what runs can't drift apart. --root-cwd fills in
// the project path from the working dir the agent starts us in.
func parseRunMcpArgv(argv []string) (agent string, contextMode, rootCwd bool, rest []string, ok bool) {
	if len(argv) >= 2 && argv[0] == "--agent" {
		agent, argv = argv[1], argv[2:]
	}
	if len(argv) > 0 && argv[0] == "--context-mode" {
		contextMode, argv = true, argv[1:]
	}
	if len(argv) > 0 && argv[0] == "--root-cwd" {
		rootCwd, argv = true, argv[1:]
	}
	// What's left has to be a command. A leftover flag means the entry was
	// written wrong — running it would just exec the flag.
	if len(argv) == 0 || strings.HasPrefix(argv[0], "--") {
		return "", false, false, nil, false
	}
	return agent, contextMode, rootCwd, argv, true
}

func RunMcp(argv []string) int {
	agent, contextMode, rootCwd, argv, ok := parseRunMcpArgv(argv)
	if !ok {
		return 1
	}
	if rootCwd {
		if wd, err := os.Getwd(); err == nil && wd != "" {
			argv = append(argv, "--root", wd)
		}
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

func isCodegraphCommand(p string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(p, "\\", "/")))
	return base == "codegraph" || base == "codegraph.cmd" || base == "codegraph.exe" || base == "codegraph.bat"
}
