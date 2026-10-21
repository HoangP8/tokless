package agents

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// openCodeBYOK is one user-defined provider that can ride the headroom proxy.
type openCodeBYOK struct {
	ID          string
	File        string
	BaseURL     string
	APIKey      string
	Npm         string
	Protocol    string
	RouteHeader string
	Native      bool
	Name        string
	RawAPIKey   string
}

// openCodeConfigFiles returns every global OpenCode config that may hold providers.
func openCodeConfigFiles() []string {
	dir := util.OpenCodePathsResolved().Dir
	paths := []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, "opencode.jsonc"),
	}
	custom := util.OpenCodePathsResolved().Config
	for _, path := range paths {
		if path == custom {
			return paths
		}
	}
	return append(paths, custom)
}

// DiscoverOpenCodeBYOK finds user providers with a real upstream + credential.
func DiscoverOpenCodeBYOK() []openCodeBYOK {
	return discoverOpenCodeBYOKIn(openCodeConfigFiles(), "opencode:", byokStashPath())
}

// discoverOpenCodeBYOKIn discovers providers in one OpenCode version.
func discoverOpenCodeBYOKIn(files []string, prefix string, stashPath string) []openCodeBYOK {
	proxyBase := strings.TrimRight(ProxyEndpointFor(strings.TrimSuffix(prefix, ":")), "/")
	type acc struct {
		base, key, raw, file, npm string
	}
	got := map[string]*acc{}
	duplicates := map[string]bool{}
	order := []string{}

	for _, path := range files {
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
			base, rawKey := providerBaseAndRawKey(m)
			key := resolveAPIKey(rawKey)
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
				a.raw = rawKey
			}
			if npm, ok := m.Get("npm"); ok {
				if s, ok := npm.(string); ok && s != "" {
					a.npm = s
				}
			}
			if base != "" && isValidBYOKUpstream(base) && !sameProxyBase(base, proxyBase) {
				a.base = base
				a.file = path
			}
		}
	}

	stashed := loadBYOKStashAt(stashPath)
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
		routeHeader := ""
		if route, ok := util.ReadBYOKRoute(prefix + id); ok {
			routeHeader = util.BYOKRouteHeader(route)
		}
		out = append(out, openCodeBYOK{
			ID:          id,
			File:        a.file,
			BaseURL:     a.base,
			APIKey:      a.key,
			RawAPIKey:   a.raw,
			Npm:         a.npm,
			Protocol:    openCodeProtocol(a.npm),
			RouteHeader: routeHeader,
		})
	}
	return out
}

// openCodeSkip is a provider that was not wired, with the reason why.
type openCodeSkip struct {
	ID string

	Reason string
}

// openCodeSkipMessages renders the skipped providers for a log line.
func openCodeSkipMessages(skipped []openCodeSkip) []string {
	out := make([]string, 0, len(skipped))
	for _, s := range skipped {
		if s.ID == "" {
			out = append(out, s.Reason)
			continue
		}
		out = append(out, s.ID+": "+s.Reason)
	}
	return out
}

// discoverOpenCodeV2BYOKIn discovers providers in the native OpenCode v2 shape, so a v2-only config can ride the proxy.
func discoverOpenCodeV2BYOKIn(files []string, prefix string, stashPath string) ([]openCodeBYOK, []openCodeSkip) {
	proxyBase := openCodeProxyEndpointFor(prefix)
	stashed := loadBYOKStashAt(stashPath)
	var out []openCodeBYOK
	var skipped []openCodeSkip
	for _, path := range files {
		raw, ok := util.ReadFileSafe(path)
		if !ok {
			continue
		}
		if util.HasJSONCComments(raw) {
			skipped = append(skipped, openCodeSkip{Reason: "comment-bearing config " + path})
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		providers, ok := mapChild(cfg, "providers")
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
			npm := openCodeV2ProviderNpm(m)
			if npm == "" {
				skipped = append(skipped, openCodeSkip{ID: id, Reason: "unknown package"})
				continue
			}
			base, key := openCodeV2ProviderBaseAndKey(m)
			stashedBase := ""
			if s, ok := stashed[id]; ok {
				stashedBase = s.BaseURL
			}
			upstream := openCodeV2ProviderUpstream(m, base, proxyBase, stashedBase)
			if !isValidBYOKUpstream(upstream) {
				upstream = ""
			}
			if upstream == "" {
				skipped = append(skipped, openCodeSkip{ID: id, Reason: "no upstream"})
				continue
			}
			if key == "" {
				skipped = append(skipped, openCodeSkip{ID: id, Reason: "no api key"})
				continue
			}
			routeHeader := ""
			if route, ok := util.ReadBYOKRoute(prefix + id); ok {
				routeHeader = util.BYOKRouteHeader(route)
			}
			out = append(out, openCodeBYOK{
				ID:          id,
				File:        path,
				BaseURL:     upstream,
				APIKey:      key,
				RawAPIKey:   key,
				Npm:         npm,
				Protocol:    openCodeProtocol(npm),
				RouteHeader: routeHeader,
				Native:      true,
				Name:        openCodeV2ProviderName(m),
			})
		}
	}
	return out, skipped
}

