package agents

import (
	"encoding/json"
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
	pluginRemoved := removeOpenCodeTransportPlugin()
	byokRemoved := unwireOpenCodeBYOK()
	return pluginRemoved || byokRemoved
}

func OpenCodeProxyWired() bool {
	return openCodeTransportPluginWired() && openCodeRetrieveToolDisabled()
}

func OpenCodeProxySatisfied() bool {
	return OpenCodeProxyWired()
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
	retrieveStateRaw, retrieveStateExists := util.ReadFileSafe(openCodeRetrieveStatePath())
	if util.HasJSONCComments(raw) {
		return false, file
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	tools := getOrCreateMap(cfg, "tools")
	if value, ok := tools.Get("headroom_retrieve"); !ok || value != false {
		var previous any
		if value, ok := tools.Get("headroom_retrieve"); ok {
			previous = value
		}
		if err := saveOpenCodeRetrieveState(previous); err != nil {
			return false, file
		}
		tools.Set("headroom_retrieve", false)
		changed = true
	}
	plugins, ok := cfg.Get("plugin")
	if ok {
		entries, ok := plugins.([]any)
		if !ok {
			_ = restoreOpenCodeRetrieveState(retrieveStateRaw, retrieveStateExists)
			return false, file
		}
		for _, entry := range entries {
			if isOpenCodeTransportPluginEntry(entry) {
				if changed {
					if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
						_ = restoreOpenCodeRetrieveState(retrieveStateRaw, retrieveStateExists)
						return false, file
					}
				}
				return changed, file
			}
		}
		cfg.Set("plugin", append(entries, openCodeTransportPluginEntry()))
		changed = true
	} else {
		cfg.Set("plugin", []any{openCodeTransportPluginEntry()})
		changed = true
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://opencode.ai/config.json")
	}
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		_ = restoreOpenCodeRetrieveState(retrieveStateRaw, retrieveStateExists)
		return false, file
	}
	return changed, file
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
	if value, ok := loadOpenCodeRetrieveState(); ok {
		if value == nil {
			tools, _ := mapChild(cfg, "tools")
			if tools != nil {
				tools.Delete("headroom_retrieve")
				if tools.Len() == 0 {
					cfg.Delete("tools")
				}
			}
		} else {
			tools := getOrCreateMap(cfg, "tools")
			tools.Set("headroom_retrieve", value)
		}
	}
	if len(next) == 0 {
		cfg.Delete("plugin")
	} else {
		cfg.Set("plugin", next)
	}
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		return false
	}
	_ = clearOpenCodeRetrieveState()
	return true
}

func restoreOpenCodeRetrieveState(raw string, exists bool) error {
	if exists {
		return util.WriteFileMode(openCodeRetrieveStatePath(), raw, 0o600)
	}
	if err := os.Remove(openCodeRetrieveStatePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func openCodeRetrieveStatePath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "opencode.retrieve.stash.json")
}

func saveOpenCodeRetrieveState(value any) error {
	b, err := json.Marshal(struct {
		Present bool `json:"present"`
		Value   any  `json:"value"`
	}{Present: value != nil, Value: value})
	if err != nil {
		return err
	}
	return util.WriteFileMode(openCodeRetrieveStatePath(), string(b), 0o600)
}

func loadOpenCodeRetrieveState() (any, bool) {
	raw, ok := util.ReadFileSafe(openCodeRetrieveStatePath())
	if !ok {
		return nil, false
	}
	var state struct {
		Present bool `json:"present"`
		Value   any  `json:"value"`
	}
	if json.Unmarshal([]byte(raw), &state) != nil {
		return nil, false
	}
	if !state.Present {
		return nil, true
	}
	return state.Value, true
}

func clearOpenCodeRetrieveState() error {
	if err := os.Remove(openCodeRetrieveStatePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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

func openCodeRetrieveToolDisabled() bool {
	raw, ok := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if !ok || util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	toolsV, ok := cfg.Get("tools")
	if !ok {
		return false
	}
	tools, ok := toolsV.(*util.OrderedMap)
	if !ok {
		return false
	}
	value, ok := tools.Get("headroom_retrieve")
	return ok && value == false
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
