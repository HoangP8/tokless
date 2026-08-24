package agents

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// SyncOpenCodeBYOKRoutes is retained for command output compatibility.
func SyncOpenCodeBYOKRoutes() int {
	return 0
}

// wireOpenCodeBYOK points every discovered BYOK provider at Headroom while
// retaining its original upstream in Headroom's supported per-request header.
func wireOpenCodeBYOK() (changed bool, _ []openCodeBYOK) {
	proxyBase := ProxyEndpointFor("opencode")
	if proxyBase == "" {
		return false, nil
	}
	byoks := DiscoverOpenCodeBYOK()
	if len(byoks) == 0 {
		_ = clearBYOKStash()
		return false, nil
	}
	newStash := map[string]byokStashEntry{}
	for _, b := range byoks {
		newStash[b.ID] = byokStashEntry{File: b.File, BaseURL: b.BaseURL}
		if setOpenCodeProviderRoute(b.File, b.ID, proxyBase, b.BaseURL) {
			changed = true
		}
	}
	_ = saveBYOKStash(newStash)
	return changed, byoks
}

// unwireOpenCodeBYOK restores original baseURLs from stash.
func unwireOpenCodeBYOK() bool {
	stashed := loadBYOKStash()
	if len(stashed) == 0 {
		return false
	}
	removed := false
	for id, s := range stashed {
		if s.BaseURL == "" || s.File == "" {
			continue
		}
		if setOpenCodeProviderRoute(s.File, id, s.BaseURL, "") {
			removed = true
		}
	}
	_ = clearBYOKStash()
	return removed
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
	if upstream == "" && current != ProxyEndpointFor("opencode") {
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
