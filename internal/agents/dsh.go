package agents

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// dsh is the DeepSeek Harness. Its provider config is YAML in
// $DSH_HOME/cordis.patch.yml. Providers live under the llm-pi-ai plugin's
// config.providers.<id> mapping, which exposes baseURL and a headers map, so
// dsh can be routed through the headroom OpenAI front exactly like
// command-code: baseURL points at 8787/v1 and the real upstream travels in
// x-headroom-base-url / x-headroom-original-path.
var dsh = &core.AgentManifest{
	ID:        "dsh",
	Label:     "DeepSeek Harness",
	Homepage:  "https://github.com/deepseek-ai/deepseek-harness",
	CLIBin:    "dsh",
	ConfigDir: dshHome,
	Detect: func() core.Detection {
		return detectAgent("dsh", dshHome(), util.ExpectedBinDirs(), nil)
	},
}

func dshHome() string {
	home := os.Getenv("DSH_HOME")
	if home == "~" {
		return util.Home()
	}
	if strings.HasPrefix(home, "~/") {
		return filepath.Join(util.Home(), home[2:])
	}
	if home != "" {
		return home
	}
	return filepath.Join(util.Home(), ".dsh")
}

// dshPatchFile is the home-level cordis patch that carries custom providers.
func dshPatchFile() string {
	return filepath.Join(dshHome(), "cordis.patch.yml")
}

// DshInstructionsFile returns the global AGENTS.md path for DeepSeek Harness.
func DshInstructionsFile() string {
	return filepath.Join(dshHome(), "AGENTS.md")
}

// DshHooksBridgeEntry is the cordis.patch.yml row that mounts
// @deepseek-ai/dsh-hooks-claude-code so dsh runs Claude Code
// command hooks. Its config.configPath points at a hooks.json.
const DshHooksBridgeEntry = "hooks-claude-code"

// dshHooksFile is the Claude Code hooks.json the dsh bridge reads.
func dshHooksFile() string {
	return filepath.Join(dshHome(), "hooks.json")
}

// Headroom-front headers. dsh providers point baseURL at the headroom OpenAI
// front (8787/v1) and carry the real upstream in these two headers so headroom
// compresses and forwards to the actual API, mirroring opencode.
const (
	dshHeadroomBaseURLHeader   = "x-headroom-base-url"
	dshHeadroomOriginalPathHdr = "x-headroom-original-path"
)

// dshProviderProtocol maps a dsh provider `api` to the BYOK route protocol.
// An omitted api defaults to openai-completions. Only transports the headroom
// front understands are eligible.
func dshProviderProtocol(api string) string {
	switch strings.ToLower(strings.TrimSpace(api)) {
	case "", "openai-completions", "openai-chat", "openai_chat", "openai_completions":
		return "openai-completions"
	case "openai-responses", "openai_responses":
		return "openai-responses"
	case "anthropic-messages", "anthropic_messages":
		return "anthropic-messages"
	}
	return ""
}

// dshHeadroomUpstream splits a real provider base URL into the origin headroom
// forwards to (x-headroom-base-url) and the chat-completions path
// (x-headroom-original-path). ok is false when the URL cannot be parsed.
func dshHeadroomUpstream(baseURL string) (origin, chatPath string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", false
	}
	origin = u.Scheme + "://" + u.Host
	p := strings.TrimSuffix(u.Path, "/")
	chatPath = p + "/chat/completions"
	return origin, chatPath, true
}

// dshHeadroomBase reports whether base points at the headroom OpenAI front.
func dshHeadroomBase(base string) bool {
	return sameProxyBase(base, ProxyEndpointFor("dsh"))
}

func dshGatewayBase(base string) bool {
	g := util.BYOKGatewayEndpoint()
	return sameProxyBase(base, g) || sameProxyBase(base, g+"/v1")
}

// dshProxyBase matches either the headroom front (current) or the gateway
// (legacy) form.
func dshProxyBase(base string) bool {
	return dshHeadroomBase(base) || dshGatewayBase(base)
}

