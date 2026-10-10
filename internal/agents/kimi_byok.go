package agents

import (
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// kimiRouteHeader is the HTTP header the BYOK gateway reads. Kimi providers
// carry it in the TOML custom_headers table.
const kimiRouteHeader = "X-Tokless-Route"

// Headroom-front headers. Kimi providers point base_url at the headroom
// OpenAI front (8787/v1) and carry the real upstream in these two headers so
// headroom compresses and forwards to the actual API, mirroring opencode.
const (
	kimiHeadroomBaseURLHeader    = "x-headroom-base-url"
	kimiHeadroomOriginalPathHdr  = "x-headroom-original-path"
)

// Kimi's config.toml uses quoted dotted provider headers such as
// [providers."acme"]. util.TomlBlockField/findBlockRange key off exact header
// text and are awkward to reuse for surgical value edits, so this file uses a
// dedicated regex scanner (modeled on codexNamedProviderField).
var reKimiProviderHeader = regexp.MustCompile(`(?m)^[ \t]*\[\s*providers\s*\.\s*(?:"([^"]+)"|'([^']+)'|([A-Za-z0-9_-]+))\s*\](?:[ \t]*#.*)?$`)

var reKimiNextHeader = regexp.MustCompile(`(?m)^[ \t]*\[`)

var reKimiInlineCustom = regexp.MustCompile(`(?m)^([ \t]*)custom_headers[ \t]*=[ \t]*\{([^}]*)\}[ \t]*(?:#.*)?$`)

var reKimiInlinePair = regexp.MustCompile(`(?:"((?:[^"\\]|\\.)*)"|'([^']*)')\s*=\s*(?:"((?:[^"\\]|\\.)*)"|'([^']*)')`)

var reKimiBaseURL = regexp.MustCompile(`(?m)^([ \t]*(?:base_url|"base_url"|'base_url')[ \t]*=[ \t]*)(?:"(?:[^"\\]|\\.)*"|'[^']*')`)

type kimiProviderBlock struct {
	id    string
	start int
	end   int
}

// kimiScanProviders returns every [providers.<id>] table in file order.
func kimiScanProviders(raw string) []kimiProviderBlock {
	locs := reKimiProviderHeader.FindAllStringSubmatchIndex(raw, -1)
	out := make([]kimiProviderBlock, 0, len(locs))
	for _, loc := range locs {
		id := ""
		switch {
		case loc[2] >= 0:
			id = raw[loc[2]:loc[3]]
		case loc[4] >= 0:
			id = raw[loc[4]:loc[5]]
		default:
			id = raw[loc[6]:loc[7]]
		}
		end := len(raw)
		if next := reKimiNextHeader.FindStringIndex(raw[loc[1]:]); next != nil {
			end = loc[1] + next[0]
		}
		out = append(out, kimiProviderBlock{id: id, start: loc[0], end: end})
	}
	return out
}

func kimiFindProvider(raw, id string) (kimiProviderBlock, bool) {
	for _, p := range kimiScanProviders(raw) {
		if p.id == id {
			return p, true
		}
	}
	return kimiProviderBlock{}, false
}

// kimiBlockField reads a string scalar from a provider block; "" when absent.
func kimiBlockField(block, key string) string {
	re := regexp.MustCompile(`(?m)^[ \t]*(?:` + regexp.QuoteMeta(key) + `|"` + regexp.QuoteMeta(key) + `"|'` + regexp.QuoteMeta(key) + `')[ \t]*=[ \t]*(?:"((?:[^"\\]|\\.)*)"|'([^']*)')`)
	m := re.FindStringSubmatch(block)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		if v, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
			return v
		}
		return m[1]
	}
	return strings.ReplaceAll(m[2], "''", "'")
}

