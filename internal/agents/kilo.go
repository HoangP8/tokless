package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func kiloProjectDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// KiloProjectFile resolves a path inside a legacy project .kilo directory.
func KiloProjectFile(name ...string) string {
	root := kiloProjectDir()
	if root == "" {
		return ""
	}
	parts := append([]string{root, ".kilo"}, name...)
	return filepath.Join(parts...)
}

func KiloInstructionsPath() string {
	return filepath.Join(util.KiloPathsResolved().Dir, "AGENTS.md")
}

// ConfigureKiloMcpSafe wires Tokless MCP in Kilo's global config only; it
// never creates or modifies a project .kilo.
func ConfigureKiloMcpSafe(toolID string, command []string) (bool, string, error) {
	if toolID == "headroom" {
		return false, util.KiloPathsResolved().Config, fmt.Errorf("Headroom is HTTP proxy-only; MCP wiring is disabled")
	}
	if !kiloExpectedCommand(toolID, command) {
		return false, util.KiloPathsResolved().Config, fmt.Errorf("unrecognized Tokless MCP command for Kilo tool %q; refusing to adopt or overwrite entry", toolID)
	}
	p := util.KiloPathsResolved().Config
	raw, exists := util.ReadFileSafe(p)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if exists && strings.TrimSpace(raw) != "" {
			return false, p, fmt.Errorf("cannot parse Kilo global config %s; refusing to overwrite it", p)
		}
		cfg = util.NewOrderedMap()
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://app.kilo.ai/config.json")
	}
	mcp, err := kiloMcpMap(cfg)
	if err != nil {
		return false, p, err
	}
	state, stateExists, err := kiloStateRead()
	if err != nil {
		return false, p, err
	}
	if existing, ok := mcp.Get(toolID); ok {
		owned, hasOwner := state[toolID]
		if !stateExists || !hasOwner {
			return false, p, fmt.Errorf("Kilo global MCP %q has no Tokless ownership state; refusing same-name adoption", toolID)
		}
		if !kiloManagedMcpEntry(existing, owned) || !kiloExpectedCommand(toolID, owned) {
			return false, p, fmt.Errorf("Kilo global MCP %q differs from Tokless ownership state; restore the owned entry or remove it manually", toolID)
		}
		if kiloManagedMcpEntry(existing, command) {
			return false, p, nil
		}
	}
	if stateExists {
		if owned, ok := state[toolID]; ok && !stringSlicesEqual(owned, command) {
			return false, p, fmt.Errorf("Kilo global MCP %q ownership state differs from requested command", toolID)
		}
	}
	if exists && hasJSONCComment(raw) {
		return false, p, fmt.Errorf("Kilo global config %s contains JSONC comments; edit it without relocating comments before wiring Tokless", p)
	}
	desired := util.NewOrderedMap()
	desired.Set("type", "local")
	desired.Set("command", toAny(command))
	desired.Set("enabled", true)
	mcp.Set(toolID, desired)
	content := util.StringifyJSON(cfg)
	if err := kiloStateSet(toolID, command); err != nil {
		return false, p, err
	}
	if err := writeKiloFileGuarded(p, content, raw, exists); err != nil {
		_ = kiloStateDelete(toolID)
		return false, p, err
	}
	return true, p, nil
}

func RemoveKiloMcp(toolID string) bool {
	p := util.KiloPathsResolved().Config
	state, stateExists, err := kiloStateRead()
	if err != nil || !stateExists {
		return false
	}
	if _, owned := state[toolID]; !owned {
		return false
	}
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return kiloStateDelete(toolID) == nil
	}
	if hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcp")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return kiloStateDelete(toolID) == nil
	}
	entry, ok := mcp.Get(toolID)
	if !ok {
		return kiloStateDelete(toolID) == nil
	}
	if !kiloManagedMcpEntry(entry, state[toolID]) {
		return false
	}
	mcp.Delete(toolID)
	if mcp.Len() == 0 {
		cfg.Delete("mcp")
	}
	if writeKiloConfig(p, raw, true, cfg) != nil {
		return false
	}
	return kiloStateDelete(toolID) == nil
}

func KiloMcpConfigured(toolID string) bool {
	p := util.KiloPathsResolved().Config
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcp")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	entry, ok := mcp.Get(toolID)
	if !ok {
		return false
	}
	em, ok := entry.(*util.OrderedMap)
	if !ok {
		return false
	}
	t, tok := em.Get("type")
	e, eok := em.Get("enabled")
	c, cok := em.Get("command")
	command, cok2 := c.([]any)
	return tok && t == "local" && eok && e == true && cok && cok2 && len(command) > 0 && allStringsNonempty(command)
}