// openCodeV2ProviderBaseAndKey returns the provider base URL and the api key exactly as written.
func openCodeV2ProviderBaseAndKey(m *util.OrderedMap) (base, key string) {
	settings, _ := mapChild(m, "settings")
	if settings != nil {
		if v, ok := settings.Get("baseURL"); ok {
			base, _ = v.(string)
		}
		if base == "" {
			if v, ok := settings.Get("baseUrl"); ok {
				base, _ = v.(string)
			}
		}
		if v, ok := settings.Get("apiKey"); ok {
			key, _ = v.(string)
		}
	}
	base = strings.TrimSpace(base)
	key = strings.TrimSpace(key)
	return base, key
}

// openCodeV2ProviderName returns the provider display name as written by the user.
func openCodeV2ProviderName(m *util.OrderedMap) string {
	v, ok := m.Get("name")
	if !ok {
		return ""
	}
	name, _ := v.(string)
	return strings.TrimSpace(name)
}

// openCodeV2ProviderUpstream resolves the upstream of a v2 provider entry.
func openCodeV2ProviderUpstream(m *util.OrderedMap, base string, proxyBase string, stashed string) string {
	if base != "" && isAbsoluteHTTP(base) && !openCodeV2ProxyEndpoint(base, proxyBase) && !openCodeV2EndpointSlot(m, base) {
		return base
	}
	headers, ok := mapChild(m, "headers")
	if ok {
		if origin, has := headers.Get(headroomBaseURLHeader); has {
			if raw, isString := origin.(string); isString && strings.TrimSpace(raw) != "" {
				return openCodeV2UpstreamWithPath(raw, headers)
			}
		}
	}
	return strings.TrimSpace(stashed)
}

// openCodeV2EndpointSlot reports whether the baseURL of a mirrored entry is the endpoint slot and not an upstream.
func openCodeV2EndpointSlot(m *util.OrderedMap, base string) bool {
	headers, ok := mapChild(m, "headers")
	if !ok {
		return false
	}
	if _, owned := headers.Get("X-Tokless-Route"); !owned {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return false
	}
	return util.IsLoopbackHost(u.Hostname())
}

// openCodeV2ProxyEndpoint reports whether a base URL points at a proxy.
func openCodeV2ProxyEndpoint(raw string, proxyBase string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if sameProxyBase(raw, proxyBase) {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !util.IsLoopbackHost(u.Hostname()) {
		return false
	}
	port := u.Port()
	if port == "" {
		return false
	}
	for _, p := range []int{util.HeadroomProxyPort(), util.BYOKGatewayPort(), util.GrokOAuthProxyPort()} {
		if port == strconv.Itoa(p) {
			return true
		}
	}
	return false
}

// openCodeV2UpstreamWithPath rebuilds the upstream base URL from the headroom origin plus the original path.
func openCodeV2UpstreamWithPath(origin string, headers *util.OrderedMap) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	path := ""
	if v, ok := headers.Get(openCodeV2OriginalPathHeader); ok {
		if raw, ok := v.(string); ok {
			path = strings.TrimSuffix(strings.TrimSpace(raw), "/chat/completions")
			path = strings.TrimRight(path, "/")
		}
	}
	if path == "" {
		return origin
	}
	return origin + path
}

// openCodeV2ProviderNpm maps a v2 provider package to its ai-sdk npm name.
func openCodeV2ProviderNpm(m *util.OrderedMap) string {
	pkg := ""
	if v, ok := m.Get("package"); ok {
		pkg, _ = v.(string)
	}
	switch strings.TrimSpace(pkg) {
	case "@opencode/ai/providers/openai-compatible":
		return "@ai-sdk/openai-compatible"
	case "@opencode/ai/providers/openai":
		return "@ai-sdk/openai"
	case "@opencode/ai/providers/anthropic":
		return "@ai-sdk/anthropic"
	default:
		return ""
	}
}

func openCodeProtocol(npm string) string {
	switch strings.TrimSpace(npm) {
	case "@ai-sdk/anthropic":
		return "anthropic-messages"
	case "@ai-sdk/openai-compatible", "@ai-sdk/openai":
		return "openai-chat"
	default:
		return ""
	}
}

// providerBaseAndKey returns the provider base URL and the resolved api key.
func providerBaseAndKey(m *util.OrderedMap) (base, key string) {
	base, raw := providerBaseAndRawKey(m)
	return base, resolveAPIKey(raw)
}

// providerBaseAndRawKey returns the provider base URL and the api key exactly as written.
func providerBaseAndRawKey(m *util.OrderedMap) (base, key string) {
	opts, _ := m.Get("options")
	om, _ := opts.(*util.OrderedMap)
	if om != nil {
		if v, ok := om.Get("baseURL"); ok {
			base, _ = v.(string)
		}
		if base == "" {
			if v, ok := om.Get("baseUrl"); ok {
				base, _ = v.(string)
			}
		}
		if v, ok := om.Get("apiKey"); ok {
			key, _ = v.(string)
		}
	}
	if base == "" {
		if v, ok := m.Get("baseURL"); ok {
			base, _ = v.(string)
		}
	}
	if key == "" {
		if v, ok := m.Get("apiKey"); ok {
			key, _ = v.(string)
		}
	}
	base = strings.TrimSpace(base)
	key = strings.TrimSpace(key)
	return base, key
}

