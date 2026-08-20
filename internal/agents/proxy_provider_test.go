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



func TestProviderSpecActiveDefaultsWithoutEnv(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Setenv(proxyProviderEnv, "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	if got := ProviderSpecActive(); got.ID != "headroom" || got.Key != "headroom" {
		t.Fatalf("default active spec = %+v", got)
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

func TestConfigureOpenCodeProxyBYOKOnlyNoHeadroomInject(t *testing.T) {
	opencodeProxyTestHome(t)
	cfgPath := filepath.Join(util.OpenCodePathsResolved().Dir, "opencode.json")
	if err := util.EnsureDir(filepath.Dir(cfgPath)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(cfgPath, `{}`); err != nil {
		t.Fatal(err)
	}
	gatePath := filepath.Join(util.OpenCodePathsResolved().Dir, "config.json")
	gate := `{"enabled_providers":["anthropic"],"provider":{"prov-a":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.provider-a.test/v1","apiKey":"k"}}}}`
	if err := util.WriteFile(gatePath, gate); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("configure did not wire BYOK")
	}
	if changed, _ := ConfigureOpenCodeProxy(); changed {
		t.Fatal("configure was not idempotent")
	}
	raw, _ := util.ReadFileSafe(gatePath)
	if strings.Contains(raw, `"headroom"`) {
		t.Fatalf("must not inject headroom provider: %s", raw)
	}
	if !strings.Contains(raw, `"baseURL":"http://127.0.0.1:8787/v1"`) && !strings.Contains(raw, `"baseURL": "http://127.0.0.1:8787/v1"`) {
		t.Fatalf("BYOK baseURL not wired: %s", raw)
	}
	raw, _ = util.ReadFileSafe(cfgPath)
	if strings.Contains(raw, `"headroom"`) {
		t.Fatalf("must not touch opencode.json: %s", raw)
	}
}

func TestConfigureOpenCodeProxyNoBYOKNoOp(t *testing.T) {
	opencodeProxyTestHome(t)
	cfgPath := filepath.Join(util.OpenCodePathsResolved().Dir, "opencode.json")
	if err := util.EnsureDir(filepath.Dir(cfgPath)); err != nil {
		t.Fatal(err)
	}
	orig := `{"theme":"dark"}`
	if err := util.WriteFile(cfgPath, orig); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); changed {
		t.Fatal("no BYOK must be no-op")
	}
	raw, _ := util.ReadFileSafe(cfgPath)
	if raw != orig {
		t.Fatalf("config mutated without BYOK: %s", raw)
	}
}

func TestConfigureOpenCodeProxyMalformedGateNoOp(t *testing.T) {
	opencodeProxyTestHome(t)
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
	if changed, _ := ConfigureOpenCodeProxy(); changed {
		t.Fatal("malformed gate must not change")
	}
	raw, _ := util.ReadFileSafe(gatePath)
	if raw != malformed {
		t.Fatalf("malformed gate changed: %q", raw)
	}
}



