package agents

import (
	"os"
	"path/filepath"
	"strings"

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

func ensureOpenCodeEnabledProvider(path, provider string) (changed, ok bool) {
	raw, exists := util.ReadFileSafe(path)
	if !exists || util.HasJSONCComments(raw) {
		return false, true
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false, false
	}
	enabled, exists := cfg.Get("enabled_providers")
	if !exists {
		return false, true
	}
	providers, ok := enabled.([]any)
	if !ok {
		return false, false
	}
	for _, value := range providers {
		if value == provider {
			return false, true
		}
	}
	cfg.Set("enabled_providers", append(providers, provider))
	if err := util.WriteFile(path, util.StringifyJSON(cfg)); err != nil {
		return false, false
	}
	return true, true
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
	return openCodeProxyProviderBlockFor(baseURL, DefaultProviderSpec())
}

func openCodeProxyProviderBlockFor(baseURL string, spec ProviderSpec) *util.OrderedMap {
	models := util.NewOrderedMap()
	for _, m := range spec.Models {
		entry := util.NewOrderedMap()
		entry.Set("name", m.Display)
		if m.Reasoning {
			entry.Set("reasoning", true)
		}
		limit := util.NewOrderedMap()
		limit.Set("context", m.Context)
		limit.Set("output", m.Output)
		entry.Set("limit", limit)
		models.Set(m.ID, entry)
	}

	opts := util.NewOrderedMap()
	opts.Set("baseURL", baseURL)
	if spec.KeyEnv != "" {
		opts.Set("apiKey", "{env:"+spec.KeyEnv+"}")
	}

	block := util.NewOrderedMap()
	block.Set("npm", spec.Npm)
	block.Set("name", spec.Name)
	block.Set("options", opts)
	block.Set("models", models)
	return block
}

func openCodeProxySpecs() []ProviderSpec {
	return []ProviderSpec{DefaultProviderSpec()}
}

func ConfigureOpenCodeProxy() (changed bool, file string) {
	legacyChanged := unwireOpenCodeBYOK()
	pluginChanged, file := configureOpenCodeTransportPlugin()
	return legacyChanged || pluginChanged, file
}

func RemoveOpenCodeProxy() bool {
	return removeOpenCodeTransportPlugin() || unwireOpenCodeBYOK()
}

func OpenCodeProxyWired() bool {
	return openCodeTransportPluginWired()
}

func OpenCodeProxySatisfied() bool {
	return openCodeTransportPluginWired()
}

func openCodeTransportPluginPath() string {
	p := util.HeadroomPathsResolved()
	return filepath.Join(p.Tools, "headroom-ai", "lib", "python3.13", "site-packages", "headroom", "providers", "opencode", "_dist", "entry.opencode.js")
}

func openCodeTransportPluginURL() string {
	return "file://" + filepath.ToSlash(openCodeTransportPluginPath())
}

func openCodeTransportPluginEntry() []any {
	options := util.NewOrderedMap()
	options.Set("proxyUrl", strings.TrimSuffix(ProxyEndpointFor("opencode"), "/v1"))
	return []any{openCodeTransportPluginURL(), options}
}

func isOpenCodeTransportPluginEntry(v any) bool {
	entry, ok := v.([]any)
	if !ok || len(entry) != 2 {
		return false
	}
	path, _ := entry[0].(string)
	if path != openCodeTransportPluginURL() {
		return false
	}
	options, ok := entry[1].(*util.OrderedMap)
	if !ok {
		return false
	}
	proxyURL, _ := options.Get("proxyUrl")
	return proxyURL == strings.TrimSuffix(ProxyEndpointFor("opencode"), "/v1")
}

func configureOpenCodeTransportPlugin() (changed bool, file string) {
	file = util.OpenCodePathsResolved().Config
	if !util.Exists(openCodeTransportPluginPath()) {
		return false, file
	}
	raw, _ := util.ReadFileSafe(file)
	if util.HasJSONCComments(raw) {
		return false, file
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	plugins, ok := cfg.Get("plugin")
	if ok {
		entries, ok := plugins.([]any)
		if !ok {
			return false, file
		}
		for _, entry := range entries {
			if isOpenCodeTransportPluginEntry(entry) {
				return false, file
			}
		}
		cfg.Set("plugin", append(entries, openCodeTransportPluginEntry()))
	} else {
		cfg.Set("plugin", []any{openCodeTransportPluginEntry()})
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://opencode.ai/config.json")
	}
	return util.WriteFile(file, util.StringifyJSON(cfg)) == nil, file
}

func removeOpenCodeTransportPlugin() bool {
	file := util.OpenCodePathsResolved().Config
	raw, ok := util.ReadFileSafe(file)
	if !ok || util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	plugins, ok := cfg.Get("plugin")
	if !ok {
		return false
	}
	entries, ok := plugins.([]any)
	if !ok {
		return false
	}
	next := make([]any, 0, len(entries))
	removed := false
	for _, entry := range entries {
		if isOpenCodeTransportPluginEntry(entry) {
			removed = true
			continue
		}
		next = append(next, entry)
	}
	if !removed {
		return false
	}
	if len(next) == 0 {
		cfg.Delete("plugin")
	} else {
		cfg.Set("plugin", next)
	}
	return util.WriteFile(file, util.StringifyJSON(cfg)) == nil
}

func openCodeTransportPluginWired() bool {
	raw, ok := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if !ok || util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	plugins, ok := cfg.Get("plugin")
	if !ok {
		return false
	}
	entries, ok := plugins.([]any)
	if !ok {
		return false
	}
	for _, entry := range entries {
		if isOpenCodeTransportPluginEntry(entry) {
			return true
		}
	}
	return false
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
