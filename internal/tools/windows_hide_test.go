package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// childProcessCall matches each Node child_process call a generated plugin makes.
var childProcessCall = regexp.MustCompile(`\b(spawn|spawnSync|exec|execSync|execFile|execFileSync|fork)\(`)

// TestPluginSpawnsHideWindowsConsole guards issue #70: every console child must set windowsHide.
func TestPluginSpawnsHideWindowsConsole(t *testing.T) {
	prev := util.IsWin
	util.IsWin = true
	t.Cleanup(func() { util.IsWin = prev })
	templates := map[string]string{
		"omp rtk":              ompRtkExtension,
		"opencode rtk":         openCodeRtkPlugin,
		"opencode2 rtk":        openCodeRtkV2Plugin,
		"amp rtk":              ampRtkPlugin,
		"cline rtk":            clineRtkHookScript(`C:\tokless.exe`),
		"pi projectmem":        piProjectmemExtension,
		"kilo projectmem":      kiloProjectmemPlugin,
		"opencode2 projectmem": openCodeProjectmemV2Plugin,
	}
	for name, src := range templates {
		calls := 0
		for _, line := range strings.Split(src, "\n") {
			if !childProcessCall.MatchString(line) {
				continue
			}
			calls++
			if !strings.Contains(line, "windowsHide: true") {
				t.Errorf("%s: child_process call without windowsHide: %s", name, strings.TrimSpace(line))
			}
		}
		if calls == 0 {
			t.Errorf("%s: no child_process call found", name)
		}
	}
}
