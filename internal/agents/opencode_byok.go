package agents

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

// openCodeBYOK is one user-defined provider that can ride the headroom proxy.
type openCodeBYOK struct {
	ID      string
	File    string
	BaseURL string
	APIKey  string
	Npm     string
}

// openCodeConfigFiles returns every global OpenCode config that may hold providers,
// in OpenCode load order (config.json first → opencode.json → opencode.jsonc).
func openCodeConfigFiles() []string {
	dir := util.OpenCodePathsResolved().Dir
	return []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, "opencode.jsonc"),
	}
}

// DiscoverOpenCodeBYOK finds user providers with a real upstream + credential.
func DiscoverOpenCodeBYOK() []openCodeBYOK {
	proxyBase := strings.TrimRight(ProxyEndpointFor("opencode"), "/")
	type acc struct {
		base, key, file, npm string
	}
	got := map[string]*acc{}
	order := []string{}

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
			}
			if key != "" {
				a.key = key
			}
			if npm, ok := m.Get("npm"); ok {
				if s, ok := npm.(string); ok && s != "" {
					a.npm = s
				}
			}
			if base != "" && isAbsoluteHTTP(base) && !sameProxyBase(base, proxyBase) {
				a.base = base
				a.file = path
			}
		}
	}

	// Fill missing originals from stash (provider already rewritten to proxy).
	stashed := loadBYOKStash()
	var out []openCodeBYOK
	for _, id := range order {
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
			ID:      id,
			File:    a.file,
			BaseURL: a.base,
			APIKey:  a.key,
			Npm:     a.npm,
		})
	}
	return out
}

func providerBaseAndKey(m *util.OrderedMap) (base, key string) {
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
	key = resolveAPIKey(strings.TrimSpace(key))
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
	return u.Scheme == "http" || u.Scheme == "https"
}

// SyncOpenCodeBYOKRoutes discovers user BYOK providers and writes the route
// table (no config rewrite).
func SyncOpenCodeBYOKRoutes() int {
	_, routes := buildBYOKRoutes()
	return len(routes)
}

func buildBYOKRoutes() (byoks []openCodeBYOK, routes []headroom.RouteEntry) {
	byoks = DiscoverOpenCodeBYOK()
	for _, b := range byoks {
		fp := headroom.KeyFingerprint(b.APIKey)
		if fp == "" {
			continue
		}
		routes = append(routes, headroom.RouteEntry{KeyFP: fp, BaseURL: b.BaseURL, ID: b.ID})
	}
	_ = headroom.SaveRouteMap(routes)
	return byoks, routes
}

// wireOpenCodeBYOK points every discovered BYOK baseURL at headroom and writes
// the key-fingerprint → real-host route table.
func wireOpenCodeBYOK() (changed bool, routes []headroom.RouteEntry) {
	proxyBase := ProxyEndpointFor("opencode")
	if proxyBase == "" {
		return false, nil
	}
	byoks, routes := buildBYOKRoutes()
	if len(byoks) == 0 {
		_ = clearBYOKStash()
		return false, nil
	}
	newStash := map[string]byokStashEntry{}
	for _, b := range byoks {
		newStash[b.ID] = byokStashEntry{File: b.File, BaseURL: b.BaseURL}
		if rewriteProviderBaseURL(b.File, b.ID, proxyBase) {
			changed = true
		}
	}
	_ = saveBYOKStash(newStash)
	return changed, routes
}

// unwireOpenCodeBYOK restores original baseURLs from stash and clears routes.
func unwireOpenCodeBYOK() bool {
	stashed := loadBYOKStash()
	if len(stashed) == 0 {
		_ = headroom.ClearRouteMap()
		return false
	}
	removed := false
	for id, s := range stashed {
		if s.BaseURL == "" || s.File == "" {
			continue
		}
		if rewriteProviderBaseURL(s.File, id, s.BaseURL) {
			removed = true
		}
	}
	_ = clearBYOKStash()
	_ = headroom.ClearRouteMap()
	return removed
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
		if next, n := replaceProviderBaseURL(raw, cur, baseURL); n > 0 {
			return util.WriteFile(path, next) == nil
		}
	}
	opts, _ := m.Get("options")
	om, _ := opts.(*util.OrderedMap)
	if om == nil {
		om = util.NewOrderedMap()
		m.Set("options", om)
	}
	om.Set("baseURL", baseURL)
	return util.WriteFile(path, util.StringifyJSON(cfg)) == nil
}

func replaceProviderBaseURL(raw, oldURL, newURL string) (string, int) {
	if oldURL == "" || oldURL == newURL {
		return raw, 0
	}
	n := 0
	for _, key := range []string{"baseURL", "baseUrl"} {
		for _, sp := range []string{`: "`, `:"`} {
			from := `"` + key + `"` + sp + oldURL + `"`
			to := `"` + key + `"` + sp + newURL + `"`
			if strings.Contains(raw, from) {
				raw = strings.Replace(raw, from, to, 1)
				n++
				return raw, n
			}
		}
	}
	return raw, 0
}

// --- stash: original baseURL per provider id (no secrets) ---

type byokStashEntry struct {
	File    string `json:"file"`
	BaseURL string `json:"base_url"`
}

type byokStashFile struct {
	Providers map[string]byokStashEntry `json:"providers"`
}

func byokStashPath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "opencode.byok.stash.json")
}

func loadBYOKStash() map[string]byokStashEntry {
	raw, ok := util.ReadFileSafe(byokStashPath())
	if !ok {
		return map[string]byokStashEntry{}
	}
	var f byokStashFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil || f.Providers == nil {
		return map[string]byokStashEntry{}
	}
	return f.Providers
}

func saveBYOKStash(m map[string]byokStashEntry) error {
	b, err := json.Marshal(byokStashFile{Providers: m})
	if err != nil {
		return err
	}
	return util.WriteFileMode(byokStashPath(), string(b), 0o600)
}

func clearBYOKStash() error {
	if err := os.Remove(byokStashPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
