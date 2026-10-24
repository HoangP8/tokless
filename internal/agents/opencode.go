package agents

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

//go:embed opencode_byok_plugin.js
var toklessOpenCodeBYOKPlugin []byte

// openCodeMcpCommand returns the canonical spawn command for a tool.
func openCodeMcpCommand(agentID, toolID string) []string {
	if toolID == "codegraph" {
		spawn := util.WrapAutoIndex(agentID, util.PickMcpSpawn("codegraph", "serve", "--mcp"))
		return append([]string{spawn.Command}, spawn.Args...)
	}
	spawn := util.McpSpawnFor(toolID)
	return append([]string{spawn.Command}, spawn.Args...)
}

// openCodeAgentID returns the manifest agent label for the v1/v2 config shape.
func openCodeAgentID(v2 bool) string {
	if v2 {
		return "opencode2"
	}
	return "opencode"
}

// openCodeMcpEntryEnabled reports enabled != false (v1) and disabled != true (v2).
func openCodeMcpEntryEnabled(entry *util.OrderedMap) bool {
	if v, ok := entry.Get("enabled"); ok {
		if b, ok := v.(bool); ok && !b {
			return false
		}
	}
	if v, ok := entry.Get("disabled"); ok {
		if b, ok := v.(bool); ok && b {
			return false
		}
	}
	return true
}

func openCodeMcpEntryCommandMatches(entry *util.OrderedMap, command []string) bool {
	v, _ := entry.Get("command")
	return anyArrEq(v, command)
}

// openCodeMcpRootEntry returns the v1 root entry for toolID, if present.
func openCodeMcpRootEntry(mcp *util.OrderedMap, toolID string) (*util.OrderedMap, bool) {
	if v, ok := mcp.Get(toolID); ok {
		if em, ok := v.(*util.OrderedMap); ok {
			return em, true
		}
	}
	return nil, false
}

// openCodeMcpServerEntry returns the v2 servers entry for toolID, if present.
func openCodeMcpServerEntry(mcp *util.OrderedMap, toolID string) (*util.OrderedMap, bool) {
	servers, ok := mapChild(mcp, "servers")
	if !ok {
		return nil, false
	}
	if v, ok := servers.Get(toolID); ok {
		if em, ok := v.(*util.OrderedMap); ok {
			return em, true
		}
	}
	return nil, false
}

// OpenCodeMcpHas reports whether toolID is enabled in either MCP config shape.
func OpenCodeMcpHas(cfg *util.OrderedMap, toolID string) bool {
	if cfg == nil {
		return false
	}
	mcp, ok := mapChild(cfg, "mcp")
	if !ok {
		return false
	}
	if em, ok := openCodeMcpRootEntry(mcp, toolID); ok && openCodeMcpEntryEnabled(em) {
		return true
	}
	if em, ok := openCodeMcpServerEntry(mcp, toolID); ok && openCodeMcpEntryEnabled(em) {
		return true
	}
	return false
}

// OpenCodeMcpMatches reports whether either MCP config shape has the enabled command.
func OpenCodeMcpMatches(cfg *util.OrderedMap, toolID string, command []string) bool {
	if cfg == nil {
		return false
	}
	mcp, ok := mapChild(cfg, "mcp")
	if !ok {
		return false
	}
	if em, ok := openCodeMcpRootEntry(mcp, toolID); ok && openCodeMcpEntryEnabled(em) && openCodeMcpEntryCommandMatches(em, command) {
		return true
	}
	if em, ok := openCodeMcpServerEntry(mcp, toolID); ok && openCodeMcpEntryEnabled(em) && openCodeMcpEntryCommandMatches(em, command) {
		return true
	}
	return false
}

// ConfigureOpenCodeMcp writes the v1 MCP entry.
func ConfigureOpenCodeMcp(toolID string) (bool, string) {
	return ConfigureOpenCodeMcpV1(toolID)
}

func ConfigureOpenCodeMcpV1(toolID string) (changed bool, file string) {
	p := util.OpenCodePathsResolved()
	return configureOpenCodeMcp(toolID, p.Config, false)
}

func ConfigureOpenCodeMcpV2(toolID string) (changed bool, file string) {
	p := util.OpenCodePathsV2()
	return configureOpenCodeMcp(toolID, p.Config, true)
}