// dshFindPluginBlock returns the line span of the plugin block whose id equals
// pluginID. The file is a YAML list; each item starts with `- id: <plugin>`.
func dshFindPluginBlock(lines []string, pluginID string) (start, end int, ok bool) {
	want := "- id: " + pluginID
	start = -1
	for i, l := range lines {
		if strings.TrimSpace(l) == want || strings.TrimRight(l, " \t") == want {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "- id: ") {
			end = i
			break
		}
	}
	return start, end, true
}

// dshProviderSpan locates the `providers:` mapping and a named provider entry
// inside a plugin block's config. Returns line indices for the providers key,
// the provider key, and the provider section end.
func dshProviderSpan(lines []string, start, end int, providerID string) (provKey, provLine, provEnd int, ok bool) {
	provKey, provLine, provEnd = -1, -1, -1
	provIndent := -1
	for i := start + 1; i < end; i++ {
		ind, key, _, _ := yamlKeyLine(lines[i])
		if key == "providers" {
			provKey, provIndent = i, ind
			break
		}
	}
	if provKey < 0 {
		return -1, -1, -1, false
	}
	directIndent := -1
	for i := provKey + 1; i < end; i++ {
		if ind, _, _, _ := yamlKeyLine(lines[i]); ind > provIndent {
			directIndent = ind
			break
		}
	}
	if directIndent < 0 {
		return provKey, -1, -1, false
	}
	for i := provKey + 1; i < end; i++ {
		ind, key, _, _ := yamlKeyLine(lines[i])
		if ind == directIndent && key == providerID {
			provLine = i
			break
		}
	}
	if provLine < 0 {
		return provKey, -1, -1, false
	}
	provEnd = end
	for i := provLine + 1; i < end; i++ {
		ind, _, _, _ := yamlKeyLine(lines[i])
		if ind >= 0 && ind <= directIndent {
			provEnd = i
			break
		}
	}
	return provKey, provLine, provEnd, true
}

// dshProviderField reads a scalar field inside a provider section.
func dshProviderField(lines []string, provLine, provEnd int, key string) (value string, line int, has bool) {
	for i := provLine + 1; i < provEnd; i++ {
		_, k, hv, v := yamlKeyLine(lines[i])
		if k == key {
			return v, i, hv
		}
	}
	return "", -1, false
}

// dshHeaderSpan locates the headers sub-map inside a provider section and
// returns its key line, indent, end, and the line of a named header.
func dshHeaderSpan(lines []string, provLine, provEnd int) (hdrKey, hdrIndent, hdrEnd int, ok bool) {
	hdrKey, hdrIndent, hdrEnd = -1, -1, -1
	for i := provLine + 1; i < provEnd; i++ {
		_, key, hasVal, _ := yamlKeyLine(lines[i])
		if key == "headers" && !hasVal {
			ind, _, _, _ := yamlKeyLine(lines[i])
			hdrKey, hdrIndent = i, ind
			break
		}
	}
	if hdrKey < 0 {
		return -1, -1, -1, false
	}
	hdrEnd = provEnd
	for i := hdrKey + 1; i < provEnd; i++ {
		ind, _, _, _ := yamlKeyLine(lines[i])
		if ind >= 0 && ind <= hdrIndent {
			hdrEnd = i
			break
		}
	}
	return hdrKey, hdrIndent, hdrEnd, true
}

// dshHeaderValue reads a named header inside a provider's headers map.
func dshHeaderValue(lines []string, provLine, provEnd int, key string) (string, bool) {
	hdrKey, hdrIndent, hdrEnd, ok := dshHeaderSpan(lines, provLine, provEnd)
	if !ok {
		return "", false
	}
	for i := hdrKey + 1; i < hdrEnd; i++ {
		ind, k, hv, v := yamlKeyLine(lines[i])
		if ind > hdrIndent && k == key {
			return v, hv
		}
	}
	return "", false
}

// dshSetProviderField rewrites a scalar field inside a provider section,
// preserving indentation and trailing comment position where feasible.
func dshSetProviderField(lines []string, line int, key, value string) {
	ind, _, _, _ := yamlKeyLine(lines[line])
	lines[line] = strings.Repeat(" ", ind) + key + ": " + value
}

