package commands

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/tools"
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

// cursorProjectDir resolves current project root like CodeGraph.
func cursorProjectDir(explicit bool) (string, bool, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	root := findProjectDir(dir)
	if looksLikeProject(root) {
		return root, true, nil
	}
	if explicit {
		return dir, true, nil
	}
	return "", false, nil
}

func RunIndex(opts InitOptions, auto bool) int {
	dir, err := os.Getwd()
	if err != nil {
		util.L.Err("cannot resolve current directory: " + err.Error())
		if auto {
			return 0
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
		if auto && t.ID == "codegraph" {
			runCodegraphAutoIndex(dir)
			continue
		}
		if t.IndexReady != nil && !t.IndexReady() {
			if auto {
				util.L.Err(t.Label + " index: not installed")
			} else {
				util.L.Raw("  " + util.C.Gray("• ") + t.Label + util.C.Gray("  not installed — run tokless first"))
			}
			failed++
			continue
		}
		ok, ierr := t.IndexProject(dir, ro)
		if auto {
			if ierr != nil {
				util.L.Err(t.Label + " index: " + ierr.Error())
				failed++
			} else if !ok {
				util.L.Err(t.Label + " index: could not index")
				failed++
			}
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

// RunCodegraphAutoIndex indexes only CodeGraph for MCP startup.
func RunCodegraphAutoIndex(workspace ...string) int {
	dir := ""
	if len(workspace) > 0 {
		dir = workspace[0]
	}
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			util.L.Err("cannot resolve current directory: " + err.Error())
			return 1
		}
	}
	dir = findProjectDir(dir)
	if !looksLikeProject(dir) {
		return 0
	}
	return runCodegraphAutoIndex(dir)
}

// RunCodegraphMcpBootstrap initializes only a missing CodeGraph index.
func RunCodegraphMcpBootstrap(workspace ...string) int {
	dir := ""
	if len(workspace) > 0 {
		dir = workspace[0]
	}
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return 0
		}
	}
	dir = findProjectDir(dir)
	if !looksLikeProject(dir) || tools.HasCodegraphIndex(dir) {
		return 0
	}
	return runCodegraphAutoIndex(dir)
}

func runCodegraphAutoIndex(dir string) int {
	ok, err := tools.RunCodegraphIndex(dir, core.RunOpts{})
	if err != nil {
		util.L.Err("CodeGraph index: " + err.Error())
		return 0
	}
	if !ok {
		util.L.Err("CodeGraph index: could not index")
		return 0
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
	return runCodegraphAutoIndex(dir)
}

// RunCursorProjectRulesHook refreshes project rules on Cursor workspace startup.
func RunCursorProjectRulesHook() int {
	input, _ := io.ReadAll(os.Stdin)
	for _, root := range resolveHookProjectDirs(input) {
		project := findProjectDir(root)
		if !looksLikeProject(project) {
			continue
		}
		if ok, err := util.InstallCursorProjectRules(project, false); err != nil || !ok {
			if err != nil {
				util.L.Err(err.Error())
			}
		}
	}
	return 0
}

func resolveHookProjectDirs(input []byte) []string {
	var req struct {
		WorkspacePaths []string `json:"workspacePaths"`
		WorkspaceRoots []string `json:"workspace_roots"`
		Cwd            string   `json:"cwd"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &req)
	}
	paths := append(append([]string{}, req.WorkspaceRoots...), req.WorkspacePaths...)
	if req.Cwd != "" {
		paths = append(paths, req.Cwd)
	}
	if len(paths) == 0 {
		if cwd, err := os.Getwd(); err == nil {
			paths = append(paths, cwd)
		}
	}
	seen := map[string]bool{}
	var roots []string
	for _, path := range paths {
		path = normalizeCursorHookPath(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		roots = append(roots, path)
	}
	return roots
}

func normalizeCursorHookPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if runtime.GOOS != "linux" || os.Getenv("WSL_DISTRO_NAME") == "" || !isWindowsAbsolutePath(path) {
		return filepath.Clean(path)
	}
	if converted, err := exec.Command("wslpath", "-u", path).Output(); err == nil {
		return filepath.Clean(strings.TrimSpace(string(converted)))
	}
	return ""
}

func isWindowsAbsolutePath(path string) bool {
	return len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

func resolveHookProjectDirFromInput(input []byte) string {
	paths := resolveHookProjectDirs(input)
	if len(paths) == 0 {
		return ""
	}
	return findProjectDir(paths[0])
}