func configureOpenCodeMcp(toolID, file string, v2 bool) (changed bool, path string) {
	path = file
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return false, path
	}
	raw, exists := util.ReadFileSafe(path)
	if !exists && util.Exists(path) {
		return false, path
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if strings.TrimSpace(raw) != "" {
			return false, path
		}
		cfg = util.NewOrderedMap()
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://opencode.ai/config.json")
	}
	mcp, ok := mapChild(cfg, "mcp")
	if !ok {
		if _, exists := cfg.Get("mcp"); exists {
			return false, path
		}
		mcp = util.NewOrderedMap()
		cfg.Set("mcp", mcp)
	}
	parent := mcp
	if v2 {
		parent, ok = mapChild(mcp, "servers")
		if !ok {
			if _, exists := mcp.Get("servers"); exists {
				return false, path
			}
			parent = util.NewOrderedMap()
			mcp.Set("servers", parent)
		}
	}
	command := openCodeMcpCommand(openCodeAgentID(v2), toolID)
	if entry, has := parent.Get(toolID); has {
		if em, isMap := entry.(*util.OrderedMap); isMap && openCodeMcpEntryEnabled(em) && openCodeMcpEntryCommandMatches(em, command) {
			return false, path
		}
	}
	entry := util.NewOrderedMap()
	entry.Set("type", "local")
	entry.Set("command", toAnySlice(command))
	if v2 {
		entry.Set("disabled", false)
	} else {
		entry.Set("enabled", true)
	}
	parent.Set(toolID, entry)
	if err := util.WriteFile(path, util.StringifyJSON(cfg)); err != nil {
		return false, path
	}
	return true, path
}

func ensureOpenCodeEnabledProvider(path, provider string) (changed, ok bool) {
	raw, exists := util.ReadFileSafe(path)
	if !exists && util.Exists(path) {
		return false, false
	}
	if !exists {
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
	removedV1 := RemoveOpenCodeMcpV1(toolID)
	removedV2 := RemoveOpenCodeMcpV2(toolID)
	return removedV1 || removedV2
}

func RemoveOpenCodeMcpV1(toolID string) bool {
	p := util.OpenCodePathsResolved()
	return removeOpenCodeMcp(toolID, p.Config, false)
}

func RemoveOpenCodeMcpV2(toolID string) bool {
	return removeOpenCodeMcp(toolID, util.OpenCodePathsV2().Config, true)
}

func removeOpenCodeMcp(toolID, path string, v2 bool) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
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
	parent := mcp
	if v2 {
		parent, ok = mapChild(mcp, "servers")
		if !ok {
			return false
		}
	}
	command := openCodeMcpCommand(openCodeAgentID(v2), toolID)
	existing, has := parent.Get(toolID)
	if !has {
		return false
	}
	entry, isMap := existing.(*util.OrderedMap)
	if !isMap || !openCodeMcpEntryCommandMatches(entry, command) {
		return false
	}
	parent.Delete(toolID)
	if v2 && parent.Len() == 0 {
		mcp.Delete("servers")
	}
	if mcp.Len() == 0 {
		cfg.Delete("mcp")
	}
	return util.WriteFile(path, util.StringifyJSON(cfg)) == nil
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
	err := withProxyRouteStashLock(func() error {
		var err error
		changed, file, err = configureOpenCodeProxyLocked()
		return err
	})
	if err != nil {
		util.L.Err("opencode proxy lock failed: " + err.Error())
	}
	return changed, file
}