// dshUpsertHeader inserts or replaces a header entry inside a provider's
// headers map, creating the map when absent.
func dshUpsertHeader(lines []string, provLine, provEnd int, key, value string) ([]string, int) {
	hdrKey, hdrIndent, hdrEnd, ok := dshHeaderSpan(lines, provLine, provEnd)
	if !ok {
		provIndent, _, _, _ := yamlKeyLine(lines[provLine])
		insert := provIndent + 2
		lines = dshInsertLine(lines, provEnd, strings.Repeat(" ", insert)+"headers:")
		lines = dshInsertLine(lines, provEnd+1, strings.Repeat(" ", insert+2)+key+": "+value)
		return lines, 2
	}
	for i := hdrKey + 1; i < hdrEnd; i++ {
		ind, k, _, old := yamlKeyLine(lines[i])
		if ind > hdrIndent && k == key {
			lines[i] = strings.Repeat(" ", ind) + key + ": " + value
			if old == value {
				return lines, 0
			}
			return lines, 1
		}
	}
	lines = dshInsertLine(lines, hdrKey+1, strings.Repeat(" ", hdrIndent+2)+key+": "+value)
	return lines, 1
}

func dshInsertLine(lines []string, at int, line string) []string {
	if at < 0 {
		at = 0
	}
	if at > len(lines) {
		at = len(lines)
	}
	lines = append(lines, "")
	copy(lines[at+1:], lines[at:])
	lines[at] = line
	return lines
}

// dshRemoveHeader drops a header entry, and the headers map when emptied.
func dshRemoveHeader(lines []string, provLine, provEnd int, key string) ([]string, bool) {
	hdrKey, _, hdrEnd, ok := dshHeaderSpan(lines, provLine, provEnd)
	if !ok {
		return lines, false
	}
	for i := hdrKey + 1; i < hdrEnd; i++ {
		_, k, _, _ := yamlKeyLine(lines[i])
		if k == key {
			lines = append(lines[:i], lines[i+1:]...)
			// Recheck the map for emptiness after removal.
			hk, _, he, still := dshHeaderSpan(lines, provLine, provEnd)
			if still {
				empty := true
				for j := hk + 1; j < he; j++ {
					if _, kk, _, _ := yamlKeyLine(lines[j]); kk != "" {
						empty = false
						break
					}
				}
				if empty {
					lines = append(lines[:hk], lines[he:]...)
				}
			}
			return lines, true
		}
	}
	return lines, false
}

// dshProviderIDs returns every provider id directly under the
// llm-pi-ai plugin's config.providers mapping, in file order.
func dshProviderIDs(lines []string, start, end int) []string {
	provKey := -1
	provIndent := -1
	for i := start + 1; i < end; i++ {
		ind, key, _, _ := yamlKeyLine(lines[i])
		if key == "providers" {
			provKey, provIndent = i, ind
			break
		}
	}
	if provKey < 0 {
		return nil
	}
	directIndent := -1
	for i := provKey + 1; i < end; i++ {
		if ind, _, _, _ := yamlKeyLine(lines[i]); ind > provIndent {
			directIndent = ind
			break
		}
	}
	if directIndent < 0 {
		return nil
	}
	var ids []string
	for i := provKey + 1; i < end; i++ {
		ind, key, _, _ := yamlKeyLine(lines[i])
		if ind == directIndent && key != "" {
			ids = append(ids, key)
		}
	}
	return ids
}

