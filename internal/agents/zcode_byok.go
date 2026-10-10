package agents

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// zcode stores its BYOK provider config in ~/.zcode/v2/provider_config.json
// (schemaVersion 1): config.providerConfigRules.providerRules is a list of
// {providerId, providerName, config:{...}} rows. Each row's config.api is
// {type, baseUrl, headers} and config.access carries the credential.
// Provider rows are rewritten onto the headroom OpenAI front exactly like
// command-code: baseUrl points at 8787/v1 and the real upstream travels
// in x-headroom-base-url / x-headroom-original-path, mirroring opencode.

// zcodeHeadroomBaseURLHeader and zcodeHeadroomOriginalPathHdr carry the
// real upstream so headroom compresses and forwards, mirroring opencode.
const (
	zcodeHeadroomBaseURLHeader   = "x-headroom-base-url"
	zcodeHeadroomOriginalPathHdr = "x-headroom-original-path"
)

// zcodeProviderConfigPath is the v2 provider config tokless rewrites.
func zcodeProviderConfigPath() string {
	return filepath.Join(util.Home(), ".zcode", "v2", "provider_config.json")
}

// zcodeProviderProtocol maps a zcode api.type to the BYOK route protocol.
// Only transports the headroom front understands are eligible.
func zcodeProviderProtocol(apiType string) string {
	switch strings.ToLower(strings.TrimSpace(apiType)) {
	case "", "openai-completions", "openai-chat-completions", "openai_chat_completions":
		return "openai-completions"
	case "openai-responses", "openai_responses":
		return "openai-responses"
	case "anthropic-messages", "anthropic_messages":
		return "anthropic-messages"
	}
	return ""
}

func zcodeStringField(m *util.OrderedMap, key string) string {
	v, ok := m.Get(key)
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// zcodeBaseURL reads the provider's base URL, accepting baseUrl and baseURL.
func zcodeBaseURL(m *util.OrderedMap) string {
	if s := zcodeStringField(m, "baseURL"); s != "" {
		return s
	}
	return zcodeStringField(m, "baseUrl")
}

// zcodeGatewayBase accepts the bare gateway and the /v1 form.
func zcodeGatewayBase(base string) bool {
	g := util.BYOKGatewayEndpoint()
	return sameProxyBase(base, g) || sameProxyBase(base, g+"/v1")
}

// zcodeHeadroomBase reports whether base points at the headroom front.
func zcodeHeadroomBase(base string) bool {
	return sameProxyBase(base, ProxyEndpointFor("zcode"))
}

// zcodeProxyBase matches either the headroom front (current) or the
// gateway (legacy) form.
func zcodeProxyBase(base string) bool {
	return zcodeHeadroomBase(base) || zcodeGatewayBase(base)
}

// zcodeHeadroomUpstream splits a real provider base URL into the origin
// headroom forwards to (x-headroom-base-url) and the chat-completions
// path (x-headroom-original-path). ok is false when the URL cannot be
// parsed, in which case the provider must not be pointed at headroom.
func zcodeHeadroomUpstream(baseURL string) (origin, chatPath string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", false
	}
	origin = u.Scheme + "://" + u.Host
	p := strings.TrimSuffix(u.Path, "/")
	chatPath = p + "/chat/completions"
	return origin, chatPath, true
}

// zcodeHeaders reads the provider api.headers object. ok is false when a
// headers slot exists but is not an object, or the managed header is not
// a string.
func zcodeHeaders(m *util.OrderedMap) (headers *util.OrderedMap, had bool, value string, ok bool) {
	v, exists := m.Get("headers")
	if !exists {
		return nil, false, "", true
	}
	hm, isMap := v.(*util.OrderedMap)
	if !isMap {
		return nil, false, "", false
	}
	rv, has := hm.Get(byokRouteHeaderKey)
	if !has {
		return hm, false, "", true
	}
	s, isStr := rv.(string)
	if !isStr {
		return nil, false, "", false
	}
	return hm, true, s, true
}