func configureOpenCodeProxyLocked() (changed bool, file string, resultErr error) {
	file = util.OpenCodePathsResolved().Config
	configRaw, configExists := util.ReadFileSafe(file)
	retrieveRaw, retrieveExists := util.ReadFileSafe(openCodeRetrieveStatePath())
	type routeChange struct {
		id       string
		previous BYOKRoute
	}
	var routeChanges []routeChange
	rollbackRoutes := func() error {
		var errs []error
		for i := len(routeChanges) - 1; i >= 0; i-- {
			route := routeChanges[i]
			if route.previous.ID == "" {
				if err := util.DeleteBYOKRoute(route.id); err != nil {
					errs = append(errs, fmt.Errorf("delete OpenCode route %s: %w", route.id, err))
				}
				continue
			}
			if _, _, err := util.UpsertBYOKRoute(route.id, route.previous.Protocol, route.previous.Upstream); err != nil {
				errs = append(errs, fmt.Errorf("restore OpenCode route %s: %w", route.id, err))
			}
		}
		return errors.Join(errs...)
	}
	for _, provider := range DiscoverOpenCodeBYOK() {
		if provider.Protocol == "" {
			continue
		}
		id := "opencode:" + provider.ID
		_, previous, err := util.UpsertBYOKRoute(id, provider.Protocol, provider.BaseURL)
		if err != nil {
			return false, file, errors.Join(err, rollbackRoutes())
		}
		routeChanges = append(routeChanges, routeChange{id: id, previous: previous})
	}
	pluginChanged, file := configureOpenCodeTransportPlugin()
	if !pluginChanged && !openCodeTransportPluginWired() {
		return false, file, errors.Join(fmt.Errorf("configure OpenCode transport plugin"), rollbackRoutes())
	}
	byokChanged, _, ok := wireOpenCodeBYOKLocked()
	if !ok {
		errs := []error{fmt.Errorf("configure OpenCode BYOK routes"), rollbackRoutes()}
		if err := restoreCodexConfig(file, configRaw, configExists); err != nil {
			errs = append(errs, err)
		}
		if err := restoreOpenCodeRetrieveState(retrieveRaw, retrieveExists); err != nil {
			errs = append(errs, err)
		}
		return false, file, errors.Join(errs...)
	}
	return byokChanged || pluginChanged, file, nil
}

func RemoveOpenCodeProxy() bool {
	var removed bool
	if err := withProxyRouteStashLock(func() error {
		var err error
		removed, err = removeOpenCodeProxyLocked()
		return err
	}); err != nil {
		util.L.Err("opencode proxy lock failed: " + err.Error())
		return false
	}
	return removed
}

func removeOpenCodeProxyLocked() (bool, error) {
	if _, exists := util.ReadFileSafe(openCodeRetrieveStatePath()); exists {
		if _, ok := loadOpenCodeRetrieveState(); !ok {
			return false, fmt.Errorf("invalid OpenCode retrieve stash")
		}
	}
	if !byokStashValid() {
		return false, fmt.Errorf("invalid OpenCode BYOK stash")
	}
	byokRemoved := false
	stashed := loadBYOKStash()
	routeIDs := map[string]bool{}
	if routes, err := util.ReadBYOKRoutes(); err == nil || os.IsNotExist(err) {
		for _, route := range routes {
			if strings.HasPrefix(route.ID, "opencode:") {
				routeIDs[route.ID] = true
			}
		}
	} else {
		return false, fmt.Errorf("read BYOK routes")
	}
	for _, provider := range DiscoverOpenCodeBYOK() {
		routeIDs["opencode:"+provider.ID] = true
	}
	for id := range stashed {
		routeIDs["opencode:"+id] = true
	}
	pluginWired := openCodeTransportPluginWired()
	configPath := util.OpenCodePathsResolved().Config
	configRaw, configExists := util.ReadFileSafe(configPath)
	retrieveRaw, retrieveExists := util.ReadFileSafe(openCodeRetrieveStatePath())
	byokStashRaw, byokStashExists := util.ReadFileSafe(byokStashPath())
	providerFiles := map[string]string{}
	for _, route := range stashed {
		if raw, ok := util.ReadFileSafe(route.File); ok {
			providerFiles[route.File] = raw
		}
	}
	removed, ok := unwireOpenCodeBYOKLocked()
	if !ok {
		return false, fmt.Errorf("restore OpenCode BYOK providers")
	}
	byokRemoved = removed
	pluginRemoved := removeOpenCodeTransportPlugin()
	if pluginWired && !pluginRemoved {
		var errs []error
		for path, raw := range providerFiles {
			if err := util.WriteFile(path, raw); err != nil {
				errs = append(errs, err)
			}
		}
		if err := restoreCodexConfig(byokStashPath(), byokStashRaw, byokStashExists); err != nil {
			errs = append(errs, err)
		}
		if err := restoreCodexConfig(configPath, configRaw, configExists); err != nil {
			errs = append(errs, err)
		}
		if err := restoreOpenCodeRetrieveState(retrieveRaw, retrieveExists); err != nil {
			errs = append(errs, err)
		}
		return false, errors.Join(append([]error{fmt.Errorf("remove OpenCode transport plugin")}, errs...)...)
	}
	for id := range routeIDs {
		if err := util.DeleteBYOKRoute(id); err != nil {
			for path, raw := range providerFiles {
				_ = util.WriteFile(path, raw)
			}
			_ = restoreCodexConfig(byokStashPath(), byokStashRaw, byokStashExists)
			_ = restoreCodexConfig(configPath, configRaw, configExists)
			_ = restoreOpenCodeRetrieveState(retrieveRaw, retrieveExists)
			return false, fmt.Errorf("delete OpenCode route %s: %w", id, err)
		}
	}
	return pluginRemoved || byokRemoved, nil
}