// DiscoverDshBYOK finds dsh providers with a real upstream that are not
// already on the proxy/gateway. Providers already rewritten are recovered from
// the stash so a re-run stays idempotent.
func DiscoverDshBYOK() []openCodeBYOK {
	path := dshPatchFile()
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return nil
	}
	lines := strings.Split(raw, "\n")
	start, end, ok := dshFindPluginBlock(lines, "llm-pi-ai")
	if !ok {
		return nil
	}
	stashed := loadProxyRouteStash("dsh")
	var out []openCodeBYOK
	for _, id := range dshProviderIDs(lines, start, end) {
		_, provLine, provEnd, ok := dshProviderSpan(lines, start, end, id)
		if !ok {
			continue
		}
		api, _, _ := dshProviderField(lines, provLine, provEnd, "api")
		proto := dshProviderProtocol(api)
		if proto == "" {
			continue
		}
		base, _, _ := dshProviderField(lines, provLine, provEnd, "baseURL")
		if isAbsoluteHTTP(base) && isValidBYOKUpstream(base) &&
			!sameProxyBase(base, strings.TrimRight(ProxyEndpointFor("dsh"), "/")) &&
			!dshGatewayBase(base) {
			// keep the on-disk original
		} else if s, ok := stashed[id]; ok && s.BaseURL != "" {
			base = s.BaseURL
		} else if routeID := dshRouteIDFor(lines, provLine, provEnd); routeID != "" {
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
		keyEnv, _, _ := dshProviderField(lines, provLine, provEnd, "apiKeyEnv")
		out = append(out, openCodeBYOK{ID: id, File: path, BaseURL: base, APIKey: keyEnv, Protocol: proto})
	}
	return out
}

// dshHasGatewayProvider reports whether an eligible provider already points at
// the headroom front or gateway without a verified stash entry.
func dshHasGatewayProvider(raw string) bool {
	lines := strings.Split(raw, "\n")
	start, end, ok := dshFindPluginBlock(lines, "llm-pi-ai")
	if !ok {
		return false
	}
	stashed := loadProxyRouteStash("dsh")
	for _, id := range dshProviderIDs(lines, start, end) {
		_, provLine, provEnd, ok := dshProviderSpan(lines, start, end, id)
		if !ok {
			continue
		}
		api, _, _ := dshProviderField(lines, provLine, provEnd, "api")
		if dshProviderProtocol(api) == "" {
			continue
		}
		base, _, _ := dshProviderField(lines, provLine, provEnd, "baseURL")
		if !dshProxyBase(base) {
			continue
		}
		if _, ok := stashed[id]; ok {
			continue
		}
		return true
	}
	return false
}

// dshRouteIDFor recovers the BYOK route id a gateway-pointing provider
// is bound to. The route id predates any providerId rename, so match on
// the route token carried in the provider's X-Tokless-Route header.
func dshRouteIDFor(lines []string, provLine, provEnd int) string {
	header, ok := dshHeaderValue(lines, provLine, provEnd, byokRouteHeaderKey)
	if !ok || header == "" {
		return ""
	}
	prefix, token, found := strings.Cut(header, ".")
	if !found || token == "" {
		return ""
	}
	if !strings.HasPrefix(prefix, "dsh:") {
		return ""
	}
	route, exists := util.ReadBYOKRoute(prefix)
	if !exists || route.Token != token {
		return ""
	}
	return prefix
}