// kimiProviderProtocol maps Kimi's provider type to a BYOK route protocol.
// An omitted type is treated as OpenAI-compatible (the provider still needs a
// base_url and credential to be discovered). Only OpenAI-compatible transports
// are eligible for base-URL rewriting.
func kimiProviderProtocol(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "openai", "openai-chat", "openai-completions", "openai_chat", "openai_completions":
		return "openai-completions"
	case "openai_responses", "openai-responses":
		return "openai-responses"
	}
	return ""
}

// kimiProviderKey resolves the provider credential for discovery only. It is
// never copied into the route registry.
// kimiProviderKey resolves a provider's credential for discovery only. The
// value is never persisted to config.toml, the BYOK route registry, or logs;
// the gateway forwards the harness's own Authorization header upstream.
func kimiProviderKey(block string) string {
	if k := kimiBlockField(block, "api_key"); k != "" {
		return k
	}
	if env := kimiBlockField(block, "api_key_env"); env != "" {
		return strings.TrimSpace(os.Getenv(env))
	}
	return ""
}

// DiscoverKimiBYOK finds OpenAI-compatible providers with a real upstream and
// credential. Providers already pointing at the proxy/gateway are recovered
// from the stash so a re-run stays idempotent.
func DiscoverKimiBYOK() []openCodeBYOK {
	path := util.KimiPathsResolved().Config
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return nil
	}
	proxyBase := strings.TrimRight(ProxyEndpointFor("kimi"), "/")
	gatewayBase := strings.TrimRight(util.BYOKGatewayEndpoint(), "/")
	type acc struct {
		base, key, proto string
	}
	got := map[string]*acc{}
	var order []string
	for _, p := range kimiScanProviders(raw) {
		block := raw[p.start:p.end]
		proto := kimiProviderProtocol(kimiBlockField(block, "type"))
		if proto == "" {
			continue
		}
		base := kimiBlockField(block, "base_url")
		key := kimiProviderKey(block)
		if base == "" && key == "" {
			continue
		}
		a, exists := got[p.id]
		if !exists {
			a = &acc{proto: proto}
			got[p.id] = a
			order = append(order, p.id)
		}
		if key != "" {
			a.key = key
		}
		if isAbsoluteHTTP(base) && isValidBYOKUpstream(base) &&
			!sameProxyBase(base, proxyBase) && !sameProxyBase(base, gatewayBase) && !sameProxyBase(base, gatewayBase+"/v1") {
			a.base = base
		}
	}
	stashed := loadProxyRouteStash("kimi")
	out := make([]openCodeBYOK, 0, len(order))
	for _, id := range order {
		a := got[id]
		if a.base == "" {
			if s, ok := stashed[id]; ok && s.BaseURL != "" {
				a.base = s.BaseURL
			}
		}
		if a.base == "" || a.key == "" || a.proto == "" {
			continue
		}
		out = append(out, openCodeBYOK{ID: id, File: path, BaseURL: a.base, APIKey: a.key, Protocol: a.proto})
	}
	return out
}

// kimiGatewayBase accepts the bare gateway and the /v1 form.
// kimiHeadroomUpstream splits a real provider base URL into the origin
// headroom forwards to (x-headroom-base-url) and the chat-completions
// path (x-headroom-original-path). ok is false when the URL cannot be
// parsed, in which case the provider must not be pointed at headroom.
func kimiHeadroomUpstream(baseURL string) (origin, chatPath string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", false
	}
	origin = u.Scheme + "://" + u.Host
	p := strings.TrimSuffix(u.Path, "/")
	chatPath = p + "/chat/completions"
	return origin, chatPath, true
}

// kimiHeadroomBase reports whether base points at the headroom OpenAI
// front. The gateway form is accepted for configs written by older
// versions so unwire still matches them.
func kimiHeadroomBase(base string) bool {
	return sameProxyBase(base, ProxyEndpointFor("kimi"))
}

func kimiGatewayBase(base string) bool {
	g := util.BYOKGatewayEndpoint()
	return sameProxyBase(base, g) || sameProxyBase(base, g+"/v1")
}

