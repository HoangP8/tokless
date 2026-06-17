package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

var projectMarkers = []string{".git", "package.json", "go.mod", "Cargo.toml", "pyproject.toml", "pom.xml", "build.gradle", "tsconfig.json", "requirements.txt"}

func looksLikeProject(dir string) bool {
	for _, m := range projectMarkers {
		if util.Exists(filepath.Join(dir, m)) {
			return true
		}
	}
	return false
}

func RunIndex(opts InitOptions, auto bool) int {
	dir, err := os.Getwd()
	if err != nil {
		if !auto {
			util.L.Err("cannot resolve current directory: " + err.Error())
		}
		return 1
	}

	if auto && !looksLikeProject(dir) {
		return 0
	}

	var indexable []*core.ToolManifest
	for _, t := range core.ListTools() {
		if t.IndexProject != nil {
			indexable = append(indexable, t)
		}
	}

	if !auto {
		util.L.Raw("")
		util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless index")) + util.C.Gray("  build per-project indexes in "+dir))
		util.L.Raw("")
	}

	if len(indexable) == 0 {
		if !auto {
			util.L.Raw("  " + util.C.Gray("no tools need a per-project index."))
			util.L.Raw("")
		}
		return 0
	}

	ro := core.RunOpts{DryRun: opts.DryRun, Agent: opts.Agent}
	failed := 0
	for _, t := range indexable {
		if t.Indexed != nil && t.Indexed(dir, ro) {
			if !auto {
				util.L.Raw("  " + util.C.Green("✔ ") + t.Label + util.C.Gray("  already indexed"))
			}
			continue
		}
		if t.IndexReady != nil && !t.IndexReady() {
			if !auto {
				util.L.Raw("  " + util.C.Gray("• ") + t.Label + util.C.Gray("  not installed — run tokless first"))
				failed++
			}
			continue
		}
		ok, ierr := t.IndexProject(dir, ro)
		if auto {
			continue
		}
		switch {
		case ierr != nil:
			util.L.Raw("  " + util.C.Red("✖ ") + t.Label + util.C.Gray("  "+ierr.Error()))
			failed++
		case ok:
			util.L.Raw("  " + util.C.Green("✔ ") + t.Label + util.C.Gray("  indexed"))
		default:
			util.L.Raw("  " + util.C.Yellow("! ") + t.Label + util.C.Gray("  could not index"))
			failed++
		}
	}

	if auto {
		return 0
	}

	util.L.Raw("")
	if failed == 0 {
		util.L.Raw("  " + util.C.Green("✔ Project indexed."))
	} else {
		util.L.Raw("  " + util.C.Yellow("⚠ ") + "Some tools could not index.")
	}
	util.L.Raw("")
	if failed > 0 {
		return 1
	}
	return 0
}

// RunCodegraphIndexHook handles `tokless agy-hook codegraph-index [--sync]`.
func RunCodegraphIndexHook() int {
	dir, err := os.Getwd()
	if err != nil {
		return 0
	}
	if util.Exists(filepath.Join(dir, ".codegraph")) {
		return 0
	}
	bin := util.Which("codegraph")
	if bin == "" {
		if matches, _ := filepath.Glob(filepath.Join(util.Home(), ".nvm", "versions", "node", "*", "bin")); len(matches) > 0 {
			for _, d := range matches {
				util.PrependProcessPath(d)
			}
		}
		bin = util.Which("codegraph")
	}
	if bin == "" {
		return 0
	}
	if len(os.Args) > 3 && os.Args[3] == "--sync" {
		cmd := exec.Command(bin, "init", "-i")
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		return 0
	}
	cmd := exec.Command(bin, "init", "-i")
	cmd.Dir = dir
	backgroundSpawn(cmd)
	return 0
}

// RunContextModeWarmup starts the context-mode MCP server if not already running.
func RunContextModeWarmup() int {
	if contextModeSentinelAlive() {
		return 0
	}
		spawn := util.PickMcpSpawn("context-mode", "serve", "--mcp")
		cmd := exec.Command(spawn.Command, spawn.Args...)
		backgroundSpawn(cmd)
		return 0
}

func contextModeSentinelAlive() bool {
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return false
	}
	const prefix = "context-mode-mcp-ready-"
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, e.Name()))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		if processAlive(pid) {
			return true
		}
	}
	return false
}