func OpenCodeProxyWired() bool {
	return openCodeTransportPluginWired() && openCodeRetrieveToolDisabled()
}

func OpenCodeProxySatisfied() bool {
	return OpenCodeProxyWired()
}

func openCodeHeadroomPluginDir() string {
	p := util.HeadroomPathsResolved()
	root := filepath.Join(p.Tools, "headroom-ai")
	if util.IsWin {
		root = filepath.Join(root, "Lib", "site-packages")
	} else {
		root = filepath.Join(root, "lib", "python3.13", "site-packages")
	}
	return filepath.Join(root, "headroom", "providers", "opencode", "_dist")
}

// openCodeTransportPluginPath returns the headroom bundle the wrapper imports.
func openCodeTransportPluginPath() string {
	dir := openCodeHeadroomPluginDir()
	if util.Exists(filepath.Join(dir, "index.js")) {
		return filepath.Join(dir, "index.js")
	}
	return filepath.Join(dir, "entry.opencode.js")
}

// openCodeBYOKPluginSource returns the wrapper source for the installed headroom layout.
func openCodeBYOKPluginSource() string {
	importPath := `"` + "./" + filepath.Base(openCodeTransportPluginPath()) + `"`
	return strings.Replace(string(toklessOpenCodeBYOKPlugin), `"./entry.opencode.js"`, importPath, 1)
}

func openCodeBYOKPluginPath() string {
	return filepath.Join(filepath.Dir(openCodeTransportPluginPath()), "tokless-byok.js")
}

func openCodeTransportPluginURL() string {
	return "file://" + filepath.ToSlash(openCodeBYOKPluginPath())
}

// openCodeTransportTarget identifies one OpenCode version's proxy wiring target.
type openCodeTransportTarget struct {
	agentID         string
	file            string
	prefix          string
	byoks           []openCodeBYOK
	retrieveStash   string
	transportPlugin bool
}

func openCodeV1TransportTarget() openCodeTransportTarget {
	return openCodeTransportTarget{
		agentID:         "opencode",
		file:            util.OpenCodePathsResolved().Config,
		prefix:          "opencode:",
		byoks:           DiscoverOpenCodeBYOK(),
		retrieveStash:   openCodeRetrieveStatePath(),
		transportPlugin: true,
	}
}

// openCodeV1ConfigTarget returns the v1 target without provider discovery.
func openCodeV1ConfigTarget() openCodeTransportTarget {
	return openCodeTransportTarget{
		agentID:         "opencode",
		file:            util.OpenCodePathsResolved().Config,
		prefix:          "opencode:",
		retrieveStash:   openCodeRetrieveStatePath(),
		transportPlugin: true,
	}
}

func openCodeTransportPluginEntryFor(target openCodeTransportTarget) []any {
	options := util.NewOrderedMap()
	proxyURL := strings.TrimSuffix(ProxyEndpointFor(target.agentID), "/v1")
	routes := util.NewOrderedMap()
	upstreams := util.NewOrderedMap()
	for _, provider := range target.byoks {
		if route, ok := util.ReadBYOKRoute(target.prefix + provider.ID); ok {
			routes.Set(provider.ID, util.BYOKRouteHeader(route))
			if provider.BaseURL != "" {
				upstreams.Set(provider.ID, provider.BaseURL)
			}
		}
	}
	options.Set("proxyUrl", proxyURL)
	options.Set("routes", routes)
	options.Set("upstreams", upstreams)
	options.Set("byokGatewayUrl", util.BYOKGatewayEndpoint())
	return []any{openCodeTransportPluginURL(), options}
}