// kimiProxyBase matches either the headroom front (current) or the
// gateway (legacy) form.
func kimiProxyBase(base string) bool {
	return kimiHeadroomBase(base) || kimiGatewayBase(base)
}

// kimiHasGatewayProvider reports whether an eligible provider already points at
// the headroom front or gateway without a verified stash entry.
func kimiHasGatewayProvider(raw string) bool {
	stashed := loadProxyRouteStash("kimi")
	for _, p := range kimiScanProviders(raw) {
		block := raw[p.start:p.end]
		if kimiProviderProtocol(kimiBlockField(block, "type")) == "" {
			continue
		}
		if !kimiProxyBase(kimiBlockField(block, "base_url")) {
			continue
		}
		if _, ok := stashed[p.id]; ok {
			continue
		}
		return true
	}
	return false
}

func kimiSubCustomRe(id string) *regexp.Regexp {
	q := regexp.QuoteMeta(id)
	return regexp.MustCompile(`(?m)^[ \t]*\[\s*providers\s*\.\s*(?:"` + q + `"|'` + q + `'|` + q + `)\s*\.\s*custom_headers\s*\](?:[ \t]*#.*)?$`)
}

func kimiInlinePairs(inner string) [][2]string {
	var pairs [][2]string
	for _, m := range reKimiInlinePair.FindAllStringSubmatch(inner, -1) {
		key := m[1]
		if key == "" {
			key = m[2]
		}
		val := m[3]
		if val == "" {
			val = strings.ReplaceAll(m[4], "''", "'")
		}
		pairs = append(pairs, [2]string{key, val})
	}
	return pairs
}

func kimiRenderInlinePairs(pairs [][2]string) string {
	parts := make([]string, len(pairs))
	for i, kv := range pairs {
		parts[i] = `"` + kv[0] + `" = ` + util.TomlQuoted(kv[1])
	}
	return strings.Join(parts, ", ")
}

func kimiUpsertInlinePair(pairs [][2]string, key, value string) [][2]string {
	for i := range pairs {
		if pairs[i][0] == key {
			pairs[i][1] = value
			return pairs
		}
	}
	return append(pairs, [2]string{key, value})
}

// kimiSetBaseURL rewrites the provider's base_url scalar.
func kimiSetBaseURL(raw, id, base string) (string, bool) {
	p, ok := kimiFindProvider(raw, id)
	if !ok {
		return raw, false
	}
	block := raw[p.start:p.end]
	loc := reKimiBaseURL.FindStringSubmatchIndex(block)
	if loc == nil {
		return raw, false
	}
	newBlock := block[:loc[0]] + block[loc[2]:loc[3]] + util.TomlQuoted(base) + block[loc[1]:]
	return raw[:p.start] + newBlock + raw[p.end:], true
}

// kimiSetRouteHeaderValue writes custom_headers."X-Tokless-Route" = header,
// handling inline tables, existing sub-tables, and creating a sub-table.
func kimiSetRouteHeaderValue(raw, id, header string) (string, bool) {
	return kimiSetCustomHeader(raw, id, kimiRouteHeader, header)
}

// kimiSetCustomHeader writes custom_headers.<key> = value for any header
// key, handling inline tables, existing sub-tables, and creating a
// sub-table.
func kimiSetCustomHeader(raw, id, key, value string) (string, bool) {
	p, ok := kimiFindProvider(raw, id)
	if !ok {
		return raw, false
	}
	block := raw[p.start:p.end]
	if loc := reKimiInlineCustom.FindStringSubmatchIndex(block); loc != nil {
		indent := block[loc[2]:loc[3]]
		pairs := kimiUpsertInlinePair(kimiInlinePairs(block[loc[4]:loc[5]]), key, value)
		line := indent + "custom_headers = { " + kimiRenderInlinePairs(pairs) + " }"
		newBlock := block[:loc[0]] + line + block[loc[1]:]
		return raw[:p.start] + newBlock + raw[p.end:], true
	}
	if re := kimiSubCustomRe(id); re.MatchString(raw) {
		loc := re.FindStringIndex(raw)
		end := len(raw)
		if next := reKimiNextHeader.FindStringIndex(raw[loc[1]:]); next != nil {
			end = loc[1] + next[0]
		}
		sub, ok := kimiSetSubHeaderLine(raw[loc[0]:end], key, value)
		if !ok {
			return raw, false
		}
		return raw[:loc[0]] + sub + raw[end:], true
	}
	sub := "\n[providers." + util.TomlQuoted(id) + ".custom_headers]\n" + key + " = " + util.TomlQuoted(value) + "\n"
	return raw[:p.end] + sub + raw[p.end:], true
}

