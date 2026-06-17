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
	if agent != "antigravity" {
		RunIndex(InitOptions{Agent: agent}, true)
	} else if dir, err := os.Getwd(); err == nil && util.Exists(filepath.Join(dir, ".codegraph")) {
		RunIndex(InitOptions{Agent: agent}, true)
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		path = argv[0]
	}
	return handoffMcp(path, argv, os.Environ())
}