// dshGatewayRecoverable reports whether every gateway-pointing
// provider maps to a BYOK route that still records the real
// upstream. proxy up can then re-point the provider at the headroom
// front and rebuild the stash from the route's upstream.
func dshGatewayRecoverable(raw string) bool {
	lines := strings.Split(raw, "\n")
	start, end, ok := dshFindPluginBlock(lines, "llm-pi-ai")
	if !ok {
		return false
	}
	stashed := loadProxyRouteStash("dsh")
	for _, id := range dshProviderIDs(lines, start, end) {
		_, provLine, provEnd, ok := dshProviderSpan(lines, start, end, id)
		if !ok {
			continue
		}
		api, _, _ := dshProviderField(lines, provLine, provEnd, "api")
		if dshProviderProtocol(api) == "" {
			continue
		}
		base, _, _ := dshProviderField(lines, provLine, provEnd, "baseURL")
		if !dshProxyBase(base) {
			continue
		}
		if _, ok := stashed[id]; ok {
			continue
		}
		routeID := dshRouteIDFor(lines, provLine, provEnd)
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

// ConfigureDshProxy rewrites dsh providers onto the headroom front,
// registering routes and stashing originals for removal.
func ConfigureDshProxy() (changed bool, file string) {
	file = dshPatchFile()
	if err := withProxyRouteStashLock(func() error {
		changed, file = configureDshProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("dsh proxy lock failed: " + err.Error())
	}
	return changed, file
}

func configureDshProxyLocked() (changed bool, file string) {
	file = dshPatchFile()
	if !proxyRouteStashValid("dsh") {
		return false, file
	}
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false, file
	}
	discovered := DiscoverDshBYOK()
	if len(discovered) == 0 {
		return false, file
	}
	stash := loadProxyRouteStashLocked("dsh")
	stashRaw, stashExists := util.ReadFileSafe(proxyRouteStashPath("dsh"))
	var routeChanges []byokRouteRollback
	rolledBack := false
	rollback := func() {
		if rolledBack {
			return
		}
		rolledBack = true
		for i := len(routeChanges) - 1; i >= 0; i-- {
			rollbackBYOKRouteLogged("dsh", routeChanges[i].id, routeChanges[i].previous)
		}
		restoreProxyRouteStashLogged("dsh", stashRaw, stashExists)
	}

	lines := strings.Split(raw, "\n")
	headroomBase := ProxyEndpointFor("dsh")
	changedAny := false
	for _, provider := range discovered {
		start, end, ok := dshFindPluginBlock(lines, "llm-pi-ai")
		if !ok {
			break
		}
		_, provLine, provEnd, ok := dshProviderSpan(lines, start, end, provider.ID)
		if !ok {
			continue
		}
		origin, chatPath, upstreamOK := dshHeadroomUpstream(provider.BaseURL)
		if !upstreamOK {
			util.L.Warn("dsh: skipping provider with unparseable baseURL: " + provider.ID)
			continue
		}
		upstream := normalizedHeadroomUpstream(provider.BaseURL, provider.Protocol)
		_, routeHeader, previous, routeOK := registerBYOKRoute("dsh", provider.ID, provider.Protocol, upstream)
		if !routeOK {
			rollback()
			return false, file
		}
		routeChanges = append(routeChanges, byokRouteRollback{id: provider.ID, previous: previous})

		base, baseLine, _ := dshProviderField(lines, provLine, provEnd, "baseURL")
		if base != headroomBase {
			dshSetProviderField(lines, baseLine, "baseURL", headroomBase)
			changedAny = true
		}
		var n int
		lines, n = dshUpsertHeader(lines, provLine, provEnd, byokRouteHeaderKey, routeHeader)
		changedAny = changedAny || n > 0
		// Recompute the provider span; header inserts shifted line indices.
		_, provLine, provEnd, _ = dshProviderSpan(lines, start, end, provider.ID)
		lines, n = dshUpsertHeader(lines, provLine, provEnd, dshHeadroomBaseURLHeader, origin)
		changedAny = changedAny || n > 0
		_, provLine, provEnd, _ = dshProviderSpan(lines, start, end, provider.ID)
		lines, n = dshUpsertHeader(lines, provLine, provEnd, dshHeadroomOriginalPathHdr, chatPath)
		changedAny = changedAny || n > 0
		stash[provider.ID] = proxyRouteStashEntry{
			File:      file,
			Provider:  provider.ID,
			BaseURL:   provider.BaseURL,
			Upstream:  upstream,
			HadHeader: false,
		}
	}
	if !changedAny {
		rollback()
		return false, file
	}
	if err := saveProxyRouteStash("dsh", stash); err != nil {
		rollback()
		return false, file
	}
	if err := util.WriteFileAtomic(file, strings.Join(lines, "\n"), 0o600); err != nil {
		rollback()
		return false, file
	}
	return true, file
}

// RemoveDshProxy restores stashed originals and deletes dsh: routes only when
// the config still matches what tokless wrote.
func RemoveDshProxy() bool {
	removed := false
	if err := withProxyRouteStashLock(func() error {
		removed = removeDshProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("dsh proxy lock failed: " + err.Error())
	}
	return removed
}

func removeDshProxyLocked() bool {
	if !proxyRouteStashValid("dsh") {
		return false
	}
	file := dshPatchFile()
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false
	}
	stash := loadProxyRouteStashLocked("dsh")
	if len(stash) == 0 {
		return false
	}
	lines := strings.Split(raw, "\n")
	remaining := map[string]proxyRouteStashEntry{}
	var deletedRoutes []BYOKRoute
	var deletedIDs []string
	for id, entry := range stash {
		start, end, ok := dshFindPluginBlock(lines, "llm-pi-ai")
		if !ok {
			remaining[id] = entry
			continue
		}
		_, provLine, provEnd, ok := dshProviderSpan(lines, start, end, id)
		if !ok {
			remaining[id] = entry
			continue
		}
		route, routeExists := util.ReadBYOKRoute("dsh:" + id)
		base, _, _ := dshProviderField(lines, provLine, provEnd, "baseURL")
		hdr, hdrOK := dshHeaderValue(lines, provLine, provEnd, byokRouteHeaderKey)
		matches := routeExists && hdrOK && dshProxyBase(base) && hdr == util.BYOKRouteHeader(route)
		if !matches {
			remaining[id] = entry
			continue
		}
		dshSetProviderField(lines, mustFindFieldLine(lines, provLine, provEnd, "baseURL"), "baseURL", entry.BaseURL)
		lines, _ = dshRemoveHeader(lines, provLine, provEnd, byokRouteHeaderKey)
		lines, _ = dshRemoveHeader(lines, provLine, provEnd, dshHeadroomBaseURLHeader)
		lines, _ = dshRemoveHeader(lines, provLine, provEnd, dshHeadroomOriginalPathHdr)
		deletedRoutes = append(deletedRoutes, route)
		deletedIDs = append(deletedIDs, id)
	}
	if len(deletedIDs) == 0 {
		return false
	}
	if err := util.WriteFileAtomic(file, strings.Join(lines, "\n"), 0o600); err != nil {
		return false
	}
	for _, id := range deletedIDs {
		if err := deleteBYOKRoute("dsh", id); err != nil {
			_ = util.WriteFile(file, raw)
			return false
		}
	}
	if err := saveProxyRouteStash("dsh", remaining); err != nil {
		_ = util.WriteFile(file, raw)
		for _, route := range deletedRoutes {
			_, _, _ = util.UpsertBYOKRoute(route.ID, route.Protocol, route.Upstream)
		}
		return false
	}
	return true
}

func mustFindFieldLine(lines []string, provLine, provEnd int, key string) int {
	_, line, _ := dshProviderField(lines, provLine, provEnd, key)
	return line
}

// DshProxyWired reports whether every stashed provider still points at the
// headroom front with the correct X-Tokless-Route header and the
// headroom-forward headers.
func DshProxyWired() bool {
	stash := loadProxyRouteStash("dsh")
	if len(stash) == 0 {
		return false
	}
	raw, ok := util.ReadFileSafe(dshPatchFile())
	if !ok {
		return false
	}
	lines := strings.Split(raw, "\n")
	start, end, ok := dshFindPluginBlock(lines, "llm-pi-ai")
	if !ok {
		return false
	}
	for id := range stash {
		_, provLine, provEnd, ok := dshProviderSpan(lines, start, end, id)
		if !ok {
			return false
		}
		base, _, _ := dshProviderField(lines, provLine, provEnd, "baseURL")
		if !dshHeadroomBase(base) {
			return false
		}
		route, routeExists := util.ReadBYOKRoute("dsh:" + id)
		hdr, hdrOK := dshHeaderValue(lines, provLine, provEnd, byokRouteHeaderKey)
		if !routeExists || !hdrOK || hdr != util.BYOKRouteHeader(route) {
			return false
		}
		origin, chatPath, upstreamOK := dshHeadroomUpstream(stash[id].BaseURL)
		if !upstreamOK {
			return false
		}
		if v, ok := dshHeaderValue(lines, provLine, provEnd, dshHeadroomBaseURLHeader); !ok || v != origin {
			return false
		}
		if v, ok := dshHeaderValue(lines, provLine, provEnd, dshHeadroomOriginalPathHdr); !ok || v != chatPath {
			return false
		}
	}
	return true
}

// --- RTK: transparent rewrite cordis plugin ---

// dshRtkPluginName is the cordis patch id of the tokless RTK plugin.
const dshRtkPluginName = "tokless-rtk"

// dshRtkPluginFile is the plugin module the wire installs.
func dshRtkPluginFile() string {
	return filepath.Join(dshHome(), "plugins", "tokless-rtk.mjs")
}

// dshRtkPluginSource renders the plugin that rewrites bash tool
// calls through `tokless rtk-rewrite` before they execute. It hooks
// the tools/pre-execute waterfall, which receives the live execution
// object; mutating exec.arguments.command in place rewrites the
// command transparently — no denial, no model round-trip.
func dshRtkPluginSource(exe string) string {
	return fmt.Sprintf(`// tokless-rtk: transparent RTK rewrite for dsh bash tool calls.
const TOKLESS_BIN = %s

const name = "tokless-rtk"
const inject = []

async function apply(ctx) {
  ctx.on("tools/pre-execute", async (exec, next) => {
    const args = exec?.arguments
    if (exec?.name !== "bash" || typeof args?.command !== "string" || args.command.length === 0) {
      return next()
    }
    try {
      const { spawnSync } = await import("node:child_process")
      const r = spawnSync(TOKLESS_BIN, ["rtk-rewrite", "--", args.command], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] })
      if (r.status === 0 && typeof r.stdout === "string") {
        const rewritten = r.stdout.trim()
        if (rewritten && rewritten !== args.command) {
          exec.arguments = { ...args, command: rewritten }
        }
      }
    } catch {}
    return next()
  })
}

const Config = undefined

export { Config, apply, inject, name }
`, yamlQuote(exe))
}

// dshRtkPluginEntry renders the cordis.patch.yml insert row for the
// RTK plugin. `name` is an absolute path, which the dsh loader
// anchors to a file:// URL beside the patch file.
func dshRtkPluginEntry() string {
	return fmt.Sprintf("\n- insert:\n    - id: %s\n      name: %s\n", dshRtkPluginName, yamlQuote(dshRtkPluginFile()))
}

// DshRtkWired reports whether the managed RTK plugin row is present
// in cordis.patch.yml and the plugin file is installed.
func DshRtkWired() bool {
	raw, ok := util.ReadFileSafe(dshPatchFile())
	if !ok {
		return false
	}
	return strings.Contains(raw, fmt.Sprintf("id: %s", dshRtkPluginName)) && util.Exists(dshRtkPluginFile())
}

// DshBridgeWired reports whether the hooks-claude-code bridge row
// is present in cordis.patch.yml.
func DshBridgeWired() bool {
	raw, ok := util.ReadFileSafe(dshPatchFile())
	if !ok {
		return false
	}
	return strings.Contains(raw, "name: '@deepseek-ai/dsh-hooks-claude-code'")
}

// ConfigureDshRtk installs the RTK rewrite plugin: writes the plugin
// module and inserts its cordis.patch.yml row. A legacy hook-wired
// installation (hooks.json + bridge row) is migrated: the hook is
// removed from hooks.json and the bridge row dropped when it only
// pointed at the managed hooks.json.
func ConfigureDshRtk(exe string) (bool, string, error) {
	if exe == "" {
		return false, "", nil
	}
	patched := dshPatchFile()
	plugin := dshRtkPluginFile()
	raw, ok := util.ReadFileSafe(patched)
	if !ok && util.Exists(patched) {
		return false, patched, nil
	}
	changed := false
	if !strings.Contains(raw, fmt.Sprintf("id: %s", dshRtkPluginName)) {
		if err := util.EnsureDir(filepath.Dir(patched)); err != nil {
			return false, patched, err
		}
		if err := util.WriteFile(patched, raw+dshRtkPluginEntry()); err != nil {
			return false, patched, err
		}
		changed = true
	}
	want := dshRtkPluginSource(exe)
	if cur, pok := util.ReadFileSafe(plugin); !pok || cur != want {
		if err := util.EnsureDir(filepath.Dir(plugin)); err != nil {
			return false, plugin, err
		}
		if err := util.WriteFile(plugin, want); err != nil {
			return false, plugin, err
		}
		changed = true
	}
	// Migrate away from the legacy deny+steer hook wiring.
	if hooks, hok := util.ReadFileSafe(dshHooksFile()); hok && strings.Contains(hooks, `rtk-hook dsh`) {
		if err := os.Remove(dshHooksFile()); err != nil && !os.IsNotExist(err) {
			return changed, dshHooksFile(), err
		}
		changed = true
		RemoveDshBridgeRow()
	}
	return changed, patched, nil
}

// RemoveDshBridgeRow drops the hooks-claude-code insert row from
// cordis.patch.yml when it points at the managed hooks.json.
func RemoveDshBridgeRow() bool {
	patched := dshPatchFile()
	raw, ok := util.ReadFileSafe(patched)
	if !ok {
		return false
	}
	marker := "name: '@deepseek-ai/dsh-hooks-claude-code'"
	idx := strings.Index(raw, marker)
	if idx < 0 {
		return false
	}
	rowStart := strings.LastIndex(raw[:idx], "\n- insert:")
	if rowStart < 0 {
		if strings.HasPrefix(raw, "- insert:") {
			rowStart = 0
		} else {
			return false
		}
	} else {
		rowStart++
	}
	lineEnd := idx + len(marker)
	end := strings.Index(raw[lineEnd:], "\n- ")
	if end < 0 {
		end = len(raw)
	} else {
		end += lineEnd
	}
	block := raw[rowStart:end]
	if !strings.Contains(block, yamlQuote(dshHooksFile())) {
		return false
	}
	result := raw[:rowStart] + raw[end:]
	if end == len(raw) {
		result = strings.TrimRight(result, "\r\n") + "\n"
	}
	return util.WriteFile(patched, result) == nil
}

// RemoveDshRtk drops the RTK plugin row and the plugin module.
func RemoveDshRtk() bool {
	patched := dshPatchFile()
	raw, ok := util.ReadFileSafe(patched)
	rowRemoved := false
	if ok {
		marker := fmt.Sprintf("id: %s", dshRtkPluginName)
		if idx := strings.Index(raw, marker); idx >= 0 {
			rowStart := strings.LastIndex(raw[:idx], "\n- insert:")
			if rowStart < 0 {
				if strings.HasPrefix(raw, "- insert:") {
					rowStart = 0
				} else {
					rowStart = -1
				}
			} else {
				rowStart++
			}
			if rowStart >= 0 {
				lineEnd := idx + len(marker)
				end := strings.Index(raw[lineEnd:], "\n- ")
				if end < 0 {
					end = len(raw)
				} else {
					end += lineEnd
				}
				result := raw[:rowStart] + raw[end:]
				if end == len(raw) {
					result = strings.TrimRight(result, "\r\n") + "\n"
				}
				if util.WriteFile(patched, result) != nil {
					return false
				}
				rowRemoved = true
			}
		}
	}
	pluginExisted := util.Exists(dshRtkPluginFile())
	if err := os.Remove(dshRtkPluginFile()); err != nil && !os.IsNotExist(err) {
		return false
	}
	return rowRemoved || pluginExisted
}

// yamlKeyLine parses a YAML mapping-key line into (indent, key, hasValue,
// value). It is local to dsh because dsh keys include a leading '- id: ' list
// marker, handled separately by dshFindPluginBlock.
func yamlKeyLine(line string) (int, string, bool, string) {
	trimmed := strings.TrimLeft(line, " ")
	indent := len(line) - len(trimmed)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return -1, "", false, ""
	}
	colon := strings.Index(trimmed, ":")
	if colon <= 0 {
		return -1, "", false, ""
	}
	key := trimmed[:colon]
	rest := strings.TrimSpace(trimmed[colon+1:])
	if i := strings.Index(rest, " #"); i >= 0 {
		rest = strings.TrimSpace(rest[:i])
	}
	if rest == "" {
		return indent, key, false, ""
	}
	if len(rest) >= 2 && (rest[0] == '\'' || rest[0] == '"') && rest[len(rest)-1] == rest[0] {
		rest = rest[1 : len(rest)-1]
	}
	return indent, key, true, rest
}