// resolveAPIKey expands "{env:NAME}" refs; plain secrets pass through.
func resolveAPIKey(ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "{env:") && strings.HasSuffix(ref, "}") {
		name := strings.TrimSuffix(strings.TrimPrefix(ref, "{env:"), "}")
		return strings.TrimSpace(os.Getenv(name))
	}
	return ref
}

func sameProxyBase(a, b string) bool {
	a = strings.TrimRight(strings.ToLower(a), "/")
	b = strings.TrimRight(strings.ToLower(b), "/")
	return a != "" && a == b
}

func isAbsoluteHTTP(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func isValidBYOKUpstream(raw string) bool {
	return isAbsoluteHTTP(raw) && util.ValidBYOKUpstream(raw)
}

// SyncOpenCodeBYOKRoutes is retained for command output compatibility.
func SyncOpenCodeBYOKRoutes() int {
	return 0
}

// wireOpenCodeBYOK migrates providers from the retired static URL rewrite.
func wireOpenCodeBYOK() (changed bool, _ []openCodeBYOK) {
	changed, byoks, _ := wireOpenCodeBYOKChecked()
	return changed, byoks
}

func wireOpenCodeBYOKChecked() (changed bool, byoks []openCodeBYOK, success bool) {
	err := withProxyRouteStashLock(func() error {
		var ok bool
		changed, byoks, ok = wireOpenCodeBYOKLocked()
		if !ok {
			return fmt.Errorf("invalid OpenCode BYOK stash")
		}
		return nil
	})
	if err != nil {
		return false, nil, false
	}
	return changed, byoks, true
}

func wireOpenCodeBYOKLocked() (changed bool, byoks []openCodeBYOK, ok bool) {
	if !byokStashValid() {
		return false, nil, false
	}
	legacyChanged, ok := unwireOpenCodeBYOKLocked()
	if !ok {
		return false, nil, false
	}
	byoks = DiscoverOpenCodeBYOK()
	if len(byoks) == 0 {
		if openCodeHasOrphanedProxyRoute() {
			return false, nil, false
		}
		return legacyChanged, nil, true
	}
	if openCodeHasOrphanedProxyRoute() {
		return false, nil, false
	}
	return legacyChanged, byoks, true
}

// unwireOpenCodeBYOK restores original baseURLs from stash.
func unwireOpenCodeBYOK() bool {
	var removed bool
	if err := withProxyRouteStashLock(func() error {
		var ok bool
		removed, ok = unwireOpenCodeBYOKLocked()
		if !ok {
			return fmt.Errorf("invalid OpenCode BYOK stash")
		}
		return nil
	}); err != nil {
		return false
	}
	return removed
}

func unwireOpenCodeBYOKLocked() (removed bool, ok bool) {
	stashed := loadBYOKStash()
	if !byokStashValid() {
		return false, false
	}
	removed = false
	originals := map[string]string{}
	if pruneOpenCodeV2MirrorsInV1Config(stashed, originals) {
		removed = true
	}
	if len(stashed) == 0 {
		return removed, true
	}
	remaining := map[string]byokStashEntry{}
	for id, s := range stashed {
		if s.BaseURL == "" || s.File == "" {
			remaining[id] = s
			continue
		}
		if _, exists := originals[s.File]; !exists {
			if raw, ok := util.ReadFileSafe(s.File); ok {
				originals[s.File] = raw
			}
		}
		if setOpenCodeProviderRoute(s.File, id, s.BaseURL, "") {
			removed = true
		} else {
			return false, restoreOpenCodeProviderFiles(originals)
		}
	}
	if len(remaining) == 0 {
		if err := clearBYOKStash(); err != nil {
			return false, restoreOpenCodeProviderFiles(originals)
		}
	} else {
		if err := saveBYOKStash(remaining); err != nil {
			return false, restoreOpenCodeProviderFiles(originals)
		}
	}
	return removed, true
}

// pruneOpenCodeV2MirrorsInV1Config removes the v2-shaped provider entries an older tokless left in the v1 config.
func pruneOpenCodeV2MirrorsInV1Config(stashed map[string]byokStashEntry, originals map[string]string) bool {
	changed := false
	v2Stashed := loadBYOKStashAt(byokStashPathFor("opencode2"))
	for _, path := range openCodeConfigFiles() {
		raw, ok := util.ReadFileSafe(path)
		if !ok || util.HasJSONCComments(raw) {
			continue
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			continue
		}
		providers, has := mapChild(cfg, "providers")
		if !has {
			continue
		}
		fileChanged := false
		for _, id := range append([]string(nil), providers.Keys()...) {
			value, _ := providers.Get(id)
			entry, ok := value.(*util.OrderedMap)
			if !ok {
				continue
			}
			headers, ok := mapChild(entry, "headers")
			if !ok {
				continue
			}
			route, _ := headers.Get("X-Tokless-Route")
			s, ok := route.(string)
			if !ok || !openCodeRouteNamespaceOwned(s) {
				continue
			}
			if stash, ok := stashed[id]; ok && stash.File == path {
				restoreOpenCodeV2ProviderEntry(entry, openCodeBYOK{ID: id, BaseURL: stash.BaseURL, APIKey: stash.APIKey})
				fileChanged = true
				continue
			}
			if stash, ok := v2Stashed[id]; ok && stash.Native && stash.File == path {
				restoreOpenCodeV2ProviderEntry(entry, openCodeBYOK{ID: id, BaseURL: stash.BaseURL, APIKey: stash.APIKey})
				fileChanged = true
				continue
			}
			providers.Delete(id)
			fileChanged = true
		}
		if !fileChanged {
			continue
		}
		if providers.Len() == 0 {
			cfg.Delete("providers")
		}
		newRaw := util.StringifyJSON(cfg)
		if newRaw == raw {
			continue
		}
		if _, exists := originals[path]; !exists {
			originals[path] = raw
		}
		if util.WriteFile(path, newRaw) != nil {
			return changed
		}
		changed = true
	}
	return changed
}

// openCodeRouteNamespaceOwned reports whether a route header belongs to a tokless OpenCode route namespace.
func openCodeRouteNamespaceOwned(route string) bool {
	return strings.HasPrefix(route, "opencode:") || strings.HasPrefix(route, "opencode2:")
}

func openCodeV2Package(npm string) string {
	switch strings.TrimSpace(npm) {
	case "@ai-sdk/openai-compatible":
		return "@opencode/ai/providers/openai-compatible"
	case "@ai-sdk/openai":
		return "@opencode/ai/providers/openai"
	case "@ai-sdk/anthropic":
		return "@opencode/ai/providers/anthropic"
	default:
		return "@opencode/ai/providers/openai-compatible"
	}
}

// openCodeV2LegacyRoutePrefix is the namespace older tokless versions wrote into the v2 mirror.
const openCodeV2LegacyRoutePrefix = "opencode:"

// openCodeV2RoutePrefixOwned reports whether a route header is in the current or legacy namespace.
func openCodeV2RoutePrefixOwned(route string, prefix string) bool {
	return strings.HasPrefix(route, prefix) || strings.HasPrefix(route, openCodeV2LegacyRoutePrefix)
}

// openCodeV2ProviderEntryMatches reports whether the mirror entry already carries the wanted route and upstream.
func openCodeV2ProviderEntryMatches(providers *util.OrderedMap, id, endpoint, routeHeader, upstream string) bool {
	if providers == nil {
		return false
	}
	value, ok := providers.Get(id)
	if !ok {
		return false
	}
	p, ok := value.(*util.OrderedMap)
	if !ok {
		return false
	}
	settings, ok := mapChild(p, "settings")
	if !ok {
		return false
	}
	base, _ := settings.Get("baseURL")
	if base != endpoint {
		return false
	}
	headers, ok := mapChild(p, "headers")
	if !ok {
		return false
	}
	route, _ := headers.Get("X-Tokless-Route")
	origin, path := openCodeV2UpstreamHeaders(upstream)
	headroom, _ := headers.Get(headroomBaseURLHeader)
	if route != routeHeader || headroom != origin {
		return false
	}
	current, _ := headers.Get(openCodeV2OriginalPathHeader)
	if path == "" {
		return current == nil
	}
	return current == path
}

// openCodeV2UpstreamHeaders returns the origin and the original path a mirrored entry carries for an upstream.
func openCodeV2UpstreamHeaders(upstream string) (origin string, path string) {
	origin = openCodeV2Upstream(upstream)
	u, _ := url.Parse(upstream)
	if u != nil && strings.Trim(u.EscapedPath(), "/") != "" {
		path = strings.TrimRight(u.EscapedPath(), "/") + "/chat/completions"
	}
	return origin, path
}

// openCodeV2NativeEntries returns the native v2 providers of one file.
func openCodeV2NativeEntries(file string, prefix string) map[string]openCodeBYOK {
	out := map[string]openCodeBYOK{}
	stashPath := byokStashPathFor(strings.TrimSuffix(prefix, ":"))
	byoks, _ := discoverOpenCodeV2BYOKIn([]string{file}, prefix, stashPath)
	for _, b := range byoks {
		out[b.ID] = b
	}
	return out
}

// openCodeV2ProvidersOf returns the providers map of one config file.
func openCodeV2ProvidersOf(file string) *util.OrderedMap {
	raw, ok := util.ReadFileSafe(file)
	if !ok || util.HasJSONCComments(raw) {
		return nil
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return nil
	}
	providers, _ := mapChild(cfg, "providers")
	return providers
}

// openCodeV2StashFor records the providers the v2 mirror takes over.
func openCodeV2StashFor(byoks []openCodeBYOK, mirrorFile string, prefix string) map[string]byokStashEntry {
	out := map[string]byokStashEntry{}
	providers := openCodeV2ProvidersOf(mirrorFile)
	proxyBase := openCodeProxyEndpointFor(prefix)
	previous := loadBYOKStashAt(byokStashPathFor(strings.TrimSuffix(prefix, ":")))
	for _, b := range byoks {
		if b.RouteHeader == "" {
			continue
		}
		var m *util.OrderedMap
		if providers != nil {
			if value, ok := providers.Get(b.ID); ok {
				m, _ = value.(*util.OrderedMap)
			}
		}
		native := m != nil && !openCodeV2OwnedRoute(providers, b.ID, prefix)
		base := b.BaseURL
		key := ""
		if native {
			entryBase, entryKey := openCodeV2ProviderBaseAndKey(m)
			prev := previous[b.ID]
			if up := openCodeV2ProviderUpstream(m, entryBase, proxyBase, prev.BaseURL); up != "" {
				base = up
			}
			key = entryKey
		}
		out[b.ID] = byokStashEntry{
			File:    mirrorFile,
			BaseURL: base,
			Native:  native,
			APIKey:  key,
		}
	}
	for id, prev := range previous {
		cur, ok := out[id]
		if !ok {
			out[id] = prev
			continue
		}
		if !prev.Native {
			continue
		}
		cur.Native = true
		if prev.File != "" {
			cur.File = prev.File
		}
		if prev.BaseURL != "" && !openCodeV2ProxyEndpoint(prev.BaseURL, proxyBase) {
			cur.BaseURL = prev.BaseURL
		}
		if prev.APIKey != "" {
			cur.APIKey = prev.APIKey
		}
		out[id] = cur
	}
	return out
}

// openCodeProxyEndpointFor returns the proxy endpoint a lane points at.
func openCodeProxyEndpointFor(prefix string) string {
	endpoint := util.BYOKGatewayEndpoint()
	if e := strings.TrimSpace(ProxyEndpointFor(strings.TrimSuffix(prefix, ":"))); e != "" {
		endpoint = e
	}
	return endpoint
}

// syncOpenCodeV2BYOKFor mirrors providers into the v2 provider shape, touching only tokless-owned or native entries.
func syncOpenCodeV2BYOKFor(byoks []openCodeBYOK, file string, prefix string, prune bool) (changed, ok bool) {
	actualHome, _ := os.UserHomeDir()
	if util.Home() != actualHome {
		return false, true
	}
	raw, exists := util.ReadFileSafe(file)
	if !exists {
		raw = "{}"
	}
	if util.HasJSONCComments(raw) {
		return false, false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false, false
	}
	providers, has := mapChild(cfg, "providers")
	if !has && len(byoks) == 0 {
		return false, true
	}
	endpoint := openCodeProxyEndpointFor(prefix)
	wanted := map[string]bool{}
	if !prune {
		for _, b := range byoks {
			if b.RouteHeader == "" {
				continue
			}
			wanted[b.ID] = true
		}
		if len(wanted) == 0 {
			return false, true
		}
	}
	if !has {
		if prune {
			return false, true
		}
		providers = util.NewOrderedMap()
		cfg.Set("providers", providers)
	}
	nativeEntries := openCodeV2NativeEntries(file, prefix)
	if !prune {
		for _, b := range byoks {
			if b.RouteHeader == "" {
				continue
			}
			_, isNative := nativeEntries[b.ID]
			value, present := providers.Get(b.ID)
			entry, isEntry := value.(*util.OrderedMap)
			if present && isEntry {
				if openCodeV2ProviderEntryMatches(providers, b.ID, endpoint, b.RouteHeader, b.BaseURL) {
					continue
				}
				if !openCodeV2OwnedRoute(providers, b.ID, prefix) && !isNative {
					continue
				}
				openCodeV2RewriteProviderEntry(entry, b, endpoint)
				changed = true
				continue
			}
			if present {
				continue
			}
			providers.Set(b.ID, openCodeV2ProviderEntry(b, endpoint))
			changed = true
		}
	}
	if prune {
		stashed := loadBYOKStashAt(byokStashPathFor(strings.TrimSuffix(prefix, ":")))
		for _, id := range append([]string(nil), providers.Keys()...) {
			if v, _ := providers.Get(id); v != nil {
				if m, ok := v.(*util.OrderedMap); ok {
					h, hasHeaders := mapChild(m, "headers")
					if !hasHeaders {
						continue
					}
					route, _ := h.Get("X-Tokless-Route")
					if s, ok := route.(string); ok && openCodeV2RoutePrefixOwned(s, prefix) {
						if entry, isNative := stashed[id]; isNative && entry.Native && entry.File == file {
							if restoreOpenCodeV2ProviderEntry(m, openCodeBYOK{ID: id, BaseURL: entry.BaseURL, APIKey: entry.APIKey}) {
								changed = true
							}
							continue
						}
						providers.Delete(id)
						changed = true
					}
				}
			}
		}
		if !changed {
			return false, true
		}
	}
	if providers.Len() == 0 && changed {
		cfg.Delete("providers")
	}
	newRaw := util.StringifyJSON(cfg)
	if newRaw == raw {
		return changed, true
	}
	if err := util.WriteFile(file, newRaw); err != nil {
		return false, false
	}
	return true, true
}

// openCodeV2OwnedRoute reports whether the mirror entry is tokless-owned.
func openCodeV2OwnedRoute(providers *util.OrderedMap, id string, prefix string) bool {
	if providers == nil {
		return false
	}
	value, ok := providers.Get(id)
	if !ok {
		return false
	}
	entry, ok := value.(*util.OrderedMap)
	if !ok {
		return false
	}
	headers, ok := mapChild(entry, "headers")
	if !ok {
		return false
	}
	route, _ := headers.Get("X-Tokless-Route")
	s, ok := route.(string)
	return ok && openCodeV2RoutePrefixOwned(s, prefix)
}

func openCodeV2Upstream(raw string) string {
	u, _ := url.Parse(raw)
	if u != nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return raw
}

// openCodeV2ProviderEntry mirrors a provider into the v2 shape; the key stays raw, so no secret is materialized.
func openCodeV2ProviderEntry(b openCodeBYOK, endpoint string) *util.OrderedMap {
	entry := util.NewOrderedMap()
	name := b.Name
	if name == "" {
		name = b.ID
	}
	entry.Set("name", name)
	entry.Set("package", openCodeV2Package(b.Npm))
	settings := util.NewOrderedMap()
	settings.Set("baseURL", endpoint)
	if key := openCodeBYOKRawKey(b); key != "" {
		settings.Set("apiKey", key)
	}
	entry.Set("settings", settings)
	origin, path := openCodeV2UpstreamHeaders(b.BaseURL)
	headers := util.NewOrderedMap()
	headers.Set("X-Tokless-Route", b.RouteHeader)
	headers.Set(headroomBaseURLHeader, origin)
	if path != "" {
		headers.Set(openCodeV2OriginalPathHeader, path)
	}
	entry.Set("headers", headers)
	entry.Set("models", openCodeV2Models(b.File, b.ID))
	return entry
}

// openCodeBYOKRawKey returns the key of a provider exactly as the user wrote it.
func openCodeBYOKRawKey(b openCodeBYOK) string {
	if b.RawAPIKey != "" {
		return b.RawAPIKey
	}
	return b.APIKey
}

// openCodeV2RewriteProviderEntry rewrites a native v2 provider entry in place.
func openCodeV2RewriteProviderEntry(entry *util.OrderedMap, b openCodeBYOK, endpoint string) {
	if entry == nil {
		return
	}
	settings := getOrCreateMap(entry, "settings")
	settings.Set("baseURL", endpoint)
	settings.Delete("baseUrl")
	if key := openCodeV2EntryKey(settings, b); key != "" {
		settings.Set("apiKey", key)
	}
	if !b.Native {
		entry.Set("package", openCodeV2Package(b.Npm))
	}
	origin, path := openCodeV2UpstreamHeaders(b.BaseURL)
	headers := getOrCreateMap(entry, "headers")
	headers.Set("X-Tokless-Route", b.RouteHeader)
	headers.Set(headroomBaseURLHeader, origin)
	if path != "" {
		headers.Set(openCodeV2OriginalPathHeader, path)
	} else {
		headers.Delete(openCodeV2OriginalPathHeader)
	}
}

// openCodeV2EntryKey returns the key a mirrored entry keeps.
func openCodeV2EntryKey(settings *util.OrderedMap, b openCodeBYOK) string {
	if !b.Native {
		return openCodeBYOKRawKey(b)
	}
	if v, ok := settings.Get("apiKey"); ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return openCodeBYOKRawKey(b)
}

// openCodeV2OriginalPathHeader carries the upstream path a mirrored entry replaced with the proxy endpoint.
const openCodeV2OriginalPathHeader = "x-headroom-original-path"

// restoreOpenCodeV2ProviderEntry gives a taken-over native provider entry back to the user.
func restoreOpenCodeV2ProviderEntry(entry *util.OrderedMap, b openCodeBYOK) bool {
	if entry == nil {
		return false
	}
	settings := getOrCreateMap(entry, "settings")
	if b.BaseURL != "" {
		settings.Set("baseURL", b.BaseURL)
		settings.Delete("baseUrl")
	}
	if b.APIKey != "" {
		settings.Set("apiKey", b.APIKey)
	}
	headers, ok := mapChild(entry, "headers")
	if !ok {
		return true
	}
	headers.Delete("X-Tokless-Route")
	headers.Delete(headroomBaseURLHeader)
	headers.Delete(openCodeV2OriginalPathHeader)
	if headers.Len() == 0 {
		entry.Delete("headers")
	}
	return true
}

func openCodeV2Models(file, id string) *util.OrderedMap {
	out := util.NewOrderedMap()
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return out
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return out
	}
	p, ok := openCodeV2ModelsProvider(cfg, id)
	if !ok {
		return out
	}
	mv, ok := p.Get("models")
	if !ok {
		return out
	}
	models, ok := mv.(*util.OrderedMap)
	if !ok {
		return out
	}
	for _, key := range models.Keys() {
		value, _ := models.Get(key)
		m, ok := value.(*util.OrderedMap)
		if !ok {
			continue
		}
		entry := util.NewOrderedMap()
		modelID, _ := m.Get("modelID")
		if modelID == nil {
			modelID = key
		}
		entry.Set("modelID", modelID)
		for _, field := range []string{"name", "limit"} {
			if v, ok := m.Get(field); ok {
				entry.Set(field, v)
			}
		}
		out.Set(key, entry)
	}
	return out
}