// openCodeTransportPluginEntryMerged keeps the existing routes and upstreams but refreshes the endpoint.
func openCodeTransportPluginEntryMerged(entry []any, existing any) ([]any, bool) {
	fresh, ok := entry[1].(*util.OrderedMap)
	if !ok {
		return entry, true
	}
	old, ok := existing.([]any)
	if !ok || len(old) != 2 {
		return entry, true
	}
	oldOptions, ok := old[1].(*util.OrderedMap)
	if !ok {
		return entry, true
	}
	merged := util.NewOrderedMap()
	proxyURL, _ := fresh.Get("proxyUrl")
	merged.Set("proxyUrl", proxyURL)
	routes, ok := oldOptions.Get("routes")
	if !ok {
		routes, _ = fresh.Get("routes")
	}
	merged.Set("routes", routes)
	upstreams, ok := oldOptions.Get("upstreams")
	if !ok {
		upstreams, _ = fresh.Get("upstreams")
	}
	merged.Set("upstreams", upstreams)
	gateway, _ := fresh.Get("byokGatewayUrl")
	merged.Set("byokGatewayUrl", gateway)
	oldProxy, _ := oldOptions.Get("proxyUrl")
	oldGateway, _ := oldOptions.Get("byokGatewayUrl")
	return []any{entry[0], merged}, oldProxy != proxyURL || oldGateway != gateway
}

func isOpenCodeTransportPluginPathEntry(v any) bool {
	entry, ok := v.([]any)
	if !ok || len(entry) != 2 {
		return false
	}
	path, _ := entry[0].(string)
	return path == openCodeTransportPluginURL()
}

func isOpenCodeTransportPluginEntry(v any) bool {
	return isOpenCodeTransportPluginEntryFor(openCodeV1ConfigTarget(), v)
}

func isOpenCodeTransportPluginEntryFor(target openCodeTransportTarget, v any) bool {
	if !isOpenCodeTransportPluginPathEntry(v) {
		return false
	}
	entry := v.([]any)
	options, ok := entry[1].(*util.OrderedMap)
	if !ok {
		return false
	}
	proxyURL, _ := options.Get("proxyUrl")
	return proxyURL == util.BYOKGatewayEndpoint() || proxyURL == strings.TrimSuffix(ProxyEndpointFor(target.agentID), "/v1")
}

// openCodeTransportEntryHasRoutes reports whether a managed plugin entry carries routes or upstreams.
func openCodeTransportEntryHasRoutes(v any) bool {
	entry, ok := v.([]any)
	if !ok || len(entry) != 2 {
		return false
	}
	options, ok := entry[1].(*util.OrderedMap)
	if !ok {
		return false
	}
	for _, key := range []string{"routes", "upstreams"} {
		value, ok := options.Get(key)
		if !ok {
			continue
		}
		if m, ok := value.(*util.OrderedMap); ok && m.Len() > 0 {
			return true
		}
	}
	return false
}

