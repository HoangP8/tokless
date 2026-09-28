package agents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// kiloConfigFiles lists kilo global configs in load order.
func kiloConfigFiles() []string {
	dir := util.KiloPathsResolved().Dir
	return []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "kilo.json"),
		filepath.Join(dir, "kilo.jsonc"),
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, "opencode.jsonc"),
	}
}

// DiscoverKiloBYOK finds user providers with a real upstream + credential.
func DiscoverKiloBYOK() []openCodeBYOK {
	proxyBase := strings.TrimRight(ProxyEndpointFor("kilo"), "/")
	gatewayBase := strings.TrimRight(util.BYOKGatewayEndpoint(), "/")
	type acc struct {
		base, key, file, npm string
	}
	got := map[string]*acc{}
	duplicates := map[string]bool{}
	order := []string{}

	for _, path := range kiloConfigFiles() {
		raw, ok := util.ReadFileSafe(path)
		if !ok || util.HasJSONCComments(raw) {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		providers, ok := mapChild(cfg, "provider")
		if !ok {
			continue
		}
		for _, id := range providers.Keys() {
			block, ok := providers.Get(id)
			if !ok {
				continue
			}
			m, ok := block.(*util.OrderedMap)
			if !ok {
				continue
			}
			base, key := providerBaseAndKey(m)
			if base == "" && key == "" {
				continue
			}
			a, exists := got[id]
			if !exists {
				a = &acc{}
				got[id] = a
				order = append(order, id)
			} else if (base != "" && a.base != "" && base != a.base) || (key != "" && a.key != "" && key != a.key) {
				duplicates[id] = true
			}
			if key != "" {
				a.key = key
			}
			if npm, ok := m.Get("npm"); ok {
				if s, ok := npm.(string); ok && s != "" {
					a.npm = s
				}
			}
			if base != "" && isValidBYOKUpstream(base) && !sameProxyBase(base, proxyBase) && !sameProxyBase(base, gatewayBase) {
				a.base = base
				a.file = path
			}
		}
	}

	// Fill missing originals from stash (provider already rewritten to proxy).
	stashed := loadProxyRouteStash("kilo")
	var out []openCodeBYOK
	for _, id := range order {
		if duplicates[id] {
			continue
		}
		a := got[id]
		if a.base == "" {
			if s, ok := stashed[id]; ok && s.BaseURL != "" {
				a.base = s.BaseURL
				if a.file == "" {
					a.file = s.File
				}
			}
		}
		if a.base == "" || a.key == "" || a.file == "" {
			continue
		}
		out = append(out, openCodeBYOK{
			ID:       id,
			File:     a.file,
			BaseURL:  a.base,
			APIKey:   a.key,
			Npm:      a.npm,
			Protocol: openCodeProtocol(a.npm),
		})
	}
	return out
}

// kiloTransportPluginTarget picks the config carrying the plugin entry:
// highest-precedence with a plugin array, else highest readable, else kilo.jsonc.
func kiloTransportPluginTarget() string {
	files := kiloConfigFiles()
	for i := len(files) - 1; i >= 0; i-- {
		raw, ok := util.ReadFileSafe(files[i])
		if !ok {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		if _, has := cfg.Get("plugin"); has {
			return files[i]
		}
	}
	for i := len(files) - 1; i >= 0; i-- {
		raw, ok := util.ReadFileSafe(files[i])
		if !ok || util.HasJSONCComments(raw) {
			continue
		}
		return files[i]
	}
	return util.KiloPathsResolved().Config
}

// kiloTransportPluginPath: generated wrapper; options embedded since kilo's
// plugin schema is string-only and passes a context object to exports.
func kiloTransportPluginPath() string {
	return filepath.Join(filepath.Dir(openCodeBYOKPluginPath()), "tokless-byok.kilo.js")
}

func kiloTransportPluginURL() string {
	return "file://" + filepath.ToSlash(kiloTransportPluginPath())
}

// kiloTransportPluginEntry: raw path on Windows (file://C:/ parses as UNC
// hostname), file:// URL elsewhere.
func kiloTransportPluginEntry() string {
	if util.IsWin {
		return filepath.ToSlash(kiloTransportPluginPath())
	}
	return kiloTransportPluginURL()
}

// isKiloTransportPluginEntry matches the current platform entry, the file://
// form (unix self-heal), and the legacy tuple form so it can be rewritten.
func isKiloTransportPluginEntry(v any) bool {
	switch entry := v.(type) {
	case string:
		return entry == kiloTransportPluginEntry() || entry == kiloTransportPluginURL()
	case []any:
		return isOpenCodeTransportPluginPathEntry(entry)
	default:
		return false
	}
}

func kiloTransportPluginOptions() *util.OrderedMap {
	options := util.NewOrderedMap()
	proxyURL := strings.TrimSuffix(ProxyEndpointFor("kilo"), "/v1")
	routes := util.NewOrderedMap()
	upstreams := util.NewOrderedMap()
	for _, provider := range DiscoverKiloBYOK() {
		if route, ok := util.ReadBYOKRoute("kilo:" + provider.ID); ok {
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
	return options
}

func kiloTransportPluginWrapper() string {
	var b strings.Builder
	b.WriteString("import ToklessBYOKPlugin from \"./tokless-byok.js\";\n\n")
	b.WriteString("const OPTIONS = ")
	b.WriteString(util.StringifyJSON(kiloTransportPluginOptions()))
	b.WriteString(";\n\nexport default async function (input) {\n  return ToklessBYOKPlugin(input, OPTIONS);\n}\n")
	return b.String()
}

func kiloTransportPluginWired() bool {
	raw, ok := util.ReadFileSafe(kiloTransportPluginTarget())
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
		if s, ok := entry.(string); ok && (s == kiloTransportPluginEntry() || s == kiloTransportPluginURL()) {
			return util.Exists(kiloTransportPluginPath())
		}
	}
	return false
}

func configureKiloTransportPlugin() (bool, string) {
	file := kiloTransportPluginTarget()
	if err := util.EnsureDir(filepath.Dir(openCodeBYOKPluginPath())); err != nil {
		return false, file
	}
	if !util.Exists(openCodeTransportPluginPath()) {
		return false, file
	}
	pluginPath := kiloTransportPluginPath()
	wrapper := kiloTransportPluginWrapper()
	wrapperExisted := util.Exists(pluginPath)
	committed := false
	defer func() {
		if !committed && !wrapperExisted {
			_ = os.Remove(pluginPath)
		}
	}()
	changed := false
	if raw, ok := util.ReadFileSafe(pluginPath); !ok || raw != wrapper {
		if err := util.WriteFile(pluginPath, wrapper); err != nil {
			return false, file
		}
		changed = true
	}
	raw, exists := util.ReadFileSafe(file)
	if !exists && util.Exists(file) {
		return false, file
	}
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
	plugins, ok := cfg.Get("plugin")
	if ok {
		entries, ok := plugins.([]any)
		if !ok {
			return false, file
		}
		next := make([]any, 0, len(entries)+1)
		managed := false
		for _, entry := range entries {
			if !isKiloTransportPluginEntry(entry) {
				next = append(next, entry)
				continue
			}
			if managed {
				changed = true
				continue
			}
			if _, isString := entry.(string); !isString {
				changed = true
			}
			next = append(next, kiloTransportPluginEntry())
			managed = true
		}
		if !managed {
			next = append(next, kiloTransportPluginEntry())
			changed = true
		}
		cfg.Set("plugin", next)
	} else {
		cfg.Set("plugin", []any{kiloTransportPluginEntry()})
		changed = true
	}
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		return false, file
	}
	committed = true
	return changed, file
}

func removeKiloTransportPlugin() bool {
	file := kiloTransportPluginTarget()
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
		if isKiloTransportPluginEntry(entry) {
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
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		return false
	}
	if err := os.Remove(kiloTransportPluginPath()); err != nil && !os.IsNotExist(err) {
		_ = util.WriteFile(file, raw)
		return false
	}
	return true
}

// configureKiloPluginWiring registers kilo:<id> BYOK routes then writes the
// transport plugin entry; routes roll back if wiring fails (stash lock held).
func configureKiloPluginWiring() (changed bool, file string, resultErr error) {
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
					errs = append(errs, fmt.Errorf("delete kilo route %s: %w", route.id, err))
				}
				continue
			}
			if _, _, err := util.UpsertBYOKRoute(route.id, route.previous.Protocol, route.previous.Upstream); err != nil {
				errs = append(errs, fmt.Errorf("restore kilo route %s: %w", route.id, err))
			}
		}
		return errors.Join(errs...)
	}
	for _, provider := range DiscoverKiloBYOK() {
		if provider.Protocol == "" {
			continue
		}
		id := "kilo:" + provider.ID
		_, previous, err := util.UpsertBYOKRoute(id, provider.Protocol, provider.BaseURL)
		if err != nil {
			return false, "", errors.Join(err, rollbackRoutes())
		}
		routeChanges = append(routeChanges, routeChange{id: id, previous: previous})
	}
	pluginChanged, file := configureKiloTransportPlugin()
	if !pluginChanged && !kiloTransportPluginWired() {
		return false, file, errors.Join(fmt.Errorf("configure kilo transport plugin"), rollbackRoutes())
	}
	return pluginChanged, file, nil
}
