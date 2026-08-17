package agents

import (
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// TestProxyEndpointForRouting pins the single wire-protocol rule: OpenAI-
// compatible clients point at the daemon's /v1 surface, Anthropic-/Gemini-native
// clients at the bare URL (their SDK appends /v1/messages or /v1beta themselves),
// and anything tokless does not wire has no endpoint.
func TestProxyEndpointForRouting(t *testing.T) {
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "")
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	openai := proxyTestURL + "/v1"
	bare := proxyTestURL

	for _, id := range []string{"opencode", "kilo", "pi", "droid", "grok", "copilot", "cline"} {
		if got := ProxyEndpointFor(id); got != openai {
			t.Errorf("ProxyEndpointFor(%q) = %q, want %q", id, got, openai)
		}
	}
	for _, id := range []string{"claude", "omp", "antigravity"} {
		if got := ProxyEndpointFor(id); got != bare {
			t.Errorf("ProxyEndpointFor(%q) = %q, want bare %q", id, got, bare)
		}
	}
	for _, id := range []string{"codex", "cursor", "unknown"} {
		if got := ProxyEndpointFor(id); got != "" {
			t.Errorf("ProxyEndpointFor(%q) = %q, want empty", id, got)
		}
	}
}
