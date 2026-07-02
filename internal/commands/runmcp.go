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
	if len(argv) == 0 {
		return 1
	}
	util.EnsureProcessPath()
	if strings.Contains(argv[0], string(filepath.Separator)) {
		util.PrependProcessPath(filepath.Dir(argv[0]))
	}
	if isCodegraphCommand(argv[0]) && !util.CodegraphBinaryHealthy(argv[0]) {
		if p := util.ResolveCodegraphBin(); p != "" {
			argv[0] = p
		}
	}
	RunIndex(InitOptions{Agent: agent}, true)
	path, err := exec.LookPath(argv[0])
	if err != nil {
		path = argv[0]
	}
	return runMcpProxy(agent, path, argv, os.Environ())
}

func isCodegraphCommand(p string) bool {
	base := strings.ToLower(filepath.Base(p))
	return base == "codegraph" || base == "codegraph.cmd" || base == "codegraph.exe" || base == "codegraph.bat"
}
