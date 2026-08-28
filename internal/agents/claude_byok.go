package agents

import (
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// Claude BYOK: when ~/.claude/settings.json already points ANTHROPIC_BASE_URL
// at a user-run gateway (LiteLLM, qwencoder, ...), tokless routes that traffic
// through headroom.
const (
	claudeProxyHeaderEnvKey = "ANTHROPIC_CUSTOM_HEADERS"
	claudeAPIKeyEnvKey      = "ANTHROPIC_API_KEY"
	claudeAuthTokenEnvKey   = "ANTHROPIC_AUTH_TOKEN"

	claudeProxyHeaderName   = "x-headroom-base-url"
	claudeProxyHeaderLine   = claudeProxyHeaderName + ": "
	claudeByokStashAgent    = "claude"
	claudeByokStashProvider = "claude"
)

// claudeBYOKUpstream returns the user's current ANTHROPIC_BASE_URL when it is
// a real endpoint other than the managed proxy ("", otherwise).
func claudeBYOKUpstream(env *util.OrderedMap) string {
	v, ok := env.Get(claudeProxyEnvKey)
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok || s == "" || s == ProxyEndpointFor("claude") {
		return ""
	}
	return s
}

// claudeCustomHeadersGet reads env.ANTHROPIC_CUSTOM_HEADERS as a list of lines.
func claudeCustomHeadersLines(env *util.OrderedMap) ([]string, bool) {
	v, ok := env.Get(claudeProxyHeaderEnvKey)
	if !ok {
		return nil, false
	}
	s, ok := v.(string)
	if !ok {
		return nil, false
	}
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	return lines, true
}

func claudeCustomHeadersSet(env *util.OrderedMap, lines []string) {
	if len(lines) == 0 {
		env.Delete(claudeProxyHeaderEnvKey)
		return
	}
	env.Set(claudeProxyHeaderEnvKey, strings.Join(lines, "\n"))
}

// claudeCustomHeadersDrop removes any x-headroom-base-url line regardless of
// spacing/case ("Name: value" parsed at the first colon).
func claudeCustomHeadersDrop(lines []string) ([]string, bool) {
	kept := make([]string, 0, len(lines))
	dropped := false
	for _, ln := range lines {
		name, _, found := strings.Cut(ln, ":")
		if !found {
			if strings.EqualFold(strings.TrimSpace(strings.TrimRight(ln, " \t\r")), claudeProxyHeaderName) {
				dropped = true
				continue
			}
		} else if strings.EqualFold(strings.TrimSpace(strings.TrimRight(name, " \t\r")), claudeProxyHeaderName) {
			dropped = true
			continue
		}
		kept = append(kept, ln)
	}
	return kept, dropped
}

func loadClaudeBYOKStash() (proxyRouteStashEntry, bool) {
	all := loadProxyRouteStash(claudeByokStashAgent)
	return claudeBYOKStashFrom(all)
}

func loadClaudeBYOKStashLocked() (proxyRouteStashEntry, bool) {
	return claudeBYOKStashFrom(loadProxyRouteStashLocked(claudeByokStashAgent))
}

func claudeBYOKStashFrom(all map[string]proxyRouteStashEntry) (proxyRouteStashEntry, bool) {
	e, ok := all[claudeByokStashProvider]
	if !ok || e.BaseURL == "" {
		return proxyRouteStashEntry{}, false
	}
	if e.File == "" {
		return proxyRouteStashEntry{}, false
	}
	return e, true
}

func saveClaudeBYOKStash(entry proxyRouteStashEntry) error {
	return stashTxn(claudeByokStashAgent, func(all map[string]proxyRouteStashEntry) error {
		if entry.BaseURL == "" {
			delete(all, claudeByokStashProvider)
		} else {
			all[claudeByokStashProvider] = entry
		}
		return saveProxyRouteStash(claudeByokStashAgent, all)
	})
}

func saveClaudeBYOKStashLocked(entry proxyRouteStashEntry, all map[string]proxyRouteStashEntry) error {
	if entry.BaseURL == "" {
		delete(all, claudeByokStashProvider)
	} else {
		all[claudeByokStashProvider] = entry
	}
	return saveProxyRouteStash(claudeByokStashAgent, all)
}

// claudeTakeoverBYOK routes an existing foreign endpoint through the proxy:
// stash originals, point BASE_URL at headroom, add the hop header.
func claudeTakeoverBYOK(cfg *util.OrderedMap, env *util.OrderedMap, upstream string) bool {
	original, ok := util.ReadFileSafe(util.ClaudeCodePaths().Settings)
	if !ok {
		return false
	}
	lines, hadHeaders := claudeCustomHeadersLines(env)
	prevHeaders := ""
	if hadHeaders {
		prevHeaders = strings.Join(lines, "\n")
	}
	entry := proxyRouteStashEntry{
		File:      util.ClaudeCodePaths().Settings,
		Provider:  claudeByokStashProvider,
		BaseURL:   upstream,
		HadHeader: hadHeaders,
		Header:    prevHeaders,
		Original:  []byte(original),
	}
	if v, ok := env.Get(claudeAPIKeyEnvKey); ok {
		if s, isStr := v.(string); isStr {
			entry.HadBaseKey = true
			entry.BaseKey = s
		}
	}
	if v, ok := env.Get(claudeAuthTokenEnvKey); ok {
		if s, isStr := v.(string); isStr {
			entry.HadAuth = true
			entry.BaseLine = s
		}
	}
	stash := loadProxyRouteStashLocked(claudeByokStashAgent)
	previousStash := cloneProxyRouteStash(stash)
	if entry.BaseKey != "" {
		entry.MovedBase = true
		env.Delete(claudeAPIKeyEnvKey)
		env.Set(claudeAuthTokenEnvKey, entry.BaseKey)
	}
	env.Set(claudeProxyEnvKey, ProxyEndpointFor("claude"))
	kept, _ := claudeCustomHeadersDrop(lines)
	kept = append(kept, claudeProxyHeaderLine+stripV1Suffix(upstream))
	claudeCustomHeadersSet(env, kept)
	managed := util.StringifyJSON(cfg)
	entry.Managed = []byte(managed)
	if err := saveClaudeBYOKStashLocked(entry, stash); err != nil {
		return false
	}
	if err := util.WriteFileAtomic(util.ClaudeCodePaths().Settings, managed, 0o644); err != nil {
		_ = saveProxyRouteStash(claudeByokStashAgent, previousStash)
		return false
	}
	return true
}

// claudeRestoreBYOK unwires managed routing: restore whatever the takeover
// mutated, leaving keys the takeover never touched exactly as they were.
func claudeRestoreBYOK(_ *util.OrderedMap, _ *util.OrderedMap) bool {
	stash := loadProxyRouteStashLocked(claudeByokStashAgent)
	entry, ok := claudeBYOKStashFrom(stash)
	if !ok {
		return false
	}
	current, ok := util.ReadFileSafe(util.ClaudeCodePaths().Settings)
	if !ok {
		return false
	}
	if len(entry.Original) > 0 && len(entry.Managed) > 0 && current == string(entry.Managed) {
		if err := util.WriteFileAtomic(util.ClaudeCodePaths().Settings, string(entry.Original), 0o644); err != nil {
			return false
		}
		return saveClaudeBYOKStashLocked(proxyRouteStashEntry{}, stash) == nil
	}
	if entry.ManagedNative {
		cfg := util.TryParseJsonc(current)
		if cfg == nil {
			return false
		}
		env, ok := mapChild(cfg, "env")
		if !ok {
			return false
		}
		if v, ok := env.Get(claudeProxyEnvKey); !ok || v != ProxyEndpointFor("claude") {
			return false
		}
		env.Delete(claudeProxyEnvKey)
		if env.Len() == 0 {
			cfg.Delete("env")
		}
		if err := util.WriteFileAtomic(util.ClaudeCodePaths().Settings, util.StringifyJSON(cfg), 0o644); err != nil {
			return false
		}
		return saveClaudeBYOKStashLocked(proxyRouteStashEntry{}, stash) == nil
	}
	cfg := util.TryParseJsonc(current)
	if cfg == nil {
		return false
	}
	env, ok := mapChild(cfg, "env")
	if !ok {
		return false
	}
	if v, ok := env.Get(claudeProxyEnvKey); !ok || v != ProxyEndpointFor("claude") {
		return false
	}
	lines, _ := claudeCustomHeadersLines(env)
	kept := make([]string, 0, len(lines))
	managedHeader := claudeProxyHeaderLine + stripV1Suffix(entry.BaseURL)
	for _, line := range lines {
		if line == managedHeader {
			continue
		}
		kept = append(kept, line)
	}
	claudeCustomHeadersSet(env, kept)
	env.Set(claudeProxyEnvKey, entry.BaseURL)
	if entry.MovedBase {
		currentAuth, authOK := env.Get(claudeAuthTokenEnvKey)
		currentKey, keyOK := env.Get(claudeAPIKeyEnvKey)
		managedAuth, authString := currentAuth.(string)
		_, keyString := currentKey.(string)
		if authOK && authString && managedAuth == entry.BaseKey && (!keyOK || !keyString) {
			if entry.HadAuth {
				env.Set(claudeAuthTokenEnvKey, entry.BaseLine)
			} else {
				env.Delete(claudeAuthTokenEnvKey)
			}
			if entry.HadBaseKey {
				env.Set(claudeAPIKeyEnvKey, entry.BaseKey)
			}
		}
	}
	if err := util.WriteFileAtomic(util.ClaudeCodePaths().Settings, util.StringifyJSON(cfg), 0o644); err != nil {
		return false
	}
	if err := saveClaudeBYOKStashLocked(proxyRouteStashEntry{}, stash); err != nil {
		return false
	}
	return true
}

func cloneProxyRouteStash(src map[string]proxyRouteStashEntry) map[string]proxyRouteStashEntry {
	dst := make(map[string]proxyRouteStashEntry, len(src))
	for key, entry := range src {
		dst[key] = entry
	}
	return dst
}
