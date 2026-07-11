package commands

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// findProjectDir walks up from dir looking for project markers.
func findProjectDir(dir string) string {
	for {
		if looksLikeProject(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "." {
			break
		}
		dir = parent
	}
	return dir
}

func RunIndex(opts InitOptions, auto bool) int {
	dir, err := os.Getwd()
	if err != nil {
		if !auto {
			util.L.Err("cannot resolve current directory: " + err.Error())
		}
		return 1
	}

	if auto {
		dir = findProjectDir(dir)
		if !looksLikeProject(dir) {
			return 0
		}
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

// RunCodegraphIndexHook handles `tokless agy-hook codegraph-index`.
func RunCodegraphIndexHook() int {
	input, _ := io.ReadAll(os.Stdin)
	dir := resolveHookProjectDirFromInput(input)
	if dir == "" {
		return 0
	}
	if util.Exists(filepath.Join(dir, ".codegraph")) {
		if bin := resolveCodegraphBin(); bin != "" {
			cmd := exec.Command(bin, "sync")
			cmd.Dir = dir
			_ = cmd.Start()
		}
		return 0
	}
	bin := resolveCodegraphBin()
	if bin == "" {
		return 0
	}
	cmd := exec.Command(bin, "init")
	cmd.Dir = dir
	_ = cmd.Start()
	return 0
}

func resolveHookProjectDirFromInput(input []byte) string {
	if len(input) > 0 {
		var req struct {
			WorkspacePaths []string `json:"workspacePaths"`
			Cwd            string   `json:"cwd"`
		}
		if json.Unmarshal(input, &req) == nil {
			if len(req.WorkspacePaths) > 0 {
				return findProjectDir(req.WorkspacePaths[0])
			}
			if req.Cwd != "" {
				return findProjectDir(req.Cwd)
			}
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return findProjectDir(dir)
}

func resolveCodegraphBin() string {
	if p := util.ResolveCodegraphBin(); p != "" {
		return p
	}
	if matches, _ := filepath.Glob(filepath.Join(util.Home(), ".nvm", "versions", "node", "*", "bin")); len(matches) > 0 {
		sep := ":"
		if util.IsWin {
			sep = ";"
		}
		cur := os.Getenv("PATH")
		prefix := strings.Join(matches, sep)
		os.Setenv("PATH", prefix+sep+cur)
	}
	return util.ResolveCodegraphBin()
}
