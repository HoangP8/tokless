package tools

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/util"
)

// managedPlugin is a tokless-owned plugin file and its current rendering.
type managedPlugin struct {
	path  string
	owned func(raw string) bool
	want  string
}

func containsMarker(marker string) func(string) bool {
	return func(raw string) bool { return strings.Contains(raw, marker) }
}

// managedPlugins lists every tokless-generated plugin template refreshed on update.
func managedPlugins(exe string) []managedPlugin {
	q := strconv.Quote(exe)
	files := []managedPlugin{
		{openCodeRtkPluginPath(), containsMarker("TOKLESS_BIN"), fmt.Sprintf(openCodeRtkPlugin, q)},
		{openCodeRtkV2PluginPath(), containsMarker("TOKLESS_BIN"), fmt.Sprintf(openCodeRtkV2Plugin, q)},
		{ompRtkExtensionPath(), containsMarker(`["rtk-hook", "omp"]`), fmt.Sprintf(ompRtkExtension, q)},
		{ampRtkPluginPath(), func(raw string) bool { return strings.HasPrefix(raw, "// tokless-rtk") }, fmt.Sprintf(ampRtkPlugin, q)},
		{clineRtkHookPath(), containsMarker(clineRtkMarker), clineRtkHookScript(exe)},
	}
	for _, agent := range []string{"pi", "omp", "kilo", "opencode", "opencode2"} {
		files = append(files, managedPlugin{projectmemExtensionPath(agent), containsMarker(projectmemExtensionMarker), projectmemExtensionContent(agent, exe)})
	}
	return files
}

// RefreshManagedPlugins rewrites tokless-owned plugin files whose content
// drifted from the current template, so template fixes reach existing installs
// on update.
func RefreshManagedPlugins() {
	exe := util.ToklessPersistedAbs()
	if exe == "" {
		return
	}
	for _, f := range managedPlugins(exe) {
		if raw, ok := util.ReadFileSafe(f.path); ok && raw != f.want && f.owned(raw) {
			if err := util.WriteFile(f.path, f.want); err != nil {
				util.L.Debug("plugin refresh failed: " + filepath.Base(f.path))
			}
		}
	}
	agents.RefreshDshRtkPlugin(exe)
}
