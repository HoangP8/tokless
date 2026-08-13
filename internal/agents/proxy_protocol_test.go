package agents

import "testing"

// TestProxyEndpointForRouting pins the single wire-protocol rule: OpenAI-
// compatible clients point at the daemon's /v1 surface, Anthropic-native
// clients at the bare URL (their SDK appends /v1/messages itself), and anything
// tokless does not wire through a config file has no endpoint.
func TestProxyEndpointForRouting(t *testing.T) {
	openai := proxyTestURL + "/v1"
	bare := proxyTestURL

	for _, id := range []string{"codex", "opencode", "kilo", "pi", "droid"} {
		if got := ProxyEndpointFor(id); got != openai {
			t.Errorf("ProxyEndpointFor(%q) = %q, want %q", id, got, openai)
		}
	}
	for _, id := range []string{"claude", "omp"} {
		if got := ProxyEndpointFor(id); got != bare {
			t.Errorf("ProxyEndpointFor(%q) = %q, want bare %q", id, got, bare)
		}
	}
	for _, id := range []string{"grok", "copilot", "cline", "cursor", "antigravity", "unknown"} {
		if got := ProxyEndpointFor(id); got != "" {
			t.Errorf("ProxyEndpointFor(%q) = %q, want empty", id, got)
		}
	}
}
