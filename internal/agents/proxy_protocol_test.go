package agents

import (
	"os"
	"path/filepath"
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

	for _, id := range []string{"codex", "opencode", "kilo", "pi", "droid", "copilot", "cline"} {
		if got := ProxyEndpointFor(id); got != openai {
			t.Errorf("ProxyEndpointFor(%q) = %q, want %q", id, got, openai)
		}
	}
	for _, id := range []string{"claude", "antigravity"} {
		if got := ProxyEndpointFor(id); got != bare {
			t.Errorf("ProxyEndpointFor(%q) = %q, want bare %q", id, got, bare)
		}
	}
	if got := ProxyEndpointFor("omp"); got != openai {
		t.Errorf("ProxyEndpointFor(%q) = %q, want %q", "omp", got, openai)
	}
	for _, id := range []string{"cursor", "unknown", "grok"} {
		if got := ProxyEndpointFor(id); got != "" {
			t.Errorf("ProxyEndpointFor(%q) = %q, want empty", id, got)
		}
	}
}

func TestProxyReadinessForStates(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "")
	t.Cleanup(func() { util.SetHomeOverride(""); _ = util.ClearProxyRoutingPreference() })

	if got := ProxyReadinessFor("cursor"); got != ProxyReadyManual {
		t.Fatalf("cursor readiness = %v, want ProxyReadyManual", got)
	}

	if got := ProxyReadinessFor("unknown"); got != ProxyReadyNotApplicable {
		t.Errorf("unknown readiness = %v, want ProxyReadyNotApplicable", got)
	}
	if got := ProxyReadinessFor("grok"); got != ProxyReadyByokExcluded {
		t.Errorf("grok readiness = %v, want ProxyReadyByokExcluded", got)
	}

	for _, id := range []string{"claude", "codex", "opencode"} {
		if got := ProxyReadinessFor(id); got != ProxyReadyNotApplicable {
			t.Errorf("inapplicable %s readiness = %v, want ProxyReadyNotApplicable", id, got)
		}
	}

	if !ConfigureProxyAgent("claude") {
		t.Fatal("ConfigureProxyAgent(claude) failed")
	}
	if !ProxyAgentApplicable("claude") {
		t.Fatal("managed Claude config should be applicable")
	}
	if err := util.SetProxyRoutingEnabled(false); err != nil {
		t.Fatal(err)
	}
	if got := ProxyReadinessFor("amp"); got != ProxyReadyManual {
		t.Fatalf("amp readiness = %v, want ProxyReadyManual", got)
	}
	if got := ProxyReadinessFor("claude"); got != ProxyReadyRoutingDisabled {
		t.Fatalf("disabled readiness = %v, want ProxyReadyRoutingDisabled", got)
	}

	if err := util.SetProxyRoutingEnabled(true); err != nil {
		t.Fatal(err)
	}
	if got := ProxyReadinessFor("claude"); got != ProxyReadyInstallMissing {
		t.Fatalf("missing install readiness = %v, want ProxyReadyInstallMissing", got)
	}

	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ProxyReadinessFor("claude"); got != ProxyReady {
		t.Fatalf("wired readiness = %v, want ProxyReady", got)
	}
}
