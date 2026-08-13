package agents

import (
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ConfigureOpenCodeMcp writes/updates a local MCP entry in opencode config.
func ConfigureOpenCodeMcp(toolID string) (changed bool, file string) {
	p := util.OpenCodePathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.Config)
	if util.HasJSONCComments(raw) {
		return false, p.Config
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://opencode.ai/config.json")
	}
	mcp := getOrCreateMap(cfg, "mcp")

	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("opencode", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.McpSpawnFor(toolID)
	}
	command := append([]string{spawn.Command}, spawn.Args...)
	desired := util.NewOrderedMap()
	desired.Set("type", "local")
	desired.Set("command", toAnySlice(command))
	desired.Set("enabled", true)

	if existing, ok := mcp.Get(toolID); ok {
		if em, ok := existing.(*util.OrderedMap); ok {
			ec, _ := em.Get("command")
			if anyArrEq(ec, command) && notDisabled(em) {
				return false, p.Config
			}
		}
	}
	mcp.Set(toolID, desired)
	if err := util.WriteFile(p.Config, util.StringifyJSON(cfg)); err != nil {
		return false, p.Config
	}
	return true, p.Config
}

func RemoveOpenCodeMcp(toolID string) bool {
	p := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return false
	}
	if util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	mcpV, ok := cfg.Get("mcp")
	if !ok {
		return false
	}
	mcp, ok := mcpV.(*util.OrderedMap)
	if !ok {
		return false
	}
	if _, ok := mcp.Get(toolID); !ok {
		return false
	}
	mcp.Delete(toolID)
	return util.WriteFile(p.Config, util.StringifyJSON(cfg)) == nil
}

// --- OpenCode headroom HTTP proxy ---

func openCodeProxyProviderBlock(baseURL string) *util.OrderedMap {
	gpt4o := util.NewOrderedMap()
	gpt4o.Set("name", "GPT-4o")
	gpt4oLimit := util.NewOrderedMap()
	gpt4oLimit.Set("context", 128000)
	gpt4oLimit.Set("output", 16384)
	gpt4o.Set("limit", gpt4oLimit)

	gpt41 := util.NewOrderedMap()
	gpt41.Set("name", "GPT-4.1")
	gpt41Limit := util.NewOrderedMap()
	gpt41Limit.Set("context", 1048576)
	gpt41Limit.Set("output", 32768)
	gpt41.Set("limit", gpt41Limit)

	models := util.NewOrderedMap()
	models.Set("gpt-4o", gpt4o)
	models.Set("gpt-4.1", gpt41)

	opts := util.NewOrderedMap()
	opts.Set("baseURL", baseURL)

	block := util.NewOrderedMap()
	block.Set("npm", "@ai-sdk/openai-compatible")
	block.Set("name", "Headroom Proxy")
	block.Set("options", opts)
	block.Set("models", models)
	return block
}

// ConfigureOpenCodeProxy merges the `headroom` provider into the top-level
// `provider` object of the opencode config.
func ConfigureOpenCodeProxy() (changed bool, file string) {
	p := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return false, p.Config
	}
	if util.HasJSONCComments(raw) {
		return false, p.Config
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false, p.Config
	}
	var providers *util.OrderedMap
	if v, ok := cfg.Get("provider"); ok {
		em, isMap := v.(*util.OrderedMap)
		if !isMap {
			return false, p.Config
		}
		providers = em
	} else {
		providers = util.NewOrderedMap()
		cfg.Set("provider", providers)
	}
	entry := openCodeProxyProviderBlock(ProxyEndpointFor("opencode"))
	if existing, ok := providers.Get("headroom"); ok {
		if util.StringifyJSON(existing) == util.StringifyJSON(entry) {
			return false, p.Config
		}
		return false, p.Config
	}
	providers.Set("headroom", entry)
	if err := util.WriteFile(p.Config, util.StringifyJSON(cfg)); err != nil {
		return false, p.Config
	}
	return true, p.Config
}

// RemoveOpenCodeProxy deletes the `headroom` provider only when it still
// matches what tokless set.
func RemoveOpenCodeProxy() bool {
	p := util.OpenCodePathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return false
	}
	if util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		return false
	}
	existing, ok := providers.Get("headroom")
	if !ok {
		return false
	}
	if util.StringifyJSON(existing) != util.StringifyJSON(openCodeProxyProviderBlock(ProxyEndpointFor("opencode"))) {
		return false
	}
	providers.Delete("headroom")
	if providers.Len() == 0 {
		cfg.Delete("provider")
	}
	return util.WriteFile(p.Config, util.StringifyJSON(cfg)) == nil
}

// OpenCodeProxyWired reports whether the `headroom` provider is set to baseURL.
func OpenCodeProxyWired() bool {
	raw, ok := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		return false
	}
	existing, ok := providers.Get("headroom")
	if !ok {
		return false
	}
	return util.StringifyJSON(existing) == util.StringifyJSON(openCodeProxyProviderBlock(ProxyEndpointFor("opencode")))
}

func notDisabled(m *util.OrderedMap) bool {
	if v, ok := m.Get("enabled"); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
}

func anyArrEq(a any, b []string) bool {
	arr, ok := a.([]any)
	if !ok || len(arr) != len(b) {
		return false
	}
	for i, x := range arr {
		s, ok := x.(string)
		if !ok || s != b[i] {
			return false
		}
	}
	return true
}

func opencodeKnownBinDirs() []string {
	dirs := []string{
		filepath.Join(util.Home(), ".opencode", "bin"),
		filepath.Join(util.Home(), ".local", "bin"),
	}
	if util.IsWin {
		dirs = append(dirs, filepath.Join(util.Home(), "scoop", "shims"))
		if pd := os.Getenv("ProgramData"); pd != "" {
			dirs = append(dirs, filepath.Join(pd, "chocolatey", "bin"))
		}
	}
	return dirs
}

// opencodeDesktopPaths probes the OpenCode Desktop (Electron) install.
func opencodeDesktopPaths() []string {
	switch goosForDetect {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return []string{filepath.Join(local, "Programs", "OpenCode", "OpenCode.exe")}
		}
		return nil
	case "darwin":
		return []string{"/Applications/OpenCode.app"}
	default:
		return []string{"/usr/bin/ai.opencode.desktop"}
	}
}

var opencode = &core.AgentManifest{
	ID:        "opencode",
	Label:     "OpenCode",
	Homepage:  "https://github.com/anomalyco/opencode",
	CLIBin:    "opencode",
	ConfigDir: func() string { return util.OpenCodePathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("opencode", util.OpenCodePathsResolved().Dir, opencodeKnownBinDirs(), opencodeDesktopPaths())
	},
}