// kimiSetSubHeaderLine sets key = value inside a [providers.<id>.custom_headers]
// sub-table, replacing an existing entry for the same key.
func kimiSetSubHeaderLine(sub, key, value string) (string, bool) {
	re := kimiHeaderLineRe(key)
	loc := re.FindStringSubmatchIndex(sub)
	if loc != nil {
		return sub[:loc[0]] + sub[loc[2]:loc[3]] + key + " = " + util.TomlQuoted(value) + sub[loc[1]:], true
	}
	line := key + " = " + util.TomlQuoted(value) + "\n"
	if nl := strings.IndexByte(sub, '\n'); nl >= 0 {
		return sub[:nl+1] + line + sub[nl+1:], true
	}
	return sub + "\n" + line, true
}

// kimiHeaderLineRe matches a `key = "value"` (or single-quoted) line for
// any custom-header key inside a custom_headers table.
func kimiHeaderLineRe(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^([ \t]*)(?:` + regexp.QuoteMeta(key) + `|"` + regexp.QuoteMeta(key) + `"|'` + regexp.QuoteMeta(key) + `')[ \t]*=[ \t]*(?:"((?:[^"\\]|\\.)*)"|'([^']*)')`)
}

// kimiProviderHeaderValue reads the current X-Tokless-Route value, if any.
func kimiProviderHeaderValue(raw, id string) (string, bool) {
	return kimiCustomHeaderValue(raw, id, kimiRouteHeader)
}

// kimiCustomHeaderValue reads the current value of a custom_headers key.
func kimiCustomHeaderValue(raw, id, key string) (string, bool) {
	p, ok := kimiFindProvider(raw, id)
	if !ok {
		return "", false
	}
	block := raw[p.start:p.end]
	if loc := reKimiInlineCustom.FindStringSubmatchIndex(block); loc != nil {
		for _, kv := range kimiInlinePairs(block[loc[4]:loc[5]]) {
			if kv[0] == key {
				return kv[1], true
			}
		}
	}
	re := kimiSubCustomRe(id)
	loc := re.FindStringIndex(raw)
	if loc == nil {
		return "", false
	}
	end := len(raw)
	if next := reKimiNextHeader.FindStringIndex(raw[loc[1]:]); next != nil {
		end = loc[1] + next[0]
	}
	m := kimiHeaderLineRe(key).FindStringSubmatch(raw[loc[0]:end])
	if m == nil {
		return "", false
	}
	if m[2] != "" {
		if v, err := strconv.Unquote(`"` + m[2] + `"`); err == nil {
			return v, true
		}
		return m[2], true
	}
	return strings.ReplaceAll(m[3], "''", "'"), true
}

