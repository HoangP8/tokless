package commands

import (
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// RunInfo prints where tokless came from and where everything lives.
// It is deliberately offline: health and update checks belong to doctor.
func RunInfo() int {
	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold(util.C.Cyan("tokless info")) + util.C.Gray("  install + paths"))
	util.L.Raw("")

	rec, exact := util.InstallInfo()
	install := rec.Method
	if !exact {
		install += util.C.Gray(" (guessed from path)")
	} else if rec.At != "" {
		install += util.C.Gray("  " + firstField(rec.At, "T"))
	}

	infoRow("version", util.ToklessVersion())
	infoRow("binary", tildePath(util.ToklessAbs()))
	infoRow("install", install)

	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold("Agents"))
	for _, agent := range core.ListAgents() {
		dir := ""
		if agent.ConfigDir != nil {
			dir = tildePath(agent.ConfigDir())
		}
		if !agent.Detect().Installed {
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" "+padEnd(agent.Label, 16)+"not installed"))
			continue
		}
		if dir == "" {
			dir = util.C.Gray("—")
		}
		util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + padEnd(agent.Label, 16) + util.C.Gray(dir))
	}

	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold("Tools"))
	for _, tool := range core.ListTools() {
		if tool.InstructionOnly {
			continue
		}
		if v := util.InstalledVersionFor(tool.ID); v != nil {
			row := padEnd(tool.ID, 16) + padEnd("v"+*v, 12)
			if p := util.InstalledPathFor(tool.ID); p != "" {
				row += tildePath(p)
			}
			util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray(row))
		} else {
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" "+padEnd(tool.ID, 16)+"not installed"))
		}
	}

	util.L.Raw("")
	util.L.Raw("  " + util.C.Bold("tokless files"))
	for _, p := range []string{util.InstallMarkerPath(), util.VersionCachePath()} {
		mark := util.C.Gray(util.Sym.Bullet)
		if util.Exists(p) {
			mark = util.C.Green(util.Sym.Check)
		}
		util.L.Raw("  " + mark + " " + util.C.Gray(tildePath(p)))
	}

	util.L.Raw("")
	util.L.Info("Run " + util.C.Cyan("tokless doctor") + " to check health and updates.")
	util.L.Raw("")
	return 0
}

func infoRow(label, value string) {
	util.L.Raw("  " + util.C.Gray(padEnd(label, 9)) + value)
}

// tildePath shortens a home-relative path for display.
func tildePath(p string) string {
	if p == "" {
		return ""
	}
	home := util.Home()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + filepath.ToSlash(strings.TrimPrefix(p, home))
	}
	return p
}

func firstField(s, sep string) string {
	if i := strings.Index(s, sep); i != -1 {
		return s[:i]
	}
	return s
}