func KiloMcpMatches(toolID string, want []string) bool {
	p := util.KiloPathsResolved().Config
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcp")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	entry, ok := mcp.Get(toolID)
	if !ok || !KiloMcpConfigured(toolID) {
		return false
	}
	em, ok := entry.(*util.OrderedMap)
	if !ok {
		return false
	}
	c, ok := em.Get("command")
	return ok && anyStringSliceEqual(c, want)
}

func allStringsNonempty(values []any) bool {
	for _, value := range values {
		s, ok := value.(string)
		if !ok || s == "" {
			return false
		}
	}
	return true
}

func kiloExpectedCommand(toolID string, command []string) bool {
	if len(command) < 2 || !kiloIsToklessCommand(command[0]) || command[1] != "run-mcp" {
		return false
	}
	switch toolID {
	case "context-mode":
		return len(command) >= 4 && command[2] == "--context-mode" && kiloContextServer(command[3:])
	case "codegraph":
		return len(command) >= 7 && command[2] == "--agent" && command[3] == "kilo" && kiloCodegraphServer(command[4:])
	default:
		return false
	}
}

func kiloContextServer(command []string) bool {
	if len(command) == 1 {
		return kiloCommandBase(command[0]) == "context-mode"
	}
	if len(command) == 3 && kiloCommandBase(command[0]) == "npx" && command[1] == "--no-install" && command[2] == "context-mode" {
		return true
	}
	if len(command) == 5 && kiloCmdShimServer(command[:3], "npx") && command[3] == "--no-install" && command[4] == "context-mode" {
		return true
	}
	return len(command) == 3 && kiloCmdShimServer(command, "context-mode")
}

func kiloCodegraphServer(command []string) bool {
	if len(command) == 3 && command[1] == "serve" && command[2] == "--mcp" {
		return kiloCommandBase(command[0]) == "codegraph"
	}
	if len(command) == 5 && kiloCommandBase(command[0]) == "npx" && command[1] == "--no-install" && command[2] == "@colbymchenry/codegraph" && command[3] == "serve" && command[4] == "--mcp" {
		return true
	}
	if len(command) == 7 && kiloCmdShimServer(command[:3], "npx") && command[3] == "--no-install" && command[4] == "@colbymchenry/codegraph" && command[5] == "serve" && command[6] == "--mcp" {
		return true
	}
	return len(command) == 5 && kiloCmdShimServer(command[:3], "codegraph") && command[3] == "serve" && command[4] == "--mcp"
}

func kiloCommandBase(command string) string {
	command = strings.ReplaceAll(command, "\\", "/")
	base := strings.ToLower(filepath.Base(command))
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func kiloCmdShimServer(command []string, server string) bool {
	if len(command) != 3 || kiloCommandBase(command[0]) != "cmd" || command[1] != "/c" || kiloCommandBase(command[2]) != server {
		return false
	}
	ext := strings.ToLower(filepath.Ext(strings.ReplaceAll(command[2], "\\", "/")))
	return ext == ".cmd" || ext == ".bat"
}

func kiloIsToklessCommand(command string) bool {
	return kiloCommandBase(command) == "tokless"
}

// kiloStatePath is the ownership ledger for Kilo global MCP entries.
func kiloStatePath() string {
	return filepath.Join(util.KiloPathsResolved().Dir, ".tokless-kilo-mcp-owners.json")
}

func kiloStateRead() (map[string][]string, bool, error) {
	state, exists, _, err := kiloStateReadRaw()
	if err != nil {
		return nil, exists, err
	}
	return state, exists, nil
}

func kiloStateReadRaw() (map[string][]string, bool, string, error) {
	raw, ok := util.ReadFileSafe(kiloStatePath())
	if !ok {
		return map[string][]string{}, false, "", nil
	}
	var state map[string][]string
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, true, raw, fmt.Errorf("Kilo Tokless ownership state is unreadable; refusing to modify existing MCP entries: %w", err)
	}
	for id, command := range state {
		if len(command) == 0 || !kiloExpectedCommand(id, command) {
			return nil, true, raw, fmt.Errorf("Kilo Tokless ownership state is malformed for %q; refusing to modify existing MCP entries", id)
		}
	}
	return state, true, raw, nil
}