// kimiRemoveHeader restores or drops the managed route header and drops
// the headroom-front headers tokless added. The x-headroom-* keys never
// pre-existed, so they are always removed.
func kimiRemoveHeader(raw, id string, had bool, header string) (string, bool) {
	// Drop the two headroom-front headers first; they are always ours.
	for _, key := range []string{kimiHeadroomBaseURLHeader, kimiHeadroomOriginalPathHdr} {
		if removed, ok := kimiDropCustomHeader(raw, id, key); ok {
			raw = removed
		}
	}
	p, ok := kimiFindProvider(raw, id)
	if !ok {
		return raw, false
	}
	block := raw[p.start:p.end]
	if loc := reKimiInlineCustom.FindStringSubmatchIndex(block); loc != nil {
		indent := block[loc[2]:loc[3]]
		pairs := kimiInlinePairs(block[loc[4]:loc[5]])
		kept := pairs[:0]
		found := false
		for _, kv := range pairs {
			if kv[0] != kimiRouteHeader {
				kept = append(kept, kv)
				continue
			}
			found = true
			if had {
				kept = append(kept, [2]string{kimiRouteHeader, header})
			}
		}
		if !found && !had {
			return raw, false
		}
		var line string
		if len(kept) == 0 {
			line = ""
		} else {
			line = indent + "custom_headers = { " + kimiRenderInlinePairs(kept) + " }"
		}
		newBlock := block[:loc[0]] + line + block[loc[1]:]
		return raw[:p.start] + newBlock + raw[p.end:], true
	}
	re := kimiSubCustomRe(id)
	loc := re.FindStringIndex(raw)
	if loc == nil {
		return raw, false
	}
	end := len(raw)
	if next := reKimiNextHeader.FindStringIndex(raw[loc[1]:]); next != nil {
		end = loc[1] + next[0]
	}
	sub := raw[loc[0]:end]
	hloc := kimiHeaderLineRe(kimiRouteHeader).FindStringSubmatchIndex(sub)
	if hloc == nil {
		return raw, false
	}
	var newSub string
	if had {
		newSub = sub[:hloc[0]] + sub[hloc[2]:hloc[3]] + kimiRouteHeader + " = " + util.TomlQuoted(header) + sub[hloc[1]:]
	} else {
		lineEnd := hloc[1]
		for lineEnd < len(sub) && sub[lineEnd] == '\n' {
			lineEnd++
		}
		newSub = sub[:hloc[0]] + sub[lineEnd:]
		// Drop the sub-table header too when it no longer has any content,
		// along with the newline we inserted before it, to restore the file.
		if nl := strings.IndexByte(newSub, '\n'); nl >= 0 && strings.TrimSpace(newSub[nl+1:]) == "" {
			start := loc[0]
			if start > 0 && raw[start-1] == '\n' {
				start--
			}
			return raw[:start] + raw[end:], true
		}
	}
	return raw[:loc[0]] + newSub + raw[end:], true
}

// kimiDropCustomHeader removes a key from a provider's custom_headers,
// dropping an emptied sub-table. ok is false when the key is absent.
func kimiDropCustomHeader(raw, id, key string) (string, bool) {
	p, ok := kimiFindProvider(raw, id)
	if !ok {
		return raw, false
	}
	block := raw[p.start:p.end]
	if loc := reKimiInlineCustom.FindStringSubmatchIndex(block); loc != nil {
		indent := block[loc[2]:loc[3]]
		pairs := kimiInlinePairs(block[loc[4]:loc[5]])
		kept := pairs[:0]
		found := false
		for _, kv := range pairs {
			if kv[0] == key {
				found = true
				continue
			}
			kept = append(kept, kv)
		}
		if !found {
			return raw, false
		}
		var line string
		if len(kept) == 0 {
			line = ""
		} else {
			line = indent + "custom_headers = { " + kimiRenderInlinePairs(kept) + " }"
		}
		newBlock := block[:loc[0]] + line + block[loc[1]:]
		return raw[:p.start] + newBlock + raw[p.end:], true
	}
	re := kimiSubCustomRe(id)
	loc := re.FindStringIndex(raw)
	if loc == nil {
		return raw, false
	}
	end := len(raw)
	if next := reKimiNextHeader.FindStringIndex(raw[loc[1]:]); next != nil {
		end = loc[1] + next[0]
	}
	sub := raw[loc[0]:end]
	hloc := kimiHeaderLineRe(key).FindStringSubmatchIndex(sub)
	if hloc == nil {
		return raw, false
	}
	lineEnd := hloc[1]
	for lineEnd < len(sub) && sub[lineEnd] == '\n' {
		lineEnd++
	}
	newSub := sub[:hloc[0]] + sub[lineEnd:]
	if nl := strings.IndexByte(newSub, '\n'); nl >= 0 && strings.TrimSpace(newSub[nl+1:]) == "" {
		start := loc[0]
		if start > 0 && raw[start-1] == '\n' {
			start--
		}
		return raw[:start] + raw[end:], true
	}
	return raw[:loc[0]] + newSub + raw[end:], true
}

