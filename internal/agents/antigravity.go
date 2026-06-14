package agents

import (
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// antigravityMcpFiles returns every MCP config surface agy reads.
func antigravityMcpFiles() []string {
	p := util.AntigravityPathsResolved()
	files := []string{p.McpConfig, p.McpConfigCLI, p.Settings}
	gemini := filepath.Join(util.Home(), ".gemini")
	for _, variant := range []string{"antigravity-desktop", "antigravity-cli"} {
		if d := filepath.Join(gemini, variant); util.Exists(d) {
			files = append(files, filepath.Join(d, "mcp_config.json"))
		}
	}
	return files
}

// ConfigureAntigravityMcp upserts mcpServers.<tool> into every surface's MCP config.
func ConfigureAntigravityMcp(toolID string) (changed bool, file string) {
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("antigravity", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.PickMcpSpawn(toolID)
	}
	for _, f := range antigravityMcpFiles() {
		_ = util.EnsureDir(filepath.Dir(f))
		raw, _ := util.ReadFileSafe(f)
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			cfg = util.NewOrderedMap()
		}
		servers := getOrCreateMap(cfg, "mcpServers")
		entry := util.NewOrderedMap()
		entry.Set("command", spawn.Command)
		if len(spawn.Args) > 0 {
			entry.Set("args", spawn.Args)
		}
		entry.Set("trust", true)
		servers.Set(toolID, entry)
		if next := util.StringifyJSON(cfg); next != raw {
			_ = util.WriteFile(f, next)
			changed = true
			file = f
		}
	}
	return changed, file
}

// AntigravityMcpHas reports whether every surface's MCP config registers the tool.
func AntigravityMcpHas(toolID string) bool {
	for _, f := range antigravityMcpFiles() {
		raw, ok := util.ReadFileSafe(f)
		if !ok {
			return false
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			return false
		}
		found := false
		if s, ok := cfg.Get("mcpServers"); ok {
			if sm, ok := s.(*util.OrderedMap); ok {
				_, found = sm.Get(toolID)
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func agyKnownBinDirs() []string {
	if goosForDetect == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return []string{filepath.Join(local, "agy", "bin")}
		}
		return nil
	}
	return []string{filepath.Join(util.Home(), ".local", "bin")}
}

// antigravityDesktopPaths probes the Antigravity desktop app and IDE.
func antigravityDesktopPaths() []string {
	switch goosForDetect {
	case "windows":
		var paths []string
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			paths = append(paths,
				filepath.Join(local, "Programs", "Antigravity", "Antigravity.exe"),
				filepath.Join(local, "Programs", "Antigravity IDE", "Antigravity IDE.exe"))
		}
		return paths
	case "darwin":
		return []string{"/Applications/Antigravity.app", "/Applications/Antigravity IDE.app"}
	default:
		return []string{"/opt/antigravity", "/opt/antigravity-ide",
			"/usr/local/bin/antigravity", "/usr/local/bin/antigravity-ide"}
	}
}

var antigravity = &core.AgentManifest{
	ID:        "antigravity",
	Label:     "Antigravity",
	Homepage:  "https://antigravity.google",
	CLIBin:    "agy",
	ConfigDir: func() string { return util.AntigravityPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("agy", util.AntigravityPathsResolved().Dir, agyKnownBinDirs(), antigravityDesktopPaths())
	},
}

// RemoveAntigravityMcp deletes mcpServers.<tool> from every surface's MCP config.
func RemoveAntigravityMcp(toolID string) {
	for _, f := range antigravityMcpFiles() {
		raw, ok := util.ReadFileSafe(f)
		if !ok {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		if s, ok := cfg.Get("mcpServers"); ok {
			if sm, ok := s.(*util.OrderedMap); ok {
				if _, has := sm.Get(toolID); has {
					sm.Delete(toolID)
					_ = util.WriteFile(f, util.StringifyJSON(cfg))
				}
			}
		}
	}
}