func kiloStateSet(id string, command []string) error {
	state, exists, raw, err := kiloStateReadRaw()
	if err != nil {
		return err
	}
	state[id] = append([]string(nil), command...)
	return writeKiloFileGuarded(kiloStatePath(), util.StringifyJSON(state), raw, exists)
}

func kiloStateDelete(id string) error {
	state, exists, raw, err := kiloStateReadRaw()
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	delete(state, id)
	if len(state) == 0 {
		if err := os.Remove(kiloStatePath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeKiloFileGuarded(kiloStatePath(), util.StringifyJSON(state), raw, exists)
}

func writeKiloConfig(path, raw string, exists bool, cfg *util.OrderedMap) error {
	return writeKiloFileGuarded(path, util.StringifyJSON(cfg), raw, exists)
}

var kiloBeforeReplaceHook func(string)

// Guard catches edits observed between read and replacement.
func writeKiloFileGuarded(path, content, expectedRaw string, expectedExists bool) error {
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if kiloBeforeReplaceHook != nil {
		kiloBeforeReplaceHook(path)
	}
	current, exists := util.ReadFileSafe(path)
	if exists != expectedExists || (exists && current != expectedRaw) {
		return fmt.Errorf("Kilo file %s changed during update; retry", path)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tokless-kilo-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceKiloFile(tmpPath, path)
}

func kiloMcpMap(cfg *util.OrderedMap) (*util.OrderedMap, error) {
	if v, ok := cfg.Get("mcp"); ok {
		if m, ok := v.(*util.OrderedMap); ok {
			return m, nil
		}
		return nil, fmt.Errorf("Kilo config field %q is not an object; refusing to overwrite it", "mcp")
	}
	m := util.NewOrderedMap()
	cfg.Set("mcp", m)
	return m, nil
}

func hasJSONCComment(raw string) bool {
	inString, escaped := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		} else if c == '/' && i+1 < len(raw) && (raw[i+1] == '/' || raw[i+1] == '*') {
			return true
		}
	}
	return false
}

func kiloManagedMcpEntry(v any, command []string) bool {
	m, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	if m.Len() != 3 {
		return false
	}
	t, _ := m.Get("type")
	e, _ := m.Get("enabled")
	c, _ := m.Get("command")
	return t == "local" && e == true && anyStringSliceEqual(c, command)
}

func anyStringSliceEqual(v any, want []string) bool {
	a, ok := v.([]any)
	if !ok || len(a) != len(want) {
		return false
	}
	for i, x := range a {
		if x != want[i] {
			return false
		}
	}
	return true
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func kiloKnownBinDirs() []string {
	return []string{filepath.Join(util.Home(), ".kilo", "bin")}
}

var kilo = &core.AgentManifest{
	ID:        "kilo",
	Label:     "Kilo",
	Homepage:  "https://kilo.ai",
	CLIBin:    "kilo",
	ConfigDir: func() string { return util.KiloPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectVSCodeAgent("kilo", util.KiloPathsResolved().Dir, kiloKnownBinDirs(), "kilocode.kilo-code")
	},
}

// --- Kilo headroom HTTP proxy ---

const kiloProxyProvider = "tokless-headroom"

func kiloProviderRoute(provider *util.OrderedMap) (baseKey, base string, options, headers *util.OrderedMap, ok bool) {
	value, exists := provider.Get("options")
	if !exists {
		return "", "", nil, nil, false
	}
	options, ok = value.(*util.OrderedMap)
	if !ok {
		return "", "", nil, nil, false
	}
	for _, key := range []string{"baseURL", "baseUrl"} {
		if value, exists := options.Get(key); exists {
			base, ok = value.(string)
			if ok && strings.TrimSpace(base) != "" {
				baseKey = key
				break
			}
		}
	}
	if baseKey == "" || !isAbsoluteHTTP(base) {
		return "", "", nil, nil, false
	}
	if value, exists := options.Get("headers"); exists {
		headers, ok = value.(*util.OrderedMap)
		if !ok {
			return "", "", nil, nil, false
		}
	} else {
		headers = util.NewOrderedMap()
	}
	return baseKey, base, options, headers, true
}

func kiloNativeRoutes(cfg *util.OrderedMap, stash map[string]proxyRouteStashEntry) (bool, bool) {
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		return false, false
	}
	routed := map[string]bool{}
	changed, found := false, false
	for _, id := range providers.Keys() {
		if id == kiloProxyProvider {
			continue
		}
		value, exists := providers.Get(id)
		provider, ok := value.(*util.OrderedMap)
		if !exists || !ok {
			continue
		}
		baseKey, base, options, headers, ok := kiloProviderRoute(provider)
		if !ok {
			continue
		}
		upstream := normalizedHeadroomUpstream(base, "openai-completions")
		endpoint := ProxyEndpointFor("kilo")
		if current, exists := headers.Get(headroomBaseURLHeader); exists {
			currentString, currentOK := current.(string)
			if !currentOK || (currentString != upstream && !sameProxyBase(base, endpoint)) {
				continue
			}
		}
		found = true
		if sameProxyBase(base, endpoint) {
			current, exists := headers.Get(headroomBaseURLHeader)
			if !exists || current != upstream {
				continue
			}
			routed[id] = true
			continue
		}
		hadHeader, header := false, ""
		if current, exists := headers.Get(headroomBaseURLHeader); exists {
			currentString, currentOK := current.(string)
			if !currentOK {
				continue
			}
			hadHeader, header = true, currentString
		}
		stash[id] = proxyRouteStashEntry{File: util.KiloPathsResolved().Config, Provider: id, BaseURL: base, Upstream: upstream, BaseKey: baseKey, HadHeader: hadHeader, Header: header}
		routed[id] = true
		options.Set(baseKey, endpoint)
		if _, exists := options.Get("headers"); !exists {
			options.Set("headers", headers)
		}
		headers.Set(headroomBaseURLHeader, upstream)
		changed = true
	}
	for id := range stash {
		if !routed[id] {
			if _, exists := providers.Get(id); !exists {
				delete(stash, id)
			}
		}
	}
	return changed, found
}

// kiloProxyProviderEntry builds the opencode-style provider entry injected
// into provider.<id>.
func kiloProxyProviderEntry(endpoint string) *util.OrderedMap {
	entry := util.NewOrderedMap()
	entry.Set("npm", "@ai-sdk/openai-compatible")
	entry.Set("name", "Headroom Proxy")
	options := util.NewOrderedMap()
	options.Set("baseURL", endpoint)
	if k := proxyWireKey(); k != "tokless" {
		options.Set("apiKey", k)
	}
	entry.Set("options", options)
	model := util.NewOrderedMap()
	model.Set("name", "Headroom")
	model.Set("tool_call", true)
	limit := util.NewOrderedMap()
	limit.Set("context", 128000)
	limit.Set("output", 16384)
	model.Set("limit", limit)
	models := util.NewOrderedMap()
	models.Set(proxyWireModel(), model)
	entry.Set("models", models)
	return entry
}

// ConfigureKiloProxy injects provider.tokless-headroom into kilo.jsonc,
// pointing at the OpenAI-compatible headroom daemon endpoint.
func ConfigureKiloProxy() (changed bool, file string) {
	if err := withProxyRouteStashLock(func() error {
		changed, file = configureKiloProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("kilo proxy lock failed: " + err.Error())
	}
	return changed, file
}

func configureKiloProxyLocked() (changed bool, file string) {
	p := util.KiloPathsResolved()
	file = p.Config
	if !proxyRouteStashValid("kilo") {
		return false, file
	}
	_ = util.EnsureDir(p.Dir)
	raw, exists := util.ReadFileSafe(p.Config)
	if !exists && util.Exists(p.Config) {
		return false, file
	}
	if util.HasJSONCComments(raw) {
		return false, file
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if exists {
			return false, file
		}
		cfg = util.NewOrderedMap()
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		if _, present := cfg.Get("provider"); present {
			return false, file
		}
		providers = util.NewOrderedMap()
		cfg.Set("provider", providers)
	}
	stash := loadProxyRouteStashLocked("kilo")
	stashRaw, stashExists := util.ReadFileSafe(proxyRouteStashPath("kilo"))
	prevStashLen := len(stash)
	if nativeChanged, nativeFound := kiloNativeRoutes(cfg, stash); nativeFound {
		if saveProxyRouteStash("kilo", stash) != nil {
			return false, file
		}
		if !nativeChanged {
			return false, file
		}
		if err := util.WriteFile(p.Config, util.StringifyJSON(cfg)); err != nil {
			restoreProxyRouteStashLogged("kilo", stashRaw, stashExists)
			return false, file
		}
		return true, file
	} else if prevStashLen > 0 {
		if saveProxyRouteStash("kilo", stash) != nil {
			return false, file
		}
		return false, file
	}
	desired := kiloProxyProviderEntry(ProxyEndpointFor("kilo"))
	if existing, ok := providers.Get(kiloProxyProvider); ok {
		if jsonEqual(existing, desired) {
			return false, file
		}
		return false, file
	}
	providers.Set(kiloProxyProvider, desired)
	if err := util.WriteFile(p.Config, util.StringifyJSON(cfg)); err != nil {
		return false, file
	}
	return true, file
}

// RemoveKiloProxy deletes provider.tokless-headroom only while its value still
// equals what tokless injected.
func RemoveKiloProxy() bool {
	removed := false
	if err := withProxyRouteStashLock(func() error {
		removed = removeKiloProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("kilo proxy lock failed: " + err.Error())
	}
	return removed
}

func removeKiloProxyLocked() bool {
	if !proxyRouteStashValid("kilo") {
		return false
	}
	p := util.KiloPathsResolved()
	stash := loadProxyRouteStashLocked("kilo")
	if len(stash) > 0 {
		raw, ok := util.ReadFileSafe(p.Config)
		if !ok || util.HasJSONCComments(raw) {
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
		remaining := map[string]proxyRouteStashEntry{}
		original := raw
		changed := false
		for id, entry := range stash {
			value, exists := providers.Get(id)
			provider, providerOK := value.(*util.OrderedMap)
			if !exists || !providerOK {
				remaining[id] = entry
				continue
			}
			baseKey, base, _, headers, routeOK := kiloProviderRoute(provider)
			if !routeOK || !sameProxyBase(base, ProxyEndpointFor("kilo")) {
				remaining[id] = entry
				continue
			}
			value, exists = headers.Get(headroomBaseURLHeader)
			if !exists || value != entry.Upstream {
				remaining[id] = entry
				continue
			}
			providerOptions, _ := provider.Get("options")
			options, _ := providerOptions.(*util.OrderedMap)
			options.Set(baseKey, entry.BaseURL)
			if entry.HadHeader {
				headers.Set(headroomBaseURLHeader, entry.Header)
			} else {
				headers.Delete(headroomBaseURLHeader)
			}
			if headers.Len() == 0 {
				options.Delete("headers")
			}
			changed = true
		}
		if !changed || util.WriteFile(p.Config, util.StringifyJSON(cfg)) != nil {
			return false
		}
		if err := saveProxyRouteStash("kilo", remaining); err != nil {
			if restoreErr := util.WriteFile(p.Config, original); restoreErr != nil {
				util.L.Err("kilo proxy rollback failed: " + restoreErr.Error())
			}
			util.L.Err("kilo proxy stash update failed: " + err.Error())
			return false
		}
		return true
	}
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
	existing, ok := providers.Get(kiloProxyProvider)
	if !ok || !jsonEqual(existing, kiloProxyProviderEntry(ProxyEndpointFor("kilo"))) {
		return false
	}
	providers.Delete(kiloProxyProvider)
	if providers.Len() == 0 {
		cfg.Delete("provider")
	}
	return util.WriteFile(p.Config, util.StringifyJSON(cfg)) == nil
}

// KiloProxyWired reports whether provider.tokless-headroom points at the
// headroom daemon endpoint.
func KiloProxyWired() bool {
	raw, ok := util.ReadFileSafe(util.KiloPathsResolved().Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if stash := loadProxyRouteStash("kilo"); len(stash) > 0 {
		providers, ok := mapChild(cfg, "provider")
		if !ok {
			return false
		}
		for id, entry := range stash {
			value, exists := providers.Get(id)
			provider, providerOK := value.(*util.OrderedMap)
			if !exists || !providerOK {
				return false
			}
			_, base, _, headers, routeOK := kiloProviderRoute(provider)
			upstream, headerOK := headers.Get(headroomBaseURLHeader)
			if !routeOK || !sameProxyBase(base, ProxyEndpointFor("kilo")) || !headerOK || upstream != entry.Upstream {
				return false
			}
		}
		return true
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		return false
	}
	existing, ok := providers.Get(kiloProxyProvider)
	return ok && jsonEqual(existing, kiloProxyProviderEntry(ProxyEndpointFor("kilo")))
}

// jsonEqual compares two JSON values by canonical form.
func jsonEqual(a, b any) bool {
	return canonicalJSON(a) == canonicalJSON(b)
}

func canonicalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var m any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return string(b)
	}
	b2, _ := json.Marshal(m)
	return string(b2)
}