// openCodeV2ModelsProvider finds the provider entry that owns the models.
func openCodeV2ModelsProvider(cfg *util.OrderedMap, id string) (*util.OrderedMap, bool) {
	for _, key := range []string{"provider", "providers"} {
		ps, ok := mapChild(cfg, key)
		if !ok {
			continue
		}
		v, ok := ps.Get(id)
		if !ok {
			continue
		}
		p, ok := v.(*util.OrderedMap)
		if !ok {
			continue
		}
		return p, true
	}
	return nil, false
}

func restoreOpenCodeProviderFiles(originals map[string]string) bool {
	ok := true
	for path, raw := range originals {
		if util.WriteFile(path, raw) != nil {
			ok = false
		}
	}
	return ok
}

func openCodeProviderRouteMatches(raw, id, proxyBase, upstream string) bool {
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		return false
	}
	block, ok := providers.Get(id)
	if !ok {
		return false
	}
	provider, ok := block.(*util.OrderedMap)
	if !ok || rawProviderBaseURL(provider) != proxyBase {
		return false
	}
	options, ok := mapChild(provider, "options")
	if !ok {
		return false
	}
	headers, _ := mapChild(options, "headers")
	value, hasUpstream := headers.Get(headroomBaseURLHeader)
	return upstream == "" && !hasUpstream || hasUpstream && value == upstream
}