// kimiApplyRoute moves one provider onto the gateway. Exposed for tests.
func kimiApplyRoute(raw, id, base, header string) (string, bool) {
	next, ok := kimiSetBaseURL(raw, id, base)
	if !ok {
		return raw, false
	}
	return kimiSetRouteHeaderValue(next, id, header)
}

// ConfigureKimiProxy rewrites Kimi's OpenAI-compatible providers onto the
// headroom front, registering routes and stashing originals for removal.
func ConfigureKimiProxy() (changed bool, file string) {
	file = util.KimiPathsResolved().Config
	if err := withProxyRouteStashLock(func() error {
		changed, file = configureKimiProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("kimi proxy lock failed: " + err.Error())
	}
	return changed, file
}

func configureKimiProxyLocked() (changed bool, file string) {
	file = util.KimiPathsResolved().Config
	if !proxyRouteStashValid("kimi") {
		return false, file
	}
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false, file
	}
	providers := DiscoverKimiBYOK()
	if len(providers) == 0 {
		return false, file
	}
	stash := loadProxyRouteStashLocked("kimi")
	stashRaw, stashExists := util.ReadFileSafe(proxyRouteStashPath("kimi"))
	var routeChanges []byokRouteRollback
	rolledBack := false
	rollback := func() {
		if rolledBack {
			return
		}
		rolledBack = true
		for i := len(routeChanges) - 1; i >= 0; i-- {
			rollbackBYOKRouteLogged("kimi", routeChanges[i].id, routeChanges[i].previous)
		}
		restoreProxyRouteStashLogged("kimi", stashRaw, stashExists)
	}

	next := raw
	changedAny := false
	headroomBase := ProxyEndpointFor("kimi")
	for _, provider := range providers {
		if !isValidBYOKUpstream(provider.BaseURL) {
			continue
		}
		origin, chatPath, ok := kimiHeadroomUpstream(provider.BaseURL)
		if !ok {
			util.L.Warn("kimi: skipping provider with unparseable base_url: " + provider.ID)
			continue
		}
		upstream := normalizedHeadroomUpstream(provider.BaseURL, provider.Protocol)
		_, routeHeader, previous, routeOK := registerBYOKRoute("kimi", provider.ID, provider.Protocol, upstream)
		if !routeOK {
			rollback()
			return false, file
		}
		routeChanges = append(routeChanges, byokRouteRollback{id: provider.ID, previous: previous})
		header, hadHeader := kimiProviderHeaderValue(next, provider.ID)
		updated, ok := kimiApplyRoute(next, provider.ID, headroomBase, routeHeader)
		if !ok {
			rollback()
			return false, file
		}
		// Headroom needs the real upstream to forward the compressed
		// request, mirroring the opencode headroom front.
		updated, ok = kimiSetCustomHeader(updated, provider.ID, kimiHeadroomBaseURLHeader, origin)
		if !ok {
			rollback()
			return false, file
		}
		updated, ok = kimiSetCustomHeader(updated, provider.ID, kimiHeadroomOriginalPathHdr, chatPath)
		if !ok {
			rollback()
			return false, file
		}
		if updated != next {
			changedAny = true
		}
		next = updated
		stash[provider.ID] = proxyRouteStashEntry{
			File:      file,
			Provider:  provider.ID,
			BaseURL:   provider.BaseURL,
			Upstream:  upstream,
			HadHeader: hadHeader,
			Header:    header,
		}
	}
	if !changedAny {
		rollback()
		return false, file
	}
	if err := saveProxyRouteStash("kimi", stash); err != nil {
		rollback()
		return false, file
	}
	if err := util.WriteFileAtomic(file, next, 0o600); err != nil {
		rollback()
		return false, file
	}
	return true, file
}

