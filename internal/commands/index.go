package commands

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

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

// hookStdinDir extracts the project dir from a hook's stdin JSON payload.
func hookStdinDir() string {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice != 0 {
		return ""
	}
	return parseHookDir(os.Stdin)
}

func parseHookDir(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil || len(b) == 0 {
		return ""
	}
	var in struct {
		Cwd            string   `json:"cwd"`
		WorkspaceRoots []string `json:"workspace_roots"`
	}
	if json.Unmarshal(b, &in) != nil {
		return ""
	}
	if in.Cwd != "" {
		return in.Cwd
	}
	if len(in.WorkspaceRoots) > 0 {
		return in.WorkspaceRoots[0]
	}
	return ""
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
		if d := hookStdinDir(); d != "" {
			dir = d
		}
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