// openCodeTransportPluginStillReferenced reports whether another OpenCode config still points at the shared plugin JS.
func openCodeTransportPluginStillReferenced(target openCodeTransportTarget) bool {
	for _, path := range append(openCodeConfigFiles(), openCodeV2ConfigFiles()...) {
		if path == target.file {
			continue
		}
		raw, ok := util.ReadFileSafe(path)
		if !ok {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		plugins, ok := cfg.Get("plugin")
		if !ok {
			continue
		}
		entries, ok := plugins.([]any)
		if !ok {
			continue
		}
		for _, entry := range entries {
			if isOpenCodeTransportPluginPathEntry(entry) {
				return true
			}
		}
	}
	return false
}

func configureOpenCodeTransportPlugin() (changed bool, file string) {
	return configureOpenCodeTransportPluginIn(openCodeV1TransportTarget())
}

func configureOpenCodeTransportPluginIn(target openCodeTransportTarget) (changed bool, file string) {
	file = target.file
	if target.transportPlugin {
		if err := util.EnsureDir(filepath.Dir(openCodeBYOKPluginPath())); err != nil {
			return false, file
		}
		if !util.Exists(openCodeTransportPluginPath()) {
			return false, file
		}
	}
	pluginExisted := util.Exists(openCodeBYOKPluginPath())
	committed := false
	defer func() {
		if !committed && !pluginExisted {
			_ = os.Remove(openCodeBYOKPluginPath())
		}
	}()
	if target.transportPlugin {
		if err := util.WriteFile(openCodeBYOKPluginPath(), openCodeBYOKPluginSource()); err != nil {
			return false, file
		}
	}
	raw, exists := util.ReadFileSafe(file)
	if !exists && util.Exists(file) {
		return false, file
	}
	retrieveStateRaw, retrieveStateExists := util.ReadFileSafe(target.retrieveStash)
	if util.HasJSONCComments(raw) {
		return false, file
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if strings.TrimSpace(raw) != "" {
			return false, file
		}
		cfg = util.NewOrderedMap()
	}
	tools, ok := mapChild(cfg, "tools")
	if !ok {
		if _, exists := cfg.Get("tools"); exists {
			_ = restoreOpenCodeRetrieveStateAt(target.retrieveStash, retrieveStateRaw, retrieveStateExists)
			return false, file
		}
		tools = util.NewOrderedMap()
		cfg.Set("tools", tools)
	}
	if value, ok := tools.Get("headroom_retrieve"); !ok || value != false {
		var previous any
		if value, ok := tools.Get("headroom_retrieve"); ok {
			previous = value
		}
		if err := saveOpenCodeRetrieveStateAt(target.retrieveStash, previous); err != nil {
			return false, file
		}
		tools.Set("headroom_retrieve", false)
		changed = true
	}
	entry := openCodeTransportPluginEntryFor(target)
	plugins, ok := cfg.Get("plugin")
	if !target.transportPlugin {
		if ok {
			entries, ok := plugins.([]any)
			if !ok {
				_ = restoreOpenCodeRetrieveStateAt(target.retrieveStash, retrieveStateRaw, retrieveStateExists)
				return false, file
			}
			next, removed := openCodeTransportEntriesWithoutManaged(entries)
			if removed {
				if len(next) == 0 {
					cfg.Delete("plugin")
				} else {
					cfg.Set("plugin", next)
				}
				changed = true
			}
		}
	} else if ok {
		entries, ok := plugins.([]any)
		if !ok {
			_ = restoreOpenCodeRetrieveStateAt(target.retrieveStash, retrieveStateRaw, retrieveStateExists)
			return false, file
		}
		next := make([]any, 0, len(entries)+1)
		managed := false
		for _, existing := range entries {
			if !isOpenCodeTransportPluginPathEntry(existing) {
				next = append(next, existing)
				continue
			}
			if managed {
				changed = true
				continue
			}
			current := isOpenCodeTransportPluginEntryFor(target, existing)
			if !current {
				changed = true
			}
			if openCodeTransportEntryHasRoutes(existing) && !openCodeTransportEntryHasRoutes(entry) {
				merged, refreshed := openCodeTransportPluginEntryMerged(entry, existing)
				next = append(next, merged)
				if refreshed {
					changed = true
				}
				managed = true
				continue
			}
			next = append(next, entry)
			managed = true
		}
		if !managed {
			next = append(next, entry)
			changed = true
		}
		cfg.Set("plugin", next)
	} else {
		cfg.Set("plugin", []any{entry})
		changed = true
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://opencode.ai/config.json")
	}
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		_ = restoreOpenCodeRetrieveStateAt(target.retrieveStash, retrieveStateRaw, retrieveStateExists)
		return false, file
	}
	committed = true
	return changed, file
}

func removeOpenCodeTransportPlugin() bool {
	return removeOpenCodeTransportPluginIn(openCodeV1ConfigTarget())
}

// openCodeTransportEntriesWithoutManaged drops the managed transport plugin entries.
func openCodeTransportEntriesWithoutManaged(entries []any) (next []any, removed bool) {
	next = make([]any, 0, len(entries))
	for _, entry := range entries {
		if isOpenCodeTransportPluginPathEntry(entry) {
			removed = true
			continue
		}
		next = append(next, entry)
	}
	return next, removed
}