const headroomBaseURLHeader = "x-headroom-base-url"

func setOpenCodeProviderRoute(path, id, baseURL, upstream string) bool {
	raw, ok := util.ReadFileSafe(path)
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
	block, ok := providers.Get(id)
	if !ok {
		return false
	}
	provider, ok := block.(*util.OrderedMap)
	if !ok {
		return false
	}
	options, ok := mapChild(provider, "options")
	if !ok {
		return false
	}
	current := rawProviderBaseURL(provider)
	if current == "" {
		return false
	}
	if upstream == "" && current != ProxyEndpointFor("opencode") && current != util.BYOKGatewayEndpoint() && baseURL != util.BYOKGatewayEndpoint() {
		return false
	}
	headers, ok := mapChild(options, "headers")
	if !ok {
		if _, exists := options.Get("headers"); exists {
			return false
		}
		headers = util.NewOrderedMap()
		options.Set("headers", headers)
	}
	if v, exists := headers.Get(headroomBaseURLHeader); exists && upstream != "" && v != upstream {
		return false
	}
	currentUpstream, hasUpstream := headers.Get(headroomBaseURLHeader)
	if current == baseURL && ((upstream == "" && !hasUpstream) || currentUpstream == upstream) {
		return false
	}
	options.Set("baseURL", baseURL)
	options.Delete("baseUrl")
	if upstream == "" {
		headers.Delete(headroomBaseURLHeader)
		if headers.Len() == 0 {
			options.Delete("headers")
		}
	} else {
		headers.Set(headroomBaseURLHeader, upstream)
	}
	return util.WriteFile(path, util.StringifyJSON(cfg)) == nil
}