// zcodeProviderRule locates a providerRules entry by providerId.
type zcodeProviderRule struct {
	index int
	block *util.OrderedMap
}

func zcodeScanProviderRules(cfg *util.OrderedMap) []zcodeProviderRule {
	config, ok := mapChild(cfg, "config")
	if !ok {
		return nil
	}
	rules, ok := mapChild(config, "providerConfigRules")
	if !ok {
		return nil
	}
	list, has := rules.Get("providerRules")
	if !has {
		return nil
	}
	entries, isList := list.([]any)
	if !isList {
		return nil
	}
	var out []zcodeProviderRule
	for i, entry := range entries {
		m, ok := entry.(*util.OrderedMap)
		if !ok {
			continue
		}
		if zcodeStringField(m, "providerId") == "" {
			continue
		}
		out = append(out, zcodeProviderRule{index: i, block: m})
	}
	return out
}

func zcodeFindProviderRule(cfg *util.OrderedMap, id string) (zcodeProviderRule, bool) {
	for _, rule := range zcodeScanProviderRules(cfg) {
		if zcodeStringField(rule.block, "providerId") == id {
			return rule, true
		}
	}
	return zcodeProviderRule{}, false
}

// zcodeRuleAPI returns the provider's config.api object.
func zcodeRuleAPI(rule zcodeProviderRule) (*util.OrderedMap, bool) {
	config, ok := mapChild(rule.block, "config")
	if !ok {
		return nil, false
	}
	api, ok := mapChild(config, "api")
	if !ok {
		return nil, false
	}
	return api, true
}

// zcodeRuleCredential reads the provider credential from config.access.
func zcodeRuleCredential(rule zcodeProviderRule) string {
	config, ok := mapChild(rule.block, "config")
	if !ok {
		return ""
	}
	access, ok := mapChild(config, "access")
	if !ok {
		return ""
	}
	return zcodeStringField(access, "apiKey")
}

// zcodeLegacyProviderRules reads the pre-array form where
// providerConfigRules is a map keyed by provider id.
func zcodeLegacyProviderRules(cfg *util.OrderedMap) (*util.OrderedMap, bool) {
	config, ok := mapChild(cfg, "config")
	if !ok {
		return nil, false
	}
	rules, ok := mapChild(config, "providerConfigRules")
	if !ok {
		return nil, false
	}
	return rules, true
}

// DiscoverZcodeBYOK finds zcode providers with a real upstream that are
// not already on the proxy/gateway. Providers already rewritten are
// recovered from the stash so a re-run stays idempotent.
func DiscoverZcodeBYOK() []openCodeBYOK {
	path := zcodeProviderConfigPath()
	raw, ok := util.ReadFileSafe(path)
	if !ok || util.HasJSONCComments(raw) {
		return nil
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return nil
	}
	proxyBase := strings.TrimRight(ProxyEndpointFor("zcode"), "/")
	gatewayBase := strings.TrimRight(util.BYOKGatewayEndpoint(), "/")
	stashed := loadProxyRouteStash("zcode")
	var out []openCodeBYOK
	for _, rule := range zcodeScanProviderRules(cfg) {
		id := zcodeStringField(rule.block, "providerId")
		api, ok := zcodeRuleAPI(rule)
		if !ok {
			continue
		}
		proto := zcodeProviderProtocol(zcodeStringField(api, "type"))
		if proto == "" {
			continue
		}
		base := zcodeBaseURL(api)
		if isAbsoluteHTTP(base) && isValidBYOKUpstream(base) &&
			!sameProxyBase(base, proxyBase) && !sameProxyBase(base, gatewayBase) && !sameProxyBase(base, gatewayBase+"/v1") {
			// keep the on-disk original
		} else if s, ok := stashed[id]; ok && s.BaseURL != "" {
			base = s.BaseURL
		} else if routeID := zcodeRouteIDFor(api); routeID != "" {
			// Legacy gateway rewrite without a stash: recover the
			// real upstream tokless registered for this provider.
			if route, exists := util.ReadBYOKRoute(routeID); exists &&
				isAbsoluteHTTP(route.Upstream) && isValidBYOKUpstream(route.Upstream) {
				base = routeUpstreamToBaseURL(route.Upstream, proto)
			} else {
				base = ""
			}
		} else {
			base = ""
		}
		if base == "" {
			continue
		}
		out = append(out, openCodeBYOK{
			ID:       id,
			File:     path,
			BaseURL:  base,
			APIKey:   zcodeRuleCredential(rule),
			Protocol: proto,
		})
	}
	return out
}