// openCodeRemoveRetrieveSuppression drops the retrieve-tool flag from a config.
func openCodeRemoveRetrieveSuppression(cfg *util.OrderedMap) {
	tools, _ := mapChild(cfg, "tools")
	if tools == nil {
		return
	}
	tools.Delete("headroom_retrieve")
	if tools.Len() == 0 {
		cfg.Delete("tools")
	}
}

func removeOpenCodeTransportPluginIn(target openCodeTransportTarget) bool {
	file := target.file
	raw, ok := util.ReadFileSafe(file)
	if !ok || util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	entries := []any(nil)
	if plugins, ok := cfg.Get("plugin"); ok {
		loaded, ok := plugins.([]any)
		if !ok {
			return false
		}
		entries = loaded
	}
	next, removed := openCodeTransportEntriesWithoutManaged(entries)
	if !removed && target.transportPlugin {
		return false
	}
	if _, exists := util.ReadFileSafe(target.retrieveStash); exists {
		if _, ok := loadOpenCodeRetrieveStateAt(target.retrieveStash); !ok {
			return false
		}
	}
	value, hasRetrieveState := loadOpenCodeRetrieveStateAt(target.retrieveStash)
	suppressed := openCodeRetrieveSuppressed(cfg)
	if !removed && !hasRetrieveState && !suppressed {
		return false
	}
	if hasRetrieveState {
		if value == nil {
			openCodeRemoveRetrieveSuppression(cfg)
		} else {
			tools := getOrCreateMap(cfg, "tools")
			tools.Set("headroom_retrieve", value)
		}
	} else if suppressed {
		openCodeRemoveRetrieveSuppression(cfg)
	}
	if removed {
		if len(next) == 0 {
			cfg.Delete("plugin")
		} else {
			cfg.Set("plugin", next)
		}
	}
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		return false
	}
	if hasRetrieveState {
		if err := clearOpenCodeRetrieveStateAt(target.retrieveStash); err != nil {
			_ = util.WriteFile(file, raw)
			return false
		}
	}
	if target.transportPlugin && !openCodeTransportPluginStillReferenced(target) {
		if err := os.Remove(openCodeBYOKPluginPath()); err != nil && !os.IsNotExist(err) {
			_ = util.WriteFile(file, raw)
			return false
		}
	}
	return true
}

func restoreOpenCodeRetrieveState(raw string, exists bool) error {
	return restoreOpenCodeRetrieveStateAt(openCodeRetrieveStatePath(), raw, exists)
}

