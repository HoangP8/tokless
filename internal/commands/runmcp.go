package commands

import (
	"os"
	"os/exec"

	"github.com/HoangP8/tokless/internal/util"
)

func RunMcp(argv []string) int {
	if len(argv) == 0 {
		return 1
	}
	util.EnsureProcessPath()
	if self, err := os.Executable(); err == nil {
		idx := exec.Command(self, "index", "--auto")
		idx.Stdin, idx.Stdout, idx.Stderr = nil, nil, nil
		detachMcpChild(idx)
		_ = idx.Start()
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		path = argv[0]
	}
	return handoffMcp(path, argv, os.Environ())
}