// zcodeHasGatewayProvider reports whether an eligible provider already
// points at the headroom front or gateway without a verified stash.
func zcodeHasGatewayProvider(raw string) bool {
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	stashed := loadProxyRouteStash("zcode")
	for _, rule := range zcodeScanProviderRules(cfg) {
		id := zcodeStringField(rule.block, "providerId")
		api, ok := zcodeRuleAPI(rule)
		if !ok {
			continue
		}
		if zcodeProviderProtocol(zcodeStringField(api, "type")) == "" {
			continue
		}
		if !zcodeProxyBase(zcodeBaseURL(api)) {
			continue
		}
		if _, ok := stashed[id]; ok {
			continue
		}
		return true
	}
	return false
}

// zcodeRouteIDFor recovers the BYOK route id a gateway-pointing
// provider is bound to. The route id predates any providerId
// rename, so match on the route token carried in the provider's
// X-Tokless-Route header rather than on providerId.
func zcodeRouteIDFor(api *util.OrderedMap) string {
	headers, _, _, hdrOK := zcodeHeaders(api)
	if !hdrOK || headers == nil {
		return ""
	}
	value, _ := headers.Get(byokRouteHeaderKey)
	header, _ := value.(string)
	if header == "" {
		return ""
	}
	prefix, token, found := strings.Cut(header, ".")
	if !found || token == "" {
		return ""
	}
	if !strings.HasPrefix(prefix, "zcode:") {
		return ""
	}
	route, exists := util.ReadBYOKRoute(prefix)
	if !exists || route.Token != token {
		return ""
	}
	return prefix
}

// zcodeGatewayRecoverable reports whether every gateway-pointing
// provider maps to a BYOK route that still records the real
// upstream. proxy up can then re-point the provider at the headroom
// front and rebuild the stash from the route's upstream.
func zcodeGatewayRecoverable(raw string) bool {
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	stashed := loadProxyRouteStash("zcode")
	for _, rule := range zcodeScanProviderRules(cfg) {
		id := zcodeStringField(rule.block, "providerId")
		api, ok := zcodeRuleAPI(rule)
		if !ok {
			continue
		}
		if zcodeProviderProtocol(zcodeStringField(api, "type")) == "" {
			continue
		}
		if !zcodeProxyBase(zcodeBaseURL(api)) {
			continue
		}
		if _, ok := stashed[id]; ok {
			continue
		}
		routeID := zcodeRouteIDFor(api)
		if routeID == "" {
			return false
		}
		route, exists := util.ReadBYOKRoute(routeID)
		if !exists || strings.TrimSpace(route.Upstream) == "" || !isValidBYOKUpstream(route.Upstream) {
			return false
		}
	}
	return true
}