func restoreOpenCodeRetrieveStateAt(path, raw string, exists bool) error {
	if exists {
		return util.WriteFileMode(path, raw, 0o600)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func openCodeRetrieveStatePath() string {
	return openCodeRetrieveStatePathFor("opencode")
}

// openCodeRetrieveStatePathFor returns the retrieve-tool stash path of one OpenCode version.
func openCodeRetrieveStatePathFor(agent string) string {
	return filepath.Join(util.HeadroomPathsResolved().Root, agent+".retrieve.stash.json")
}

func saveOpenCodeRetrieveState(value any) error {
	return saveOpenCodeRetrieveStateAt(openCodeRetrieveStatePath(), value)
}

func saveOpenCodeRetrieveStateAt(path string, value any) error {
	b, err := json.Marshal(struct {
		Present bool `json:"present"`
		Value   any  `json:"value"`
	}{Present: value != nil, Value: value})
	if err != nil {
		return err
	}
	return util.WriteFileMode(path, string(b), 0o600)
}

func loadOpenCodeRetrieveState() (any, bool) {
	return loadOpenCodeRetrieveStateAt(openCodeRetrieveStatePath())
}

func loadOpenCodeRetrieveStateAt(path string) (any, bool) {
	raw, ok := util.ReadFileSafe(path)
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
	return clearOpenCodeRetrieveStateAt(openCodeRetrieveStatePath())
}

func clearOpenCodeRetrieveStateAt(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func openCodeTransportPluginWired() bool {
	return openCodeTransportPluginWiredIn(openCodeV1ConfigTarget())
}

func openCodeTransportPluginWiredIn(target openCodeTransportTarget) bool {
	raw, ok := util.ReadFileSafe(target.file)
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
		if isOpenCodeTransportPluginEntryFor(target, entry) {
			return true
		}
	}
	return false
}

func openCodeRetrieveToolDisabled() bool {
	return openCodeRetrieveToolDisabledIn(openCodeV1ConfigTarget())
}

func openCodeRetrieveToolDisabledIn(target openCodeTransportTarget) bool {
	raw, ok := util.ReadFileSafe(target.file)
	if !ok || util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	return openCodeRetrieveSuppressed(cfg)
}

// openCodeRetrieveSuppressed reports whether a config disables the retrieve tool.
func openCodeRetrieveSuppressed(cfg *util.OrderedMap) bool {
	tools, ok := mapChild(cfg, "tools")
	if !ok {
		return false
	}
	value, ok := tools.Get("headroom_retrieve")
	return ok && value == false
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
		filepath.Join(util.Home(), ".opencode2", "bin"),
	}
	for _, name := range []string{"opencode", "opencode2"} {
		if p := util.FindBinary(name, nil); p != "" {
			dirs = append(dirs, filepath.Dir(p))
		}
	}
	dirs = append(dirs, filepath.Join(util.Home(), ".local", "bin"))
	if util.IsWin {
		dirs = append(dirs, filepath.Join(util.Home(), "scoop", "shims"))
		if pd := os.Getenv("ProgramData"); pd != "" {
			dirs = append(dirs, filepath.Join(pd, "chocolatey", "bin"))
		}
	}
	return dirs
}

var openCodeVersionPattern = regexp.MustCompile(`(?:^|\s)v?(\d+)\.`)

// OpenCodeMajorVersion returns the major version reported by <bin> --version.
func OpenCodeMajorVersion(bin string) int {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return 0
	}
	m := openCodeVersionPattern.FindStringSubmatch(strings.TrimSpace(string(out)))
	if len(m) != 2 {
		return 0
	}
	var major int
	if _, err := fmt.Sscanf(m[1], "%d", &major); err != nil {
		return 0
	}
	return major
}

// openCodeBinaryPaths returns real binary candidates, including the v2 install path and PATH.
func openCodeBinaryPaths() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	names := []string{"opencode2", "opencode"}
	if util.IsWin {
		names = []string{"opencode2.exe", "opencode.exe", "opencode2", "opencode"}
	}
	for _, dir := range opencodeKnownBinDirs() {
		for _, name := range names {
			add(filepath.Join(dir, name))
		}
	}
	for _, name := range []string{"opencode2", "opencode"} {
		add(util.FindBinary(name, nil))
	}
	return out
}

// OpenCodeInstalledMajor probes installed binaries and returns 0 if none reports a version.
func OpenCodeInstalledMajor() int {
	for _, bin := range openCodeBinaryPaths() {
		if major := OpenCodeMajorVersion(bin); major != 0 {
			return major
		}
	}
	return 0
}

func OpenCodeInstalledMajors() []int {
	var majors []int
	seen := map[int]bool{}
	for _, bin := range openCodeBinaryPaths() {
		if major := OpenCodeMajorVersion(bin); major != 0 && !seen[major] {
			seen[major] = true
			majors = append(majors, major)
		}
	}
	return majors
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
		bins := opencodeKnownBinDirs()
		majors := OpenCodeInstalledMajors()
		if containsInt(majors, 1) {
			return core.Detection{Installed: true, Source: "cli"}
		}
		if len(majors) > 0 {
			return core.Detection{Installed: false, Source: ""}
		}
		return detectAgent("opencode", util.OpenCodePathsResolved().Dir, bins, opencodeDesktopPaths())
	},
}

var opencode2 = &core.AgentManifest{
	ID:        "opencode2",
	Label:     "OpenCode v2",
	Homepage:  "https://github.com/anomalyco/opencode",
	CLIBin:    "opencode2",
	ConfigDir: func() string { return util.OpenCodePathsV2().Dir },
	Detect: func() core.Detection {
		for _, major := range OpenCodeInstalledMajors() {
			if major == 2 {
				return core.Detection{Installed: true, Source: "cli"}
			}
		}
		return core.Detection{Installed: false, Source: ""}
	},
}

func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
