package commands

import (
	"os"
	"os/exec"

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
	RunIndex(InitOptions{Agent: agent}, true)
	path, err := exec.LookPath(argv[0])
	if err != nil {
		path = argv[0]
	}
	return handoffMcp(path, argv, os.Environ())
}
