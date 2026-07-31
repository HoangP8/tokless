package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// projectmem remembers issues, attempts, fixes and decisions, so an agent can
// look up what already failed instead of trying it again.
//
// Its server needs --root <project path>, which one global config entry can't
// know, so we launch it through `tokless run-mcp --root-cwd` and fill that in
// at start-up.
//
// Setup runs from `tokless index`, next to codegraph's. Upstream's `pjm init`
// also adds git hooks and a project CLAUDE.md; we undo both, they're yours.

const projectmemPkg = "projectmem"

// projectmem 0.2.0 asks for `mcp>=0.1.0` with no upper bound, but mcp 2.0
// moved mcp.server.fastmcp, so a fresh install crashes on import and the
// server never starts. Pin it until upstream bounds the dependency.
var projectmemPins = []string{"mcp<2"}

func projectmemEnsureInstalled(opts core.RunOpts) (bool, error) {
	if isTest() {
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would install projectmem (uv/pipx/pip)")
		return true, nil
	}
	opts.Reportf("checking", 0.1)
	if util.ResolvePyBin("pjm") != "" && !opts.Upgrade {
		opts.Reportf("already installed", 1)
		return true, nil
	}
	opts.Reportf("installing projectmem", 0.4)
	ok := util.PyGlobalInstall(projectmemPkg, "pjm", opts.Upgrade, projectmemPins...)
	if !ok {
		// Once upstream needs a newer mcp, our pin makes the install unsolvable.
		opts.Reportf("retrying without version pins", 0.6)
		ok = util.PyGlobalInstall(projectmemPkg, "pjm", true)
	}
	if !ok {
		util.L.Err("projectmem install failed across uv, pipx and pip.")
		util.L.Sub("Manual: uv tool install projectmem — https://github.com/riponcm/projectmem")
		return false, nil
	}
	// Same python the MCP entry will use, so a passing check means a server
	// that actually starts.
	opts.Reportf("checking server", 0.8)
	py, _ := util.ProjectmemServerCommand()
	if !util.PyImportOK(py, "projectmem.mcp_server") {
		util.L.Err("projectmem installed but its MCP server can't start.")
		util.L.Sub("Manual: uv tool install --force projectmem --with \"mcp<2\" — https://github.com/riponcm/projectmem")
		return false, nil
	}
	opts.Reportf("ready", 1)
	return true, nil
}

func projectmemReady() bool { return isTest() || util.ResolvePyBin("pjm") != "" }

// projectmemInitProject creates .projectmem/ in one project.
func projectmemInitProject(dir string, opts core.RunOpts) (bool, error) {
	if isTest() {
		return true, os.MkdirAll(filepath.Join(dir, ".projectmem"), 0o755)
	}
	bin := util.ResolvePyBin("pjm")
	if bin == "" {
		return false, fmt.Errorf("pjm executable not found")
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would run pjm init in " + dir)
		return true, nil
	}
	if util.Exists(filepath.Join(dir, ".projectmem")) {
		return true, nil
	}
	return runPjmInitGuarded(bin, dir)
}

// runPjmInitGuarded runs `pjm init` without letting it touch your files. Uses
// its opt-out flags if it has them, otherwise puts CLAUDE.md back and removes
// the git hooks afterwards.
func runPjmInitGuarded(bin, dir string) (bool, error) {
	args := append([]string{"init"}, pjmInitOptOutFlags(bin)...)
	suppressedHooks := containsStr(args, "--no-hooks")
	suppressedDoc := containsStr(args, "--no-claude-md")

	claudeMd := filepath.Join(dir, "CLAUDE.md")
	before, hadBefore := util.ReadFileSafe(claudeMd)

	r := util.Run(bin, args, util.RunOptions{Cwd: dir, Capture: true})
	if r.Code != 0 {
		return false, fmt.Errorf("pjm init failed%s", codegraphFailure(r.Stderr))
	}

	if !suppressedDoc {
		restoreProjectDoc(claudeMd, before, hadBefore)
	}
	if !suppressedHooks {
		_ = util.Run(bin, []string{"hooks", "uninstall"}, util.RunOptions{Cwd: dir, Capture: true})
	}
	return util.Exists(filepath.Join(dir, ".projectmem")), nil
}

// restoreProjectDoc puts CLAUDE.md back the way pjm found it.
func restoreProjectDoc(path, before string, hadBefore bool) {
	switch {
	case hadBefore:
		if cur, ok := util.ReadFileSafe(path); ok && cur != before {
			_ = util.WriteFile(path, before)
		}
	default:
		if util.Exists(path) {
			_ = os.Remove(path)
		}
	}
}

// pjmInitOptOutFlags returns whichever opt-out flags this pjm version has.
func pjmInitOptOutFlags(bin string) []string {
	help := util.Run(bin, []string{"init", "--help"}, util.RunOptions{Capture: true})
	text := help.Stdout + help.Stderr
	var flags []string
	for _, f := range []string{"--no-hooks", "--no-claude-md"} {
		if strings.Contains(text, f) {
			flags = append(flags, f)
		}
	}
	return flags
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

var projectmemWireFor, projectmemUnwireFor, projectmemVerifyFor = mcpAgentMaps("projectmem", projectmemReady)

var projectmem = &core.ToolManifest{
	ID:             "projectmem",
	Label:          "ProjectMem",
	Description:    "Local project memory: past issues, fixes and decisions recalled before repeating them.",
	Homepage:       "https://github.com/riponcm/projectmem",
	InstallHint:    "uv tool install projectmem",
	Channel:        core.ChannelPyPI,
	Pkg:            projectmemPkg,
	Bin:            "pjm",
	NeedsPython:    true,
	MinPythonMinor: 10,
	Install:        projectmemEnsureInstalled,
	IndexProject:   projectmemInitProject,
	IndexReady:     projectmemReady,
	WireFor:        projectmemWireFor,
	UnwireFor:      projectmemUnwireFor,
	VerifyFor:      projectmemVerifyFor,
}