// openCodeBYOKWired reports whether at least one stashed provider still points at the proxy.
func openCodeBYOKWired() bool {
	stashed := loadBYOKStash()
	if len(stashed) == 0 {
		return false
	}
	proxyBase := ProxyEndpointFor("opencode")
	for id, s := range stashed {
		raw, ok := util.ReadFileSafe(s.File)
		if !ok {
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
		block, ok := providers.Get(id)
		if !ok {
			continue
		}
		m, _ := block.(*util.OrderedMap)
		if m == nil {
			continue
		}
		base, _ := providerBaseAndKey(m)
		base = rawProviderBaseURL(m)
		if sameProxyBase(base, proxyBase) {
			return true
		}
	}
	return false
}

// openCodeHasOrphanedProxyRoute detects a provider left on Headroom without a verified legacy stash.
func openCodeHasOrphanedProxyRoute() bool {
	proxyBase := ProxyEndpointFor("opencode")
	for _, path := range openCodeConfigFiles() {
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
			provider, ok := block.(*util.OrderedMap)
			if ok && sameProxyBase(rawProviderBaseURL(provider), proxyBase) {
				return true
			}
		}
	}
	return false
}

func rawProviderBaseURL(m *util.OrderedMap) string {
	if opts, ok := m.Get("options"); ok {
		if om, ok := opts.(*util.OrderedMap); ok {
			if v, ok := om.Get("baseURL"); ok {
				if s, ok := v.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

func rewriteProviderBaseURL(path, id, baseURL string) bool {
	raw, ok := util.ReadFileSafe(path)
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
	block, ok := providers.Get(id)
	if !ok {
		return false
	}
	m, ok := block.(*util.OrderedMap)
	if !ok {
		return false
	}
	cur := rawProviderBaseURL(m)
	if cur == baseURL {
		return false
	}
	if cur != "" {
		if next, n := replaceProviderBaseURLScoped(raw, id, cur, baseURL); n > 0 {
			return util.WriteFile(path, next) == nil
		}
		return false
	}
	return false
}

func replaceProviderBaseURL(raw, oldURL, newURL string) (string, int) {
	if oldURL == "" || oldURL == newURL {
		return raw, 0
	}
	return replaceProviderBaseURLScoped(raw, "", oldURL, newURL)
}

func replaceProviderBaseURLScoped(raw, id, oldURL, newURL string) (string, int) {
	if id != "" {
		s, e, ok := providerBlockBounds(raw, id)
		if !ok {
			return raw, 0
		}
		block := raw[s:e]
		nb, n := replaceBaseURLInBlock(block, oldURL, newURL)
		if n == 0 {
			return raw, 0
		}
		return raw[:s] + nb + raw[e:], n
	}
	for _, probe := range providerBlockCandidates(raw, oldURL) {
		s, e, ok := providerBlockBounds(raw, probe)
		if !ok {
			continue
		}
		block := raw[s:e]
		if !strings.Contains(block, `"`+oldURL+`"`) {
			continue
		}
		nb, n := replaceBaseURLInBlock(block, oldURL, newURL)
		if n == 0 {
			continue
		}
		return raw[:s] + nb + raw[e:], n
	}
	b, n := replaceBaseURLInBlock(raw, oldURL, newURL)
	if n == 0 {
		return raw, 0
	}
	return b, n
}

func providerBlockCandidates(raw, oldURL string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range extractProviderIDs(raw) {
		if seen[id] {
			continue
		}
		seen[id] = true
		s, e, ok := providerBlockBounds(raw, id)
		if !ok {
			continue
		}
		if strings.Contains(raw[s:e], `"`+oldURL+`"`) {
			out = append(out, id)
		}
	}
	return out
}

func extractProviderIDs(raw string) []string {
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return nil
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		return nil
	}
	return providers.Keys()
}

func providerBlockBounds(raw, id string) (int, int, bool) {
	key := `"` + id + `"`
	pos := -1
	search := 0
	for {
		idx := strings.Index(raw[search:], key)
		if idx == -1 {
			return 0, 0, false
		}
		abs := search + idx
		after := strings.TrimLeft(raw[abs+len(key):], " \t\n\r")
		if strings.HasPrefix(after, ":") {
			pos = abs
			break
		}
		search = abs + len(key)
		if search >= len(raw) {
			return 0, 0, false
		}
	}
	colon := strings.Index(raw[pos+len(key):], ":")
	if colon == -1 {
		return 0, 0, false
	}
	braceRel := strings.Index(raw[pos+len(key)+colon:], "{")
	if braceRel == -1 {
		return 0, 0, false
	}
	start := pos + len(key) + colon + braceRel
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return start, i + 1, true
			}
		}
	}
	return 0, 0, false
}