// ConfigureZcodeProxy rewrites zcode providers onto the headroom front,
// registering routes and stashing originals for removal.
func ConfigureZcodeProxy() (changed bool, file string) {
	file = zcodeProviderConfigPath()
	if err := withProxyRouteStashLock(func() error {
		changed, file = configureZcodeProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("zcode proxy lock failed: " + err.Error())
	}
	return changed, file
}

func configureZcodeProxyLocked() (changed bool, file string) {
	file = zcodeProviderConfigPath()
	if !proxyRouteStashValid("zcode") {
		return false, file
	}
	raw, ok := util.ReadFileSafe(file)
	if !ok || util.HasJSONCComments(raw) {
		return false, file
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false, file
	}
	discovered := DiscoverZcodeBYOK()
	if len(discovered) == 0 {
		return false, file
	}
	stash := loadProxyRouteStashLocked("zcode")
	stashRaw, stashExists := util.ReadFileSafe(proxyRouteStashPath("zcode"))
	var routeChanges []byokRouteRollback
	rolledBack := false
	rollback := func() {
		if rolledBack {
			return
		}
		rolledBack = true
		for i := len(routeChanges) - 1; i >= 0; i-- {
			rollbackBYOKRouteLogged("zcode", routeChanges[i].id, routeChanges[i].previous)
		}
		restoreProxyRouteStashLogged("zcode", stashRaw, stashExists)
	}

	headroomBase := ProxyEndpointFor("zcode")
	changedAny := false
	for _, provider := range discovered {
		rule, found := zcodeFindProviderRule(cfg, provider.ID)
		if !found {
			continue
		}
		api, ok := zcodeRuleAPI(rule)
		if !ok {
			continue
		}
		origin, chatPath, upstreamOK := zcodeHeadroomUpstream(provider.BaseURL)
		if !upstreamOK {
			util.L.Warn("zcode: skipping provider with unparseable baseUrl: " + provider.ID)
			continue
		}
		upstream := normalizedHeadroomUpstream(provider.BaseURL, provider.Protocol)
		_, routeHeader, previous, routeOK := registerBYOKRoute("zcode", provider.ID, provider.Protocol, upstream)
		if !routeOK {
			rollback()
			return false, file
		}
		routeChanges = append(routeChanges, byokRouteRollback{id: provider.ID, previous: previous})

		headers, hadHeader, headerValue, hdrOK := zcodeHeaders(api)
		if !hdrOK {
			rollback()
			return false, file
		}
		if zcodeBaseURL(api) != headroomBase {
			if _, has := api.Get("baseUrl"); has {
				api.Set("baseUrl", headroomBase)
				api.Delete("baseURL")
			} else {
				api.Set("baseURL", headroomBase)
			}
			changedAny = true
		}
		if headers == nil {
			headers = util.NewOrderedMap()
			api.Set("headers", headers)
		}
		if v, exists := headers.Get(byokRouteHeaderKey); !exists || v != routeHeader {
			headers.Set(byokRouteHeaderKey, routeHeader)
			changedAny = true
		}
		// Headroom needs the real upstream to forward the compressed
		// request, mirroring the opencode headroom front.
		if v, exists := headers.Get(zcodeHeadroomBaseURLHeader); !exists || v != origin {
			headers.Set(zcodeHeadroomBaseURLHeader, origin)
			changedAny = true
		}
		if v, exists := headers.Get(zcodeHeadroomOriginalPathHdr); !exists || v != chatPath {
			headers.Set(zcodeHeadroomOriginalPathHdr, chatPath)
			changedAny = true
		}
		stash[provider.ID] = proxyRouteStashEntry{
			File:      file,
			Provider:  provider.ID,
			BaseURL:   provider.BaseURL,
			Upstream:  upstream,
			HadHeader: hadHeader,
			Header:    headerValue,
		}
	}
	if !changedAny {
		rollback()
		return false, file
	}
	if err := saveProxyRouteStash("zcode", stash); err != nil {
		rollback()
		return false, file
	}
	if err := util.WriteFileAtomic(file, util.StringifyJSON(cfg), 0o600); err != nil {
		rollback()
		return false, file
	}
	return true, file
}

// RemoveZcodeProxy restores stashed originals and deletes zcode: routes
// only when the config still matches what tokless wrote.
func RemoveZcodeProxy() bool {
	removed := false
	if err := withProxyRouteStashLock(func() error {
		removed = removeZcodeProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("zcode proxy lock failed: " + err.Error())
	}
	return removed
}

func removeZcodeProxyLocked() bool {
	if !proxyRouteStashValid("zcode") {
		return false
	}
	file := zcodeProviderConfigPath()
	raw, ok := util.ReadFileSafe(file)
	if !ok || util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	stash := loadProxyRouteStashLocked("zcode")
	if len(stash) == 0 {
		return false
	}
	remaining := map[string]proxyRouteStashEntry{}
	var deletedRoutes []BYOKRoute
	var deletedIDs []string
	for id, entry := range stash {
		rule, found := zcodeFindProviderRule(cfg, id)
		if !found {
			remaining[id] = entry
			continue
		}
		api, ok := zcodeRuleAPI(rule)
		if !ok {
			remaining[id] = entry
			continue
		}
		route, routeExists := util.ReadBYOKRoute("zcode:" + id)
		headers, _, value, hdrOK := zcodeHeaders(api)
		matches := routeExists && hdrOK && headers != nil && zcodeProxyBase(zcodeBaseURL(api)) &&
			value == util.BYOKRouteHeader(route)
		if !matches {
			remaining[id] = entry
			continue
		}
		if _, has := api.Get("baseUrl"); has {
			api.Set("baseUrl", entry.BaseURL)
			api.Delete("baseURL")
		} else {
			api.Set("baseURL", entry.BaseURL)
		}
		if entry.HadHeader {
			headers.Set(byokRouteHeaderKey, entry.Header)
		} else {
			headers.Delete(byokRouteHeaderKey)
		}
		// The headroom-front headers were added by tokless; drop them.
		headers.Delete(zcodeHeadroomBaseURLHeader)
		headers.Delete(zcodeHeadroomOriginalPathHdr)
		if headers.Len() == 0 {
			api.Delete("headers")
		}
		deletedRoutes = append(deletedRoutes, route)
		deletedIDs = append(deletedIDs, id)
	}
	if len(deletedIDs) == 0 {
		return false
	}
	if err := util.WriteFileAtomic(file, util.StringifyJSON(cfg), 0o600); err != nil {
		return false
	}
	for _, id := range deletedIDs {
		if err := deleteBYOKRoute("zcode", id); err != nil {
			_ = util.WriteFile(file, raw)
			return false
		}
	}
	if err := saveProxyRouteStash("zcode", remaining); err != nil {
		_ = util.WriteFile(file, raw)
		for _, route := range deletedRoutes {
			_, _, _ = util.UpsertBYOKRoute(route.ID, route.Protocol, route.Upstream)
		}
		return false
	}
	return true
}

// ZcodeProxyWired reports whether every stashed provider still points
// at the headroom front with the correct X-Tokless-Route header and
// the headroom-forward headers.
func ZcodeProxyWired() bool {
	stash := loadProxyRouteStash("zcode")
	if len(stash) == 0 {
		return false
	}
	raw, ok := util.ReadFileSafe(zcodeProviderConfigPath())
	if !ok || util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	for id := range stash {
		rule, found := zcodeFindProviderRule(cfg, id)
		if !found {
			return false
		}
		api, ok := zcodeRuleAPI(rule)
		if !ok {
			return false
		}
		if !zcodeHeadroomBase(zcodeBaseURL(api)) {
			return false
		}
		route, routeExists := util.ReadBYOKRoute("zcode:" + id)
		headers, _, value, hdrOK := zcodeHeaders(api)
		if !routeExists || !hdrOK || headers == nil || value != util.BYOKRouteHeader(route) {
			return false
		}
		origin, chatPath, upstreamOK := zcodeHeadroomUpstream(stash[id].BaseURL)
		if !upstreamOK {
			return false
		}
		if v, exists := headers.Get(zcodeHeadroomBaseURLHeader); !exists || v != origin {
			return false
		}
		if v, exists := headers.Get(zcodeHeadroomOriginalPathHdr); !exists || v != chatPath {
			return false
		}
	}
	return true
}
