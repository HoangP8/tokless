package commands

import (
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// RunInfo prints install provenance and paths.
func RunInfo() int {
	cmdHeader("info", "install + paths")

	rec, exact := util.InstallInfo()
	util.TreeTop("Tokless")
	util.TreeLeaf(paintKey(padEnd("version", 10)) + util.C.Bold(paintVer(util.ToklessVersion())))
	util.TreeLeaf(paintKey(padEnd("binary", 10)) + paintPath(tildePath(util.ToklessAbs())))
	util.TreeLeaf(paintKey(padEnd("install", 10)) + formatInstallMethod(rec, exact))
	util.TreeLeaf(paintKey(padEnd("platform", 10)) + util.C.Blue(runtime.GOOS+"/"+runtime.GOARCH))
	util.TreeClose()

	util.TreeCorner("Agents")
	nAgents := 0
	for _, agent := range core.ListAgents() {
		if !agent.Detect().Installed {
			continue
		}
		nAgents++
		dir := ""
		if agent.ConfigDir != nil {
			dir = tildePath(agent.ConfigDir())
		}
		path := util.C.Dim("—")
		if dir != "" {
			path = paintPath(dir)
		}
		util.TreeLeaf(util.C.Green(util.Sym.Check) + " " + paintName(padEnd(agent.Label, 16)) + path)
	}
	if nAgents == 0 {
		util.TreeLeaf(util.C.Dim("none detected on this machine"))
	}
	util.TreeClose()

	util.TreeCorner("Tools")
	for _, tool := range core.ListTools() {
		if tool.InstructionOnly {
			continue
		}
		if v := util.InstalledVersionFor(tool.ID); v != nil {
			row := paintName(padEnd(tool.ID, 14)) + paintVer(padEnd("v"+*v, 10))
			if p := util.InstalledPathFor(tool.ID); p != "" {
				row += paintPath(tildePath(p))
			}
			util.TreeLeaf(util.C.Green(util.Sym.Check) + " " + row)
		} else {
			util.TreeLeaf(util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(padEnd(tool.ID, 14)) + util.C.Gray("not installed"))
		}
	}
	util.TreeClose()

	util.TreeCorner("State")
	for _, item := range []struct {
		label string
		path  string
	}{
		{"install marker", util.InstallMarkerPath()},
		{"version cache", util.VersionCachePath()},
	} {
		if util.Exists(item.path) {
			util.TreeLeaf(util.C.Green(util.Sym.Check) + " " + paintName(padEnd(item.label, 16)) + paintPath(tildePath(item.path)))
		} else {
			util.TreeLeaf(util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(padEnd(item.label, 16)) + paintPath(tildePath(item.path)) + util.C.Yellow(" missing"))
		}
	}
	util.TreeClose()

	treeStatus(statusKV(
		util.C.Cyan(util.Sym.Info),
		"tip",
		util.C.Gray("run ")+paintCmd("tokless doctor")+util.C.Gray(" for health + updates"),
	))
	printRepoFooter(true)
	util.L.Raw("")
	return 0
}

func formatInstallMethod(rec util.InstallRecord, exact bool) string {
	method := rec.Method
	if method == "" {
		method = "unknown"
	}
	if !exact {
		return util.C.Yellow(method) + util.C.Gray("  (guessed from path)")
	}
	out := util.C.Green(util.C.Bold(method))
	if rec.At != "" {
		if t, err := time.Parse(time.RFC3339, rec.At); err == nil {
			out += " " + paintVer(t.UTC().Format("2006-01-02"))
		} else if i := strings.Index(rec.At, "T"); i != -1 {
			out += " " + paintVer(rec.At[:i])
		}
	}
	return out
}

// tildePath shortens home-relative paths for display.
func tildePath(p string) string {
	if p == "" {
		return ""
	}
	display := filepath.ToSlash(p)
	home := util.Home()
	if home == "" {
		return display
	}
	homeSlash := filepath.ToSlash(home)
	if util.IsWin {
		if strings.HasPrefix(strings.ToLower(display), strings.ToLower(homeSlash)) {
			return "~" + display[len(homeSlash):]
		}
		return display
	}
	if strings.HasPrefix(display, homeSlash) {
		return "~" + display[len(homeSlash):]
	}
	return display
}