func replaceBaseURLInBlock(block, oldURL, newURL string) (string, int) {
	for _, key := range []string{"baseURL", "baseUrl"} {
		search := `"` + key + `"`
		idx := strings.Index(block, search)
		if idx == -1 {
			continue
		}
		rest := block[idx+len(search):]
		colonIdx := strings.Index(rest, ":")
		if colonIdx == -1 {
			continue
		}
		afterColon := rest[colonIdx+1:]
		q1Rel := strings.Index(afterColon, `"`)
		if q1Rel == -1 {
			continue
		}
		q1Abs := idx + len(search) + colonIdx + 1 + q1Rel
		q2Rel := strings.Index(block[q1Abs+1:], `"`)
		if q2Rel == -1 {
			continue
		}
		q2Abs := q1Abs + 1 + q2Rel
		cur := block[q1Abs+1 : q2Abs]
		if cur != oldURL {
			continue
		}
		return block[:q1Abs+1] + newURL + block[q2Abs:], 1
	}
	return block, 0
}

// --- legacy stash: original baseURL per provider id (no secrets) ---

type byokStashEntry struct {
	File    string `json:"file"`
	BaseURL string `json:"base_url"`

	Native bool `json:"native,omitempty"`

	APIKey string `json:"api_key,omitempty"`
}