// RemoveKimiProxy restores stashed originals and deletes kimi: routes only when
// the config still matches what tokless wrote.
func RemoveKimiProxy() bool {
	removed := false
	if err := withProxyRouteStashLock(func() error {
		removed = removeKimiProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("kimi proxy lock failed: " + err.Error())
	}
	return removed
}

func removeKimiProxyLocked() bool {
	if !proxyRouteStashValid("kimi") {
		return false
	}
	file := util.KimiPathsResolved().Config
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false
	}
	stash := loadProxyRouteStashLocked("kimi")
	if len(stash) == 0 {
		return false
	}
	next := raw
	remaining := map[string]proxyRouteStashEntry{}
	var deletedRoutes []BYOKRoute
	var deletedIDs []string
	for id, entry := range stash {
		p, found := kimiFindProvider(next, id)
		if !found {
			remaining[id] = entry
			continue
		}
		block := next[p.start:p.end]
		base := kimiBlockField(block, "base_url")
		route, routeExists := util.ReadBYOKRoute("kimi:" + id)
		value, has := kimiProviderHeaderValue(next, id)
		matches := routeExists && kimiProxyBase(base) && has && value == util.BYOKRouteHeader(route)
		if !matches {
			remaining[id] = entry
			continue
		}
		updated, ok := kimiRemoveHeader(next, id, entry.HadHeader, entry.Header)
		if !ok {
			updated = next
		}
		updated, ok = kimiSetBaseURL(updated, id, entry.BaseURL)
		if !ok {
			remaining[id] = entry
			continue
		}
		next = updated
		deletedRoutes = append(deletedRoutes, route)
		deletedIDs = append(deletedIDs, id)
	}
	if len(deletedIDs) == 0 {
		return false
	}
	if err := util.WriteFileAtomic(file, next, 0o600); err != nil {
		return false
	}
	for _, id := range deletedIDs {
		if err := deleteBYOKRoute("kimi", id); err != nil {
			_ = util.WriteFile(file, raw)
			return false
		}
	}
	if err := saveProxyRouteStash("kimi", remaining); err != nil {
		_ = util.WriteFile(file, raw)
		for _, route := range deletedRoutes {
			_, _, _ = util.UpsertBYOKRoute(route.ID, route.Protocol, route.Upstream)
		}
		return false
	}
	return true
}

// KimiProxyWired reports whether every stashed provider still points at
// the headroom front with the correct X-Tokless-Route header and the
// headroom-forward headers.
func KimiProxyWired() bool {
	stash := loadProxyRouteStash("kimi")
	if len(stash) == 0 {
		return false
	}
	raw, ok := util.ReadFileSafe(util.KimiPathsResolved().Config)
	if !ok {
		return false
	}
	for id := range stash {
		p, found := kimiFindProvider(raw, id)
		if !found {
			return false
		}
		block := raw[p.start:p.end]
		if !kimiHeadroomBase(kimiBlockField(block, "base_url")) {
			return false
		}
		route, routeExists := util.ReadBYOKRoute("kimi:" + id)
		value, has := kimiProviderHeaderValue(raw, id)
		if !routeExists || !has || value != util.BYOKRouteHeader(route) {
			return false
		}
		origin, chatPath, ok := kimiHeadroomUpstream(stash[id].BaseURL)
		if !ok {
			return false
		}
		if v, has := kimiCustomHeaderValue(raw, id, kimiHeadroomBaseURLHeader); !has || v != origin {
			return false
		}
		if v, has := kimiCustomHeaderValue(raw, id, kimiHeadroomOriginalPathHdr); !has || v != chatPath {
			return false
		}
	}
	return true
}
