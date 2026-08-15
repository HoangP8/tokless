package agents

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestDefaultProviderSpecByteIdentity(t *testing.T) {
	want := `"npm": "@ai-sdk/openai-compatible"
  "name": "Headroom Proxy"
  "options": {
    "baseURL": "http://127.0.0.1:8787/v1"`
	got := util.StringifyJSON(openCodeProxyProviderBlock("http://127.0.0.1:8787/v1"))
	for _, frag := range strings.Split(want, "\n") {
		if !strings.Contains(got, frag) {
			t.Fatalf("default block missing %q:\n%s", frag, got)
		}
	}
	if strings.Contains(got, "reasoning") || strings.Contains(got, "apiKey") {
		t.Fatalf("default block must not render reasoning/apiKey:\n%s", got)
	}
	if util.StringifyJSON(openCodeProxyProviderBlock("http://127.0.0.1:8787/v1")) != util.StringifyJSON(openCodeProxyProviderBlockFor("http://127.0.0.1:8787/v1", DefaultProviderSpec())) {
		t.Fatal("openCodeProxyProviderBlock diverged from spec-parameterized default")
	}
}

func TestApiboxProviderBlockHoldsEnvKeyNotSecret(t *testing.T) {
	t.Setenv(proxyProviderEnv, "apibox")
	spec := ProviderSpecActive()
	if spec.ID != "apibox" || spec.Key != "apibox" || spec.Npm != "@ai-sdk/anthropic" || spec.Name != "APIBox" {
		t.Fatalf("apibox spec = %+v", spec)
	}
	if spec.KeyEnv != "TOKLESS_APIOBOX_KEY" {
		t.Fatalf("apibox KeyEnv = %q", spec.KeyEnv)
	}
	if len(spec.Models) != 2 || !spec.Models[0].Reasoning || spec.Models[0].Context != 200000 || spec.Models[0].Output != 64000 {
		t.Fatalf("apibox models = %+v", spec.Models)
	}
	block := util.StringifyJSON(openCodeProxyProviderBlockFor("http://127.0.0.1:8787/v1", spec))
	for _, absent := range []string{"sk-", "api.ai-box.vn"} {
		if strings.Contains(block, absent) {
			t.Fatalf("API key leaked into provider block: %s", block)
		}
	}
	if !strings.Contains(block, `"apiKey": "{env:TOKLESS_APIOBOX_KEY}"`) {
		t.Fatalf("apibox block missing env key ref: %s", block)
	}
	if !strings.Contains(block, `"deepseek-v4-flash"`) || !strings.Contains(block, `"qwen3.8-max"`) {
		t.Fatalf("apibox block missing models: %s", block)
	}
	if !strings.Contains(block, `"reasoning": true`) {
		t.Fatalf("apibox block missing reasoning flag: %s", block)
	}
}

func TestProviderSpecActiveDefaultsWithoutEnv(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Setenv(proxyProviderEnv, "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	if got := ProviderSpecActive(); got.ID != "headroom" || got.Key != "headroom" {
		t.Fatalf("default active spec = %+v", got)
	}
}

func TestProviderSpecActiveFallsBackToPersistedProvider(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Setenv(proxyProviderEnv, "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveHeadroomProxyRuntime(util.ProxyRuntime{Port: 19787, Provider: "apibox"}); err != nil {
		t.Fatal(err)
	}
	spec := ProviderSpecActive()
	if spec.ID != "apibox" || spec.Key != "apibox" {
		t.Fatalf("persisted-provider fallback spec = %+v", spec)
	}
}

func TestOpenCodeApiboxProxyRoundTrip(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv(proxyProviderEnv, "apibox")

	cfgPath := util.OpenCodePathsResolved().Config
	if err := util.EnsureDir(filepath.Dir(cfgPath)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(cfgPath, openCodeProxySeed(false)); err != nil {
		t.Fatal(err)
	}

	if ch, _ := ConfigureOpenCodeProxy(); !ch {
		t.Fatal("apibox configure returned no change")
	}
	if !OpenCodeProxyWired() {
		t.Fatal("apibox not wired after configure")
	}
	raw, _ := util.ReadFileSafe(cfgPath)
	if !strings.Contains(raw, `"apibox"`) || strings.Contains(raw, `"headroom"`) {
		t.Fatalf("apibox provider key wrong: %s", raw)
	}
	if !strings.Contains(raw, `"npm": "@ai-sdk/anthropic"`) || !strings.Contains(raw, `"apiKey": "{env:TOKLESS_APIOBOX_KEY}"`) {
		t.Fatalf("apibox block wrong: %s", raw)
	}
	if !RemoveOpenCodeProxy() {
		t.Fatal("apibox remove returned false")
	}
	if OpenCodeProxyWired() {
		t.Fatal("apibox still wired after remove")
	}
}