type byokStashFile struct {
	Providers map[string]byokStashEntry `json:"providers"`

	Lane *openCodeLaneStash `json:"lane,omitempty"`
}

// openCodeLaneStash records the resolved lane of a wire.
type openCodeLaneStash struct {
	Dir string `json:"dir"`

	Config string `json:"config"`

	Files []string `json:"files,omitempty"`
}

func byokStashPath() string {
	return byokStashPathFor("opencode")
}

// byokStashPathFor returns the BYOK stash path of one agent; stashes never cross versions.
func byokStashPathFor(agent string) string {
	return filepath.Join(util.HeadroomPathsResolved().Root, agent+".byok.stash.json")
}

func loadBYOKStash() map[string]byokStashEntry {
	return loadBYOKStashAt(byokStashPath())
}

func loadBYOKStashAt(path string) map[string]byokStashEntry {
	return loadBYOKStashFileAt(path).Providers
}

// loadBYOKStashFileAt reads a stash with its recorded lane.
func loadBYOKStashFileAt(path string) byokStashFile {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return byokStashFile{Providers: map[string]byokStashEntry{}}
	}
	var f byokStashFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil || f.Providers == nil {
		return byokStashFile{Providers: map[string]byokStashEntry{}}
	}
	return f
}

func saveBYOKStash(m map[string]byokStashEntry) error {
	return saveBYOKStashAt(byokStashPath(), m)
}

func saveBYOKStashAt(path string, m map[string]byokStashEntry) error {
	return saveBYOKStashFileAt(path, byokStashFile{Providers: m})
}

// saveBYOKStashFileAt writes a stash with its recorded lane.
func saveBYOKStashFileAt(path string, f byokStashFile) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return util.WriteFileMode(path, string(b), 0o600)
}

func byokStashValid() bool {
	return byokStashValidAt(byokStashPath())
}

func byokStashValidAt(path string) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return true
	}
	var f byokStashFile
	if json.Unmarshal([]byte(raw), &f) != nil || f.Providers == nil {
		return false
	}
	for id, entry := range f.Providers {
		if id == "" || entry.File == "" || entry.BaseURL == "" {
			return false
		}
	}
	return true
}

func clearBYOKStash() error {
	return clearBYOKStashAt(byokStashPath())
}

func clearBYOKStashAt(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
