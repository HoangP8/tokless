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

func TestOpencodeGoProviderBlockHoldsEnvKeyNotSecret(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Setenv(proxyProviderEnv, "opencode-go")
	t.Cleanup(func() { util.SetHomeOverride("") })
	spec := ProviderSpecActive()
	if spec.ID != "opencode-go" || spec.Key != "opencode-go" || spec.Npm != "@ai-sdk/anthropic" || spec.Name != "OpenCode Go" {
		t.Fatalf("opencode-go spec = %+v", spec)
	}
	if spec.KeyEnv != "TOKLESS_OPENCODE_GO_KEY" {
		t.Fatalf("opencode-go KeyEnv = %q", spec.KeyEnv)
	}
	if len(spec.Models) != 4 || spec.Models[0].ID != "qwen3.8-max" || !spec.Models[0].Reasoning {
		t.Fatalf("opencode-go models = %+v", spec.Models)
	}
	block := util.StringifyJSON(openCodeProxyProviderBlockFor("http://127.0.0.1:19787/v1", spec))
	for _, absent := range []string{"sk-", "opencode.ai/zen"} {
		if strings.Contains(block, absent) {
			t.Fatalf("secret/upstream leaked into provider block: %s", block)
		}
	}
	if !strings.Contains(block, `"apiKey": "{env:TOKLESS_OPENCODE_GO_KEY}"`) {
		t.Fatalf("opencode-go block missing env key ref: %s", block)
	}
	if !strings.Contains(block, `"qwen3.8-max"`) || !strings.Contains(block, `"qwen3.7-plus"`) || !strings.Contains(block, `"minimax-m3"`) {
		t.Fatalf("opencode-go block missing models: %s", block)
	}
}

func TestProxyWireModelAndKeyEnvOverride(t *testing.T) {
	t.Setenv("TOKLESS_PROXY_MODEL", "deepseek-v4-flash")
	t.Setenv("TOKLESS_PROXY_KEY", "test-wire-key")
	pi := piProxyProviderEntry("http://127.0.0.1:19787/v1")
	if key, _ := pi.Get("apiKey"); key != "test-wire-key" {
		t.Fatalf("pi apiKey = %v", key)
	}
	pm, _ := pi.Get("models")
	pmodels, ok := pm.([]any)
	if !ok || len(pmodels) != 1 {
		t.Fatalf("pi models = %v", pm)
	}
	first, _ := pmodels[0].(*util.OrderedMap)
	if id, _ := first.Get("id"); id != "deepseek-v4-flash" {
		t.Fatalf("pi model id = %v", id)
	}
	kilo := kiloProxyProviderEntry("http://127.0.0.1:19787/v1")
	ko, _ := kilo.Get("options")
	options, _ := ko.(*util.OrderedMap)
	if key, _ := options.Get("apiKey"); key != "test-wire-key" {
		t.Fatalf("kilo apiKey = %v", key)
	}
	km, _ := kilo.Get("models")
	models, _ := km.(*util.OrderedMap)
	if _, ok := models.Get("deepseek-v4-flash"); !ok {
		t.Fatalf("kilo models = %s", util.StringifyJSON(models))
	}
	droid := droidProxyEntry("http://127.0.0.1:19787/v1")
	if model, _ := droid.Get("model"); model != "deepseek-v4-flash" {
		t.Fatalf("droid model = %v", model)
	}
	if key, _ := droid.Get("apiKey"); key != "test-wire-key" {
		t.Fatalf("droid apiKey = %v", key)
	}
	// Defaults preserve the historical placeholders when env is unset.
	t.Setenv("TOKLESS_PROXY_MODEL", "")
	t.Setenv("TOKLESS_PROXY_KEY", "")
	if key, _ := piProxyProviderEntry("x").Get("apiKey"); key != "tokless" {
		t.Fatalf("default apiKey = %v", key)
	}
	if _, ok := droidProxyEntry("x").Get("apiKey"); ok {
		t.Fatal("default droid entry must not include apiKey")
	}
}

func TestConfigureOpenCodeProxyEnabledProviders(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv(proxyProviderEnv, "opencode-go")
	cfgPath := filepath.Join(util.OpenCodePathsResolved().Dir, "opencode.json")
	if err := util.EnsureDir(filepath.Dir(cfgPath)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(cfgPath, `{}`); err != nil {
		t.Fatal(err)
	}
	gatePath := filepath.Join(util.OpenCodePathsResolved().Dir, "config.json")
	if err := util.WriteFile(gatePath, `{"enabled_providers":["anthropic"]}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("configure did not append provider")
	}
	if changed, _ := ConfigureOpenCodeProxy(); changed {
		t.Fatal("configure was not idempotent")
	}
	raw, _ := util.ReadFileSafe(gatePath)
	if strings.Count(raw, `"opencode-go"`) != 1 {
		t.Fatalf("gate provider id count = %d, want 1: %s", strings.Count(raw, `"opencode-go"`), raw)
	}
	raw, _ = util.ReadFileSafe(cfgPath)
	if strings.Count(raw, `"opencode-go"`) != 1 {
		t.Fatalf("registry provider id count = %d, want 1: %s", strings.Count(raw, `"opencode-go"`), raw)
	}
}

func TestConfigureOpenCodeProxyEnabledProvidersAbsentNoOp(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv(proxyProviderEnv, "opencode-go")
	cfgPath := filepath.Join(util.OpenCodePathsResolved().Dir, "opencode.json")
	if err := util.EnsureDir(filepath.Dir(cfgPath)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(cfgPath, `{"theme":"dark"}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("provider configure did not change config")
	}
	raw, _ := util.ReadFileSafe(cfgPath)
	if strings.Contains(raw, "enabled_providers") {
		t.Fatalf("absent enabled_providers was created: %s", raw)
	}
}

func TestConfigureOpenCodeProxyMalformedSeparateGateNoOp(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv(proxyProviderEnv, "opencode-go")
	cfgPath := filepath.Join(util.OpenCodePathsResolved().Dir, "opencode.json")
	if err := util.EnsureDir(filepath.Dir(cfgPath)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(cfgPath, `{}`); err != nil {
		t.Fatal(err)
	}
	gatePath := filepath.Join(util.OpenCodePathsResolved().Dir, "config.json")
	malformed := `{"enabled_providers":[`
	if err := util.WriteFile(gatePath, malformed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("provider configure did not change registry")
	}
	raw, _ := util.ReadFileSafe(gatePath)
	if raw != malformed {
		t.Fatalf("malformed gate changed: %q", raw)
	}
}

func TestProviderSpecActiveOpencodeGoPersistedFallback(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Setenv(proxyProviderEnv, "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SaveHeadroomProxyRuntime(util.ProxyRuntime{Port: 19787, Provider: "opencode-go"}); err != nil {
		t.Fatal(err)
	}
	spec := ProviderSpecActive()
	if spec.ID != "opencode-go" || spec.KeyEnv != "TOKLESS_OPENCODE_GO_KEY" {
		t.Fatalf("persisted-provider opencode-go fallback spec = %+v", spec)
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
