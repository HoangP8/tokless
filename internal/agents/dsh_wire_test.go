package agents

import (
	"os"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
	"gopkg.in/yaml.v3"
)

// dshWireSeed is the original pre-wiring dsh provider block: one provider with
// a real upstream, a models list, and no headers map.
const dshWireSeed = `# tokless wiring: BYOK provider.
- id: llm-pi-ai
  name: '@deepseek-ai/dsh-llm-pi-ai'
  config:
    providers:
      gateway:
        displayName: DeepSeek Gateway
        apiKeyEnv: TOKLESS_QWENCODER_API_KEY
        api: openai-completions
        baseURL: https://api.qwencoder.cloud/api/v1
        models:
          - id: deepseek-v4.1-flash
            contextWindow: 200000
- id: agent-default-model
  name: '@deepseek-ai/dsh-agent-default-model'
  config:
    provider: gateway
    model: deepseek-v4.1-flash
`

// dshWireV1Seed keeps the original baseURL ending in /v1 so the legacy-route
// recovery path rebuilds it byte-for-byte.
const dshWireV1Seed = `- id: llm-pi-ai
  name: '@deepseek-ai/dsh-llm-pi-ai'
  config:
    providers:
      gateway:
        displayName: DeepSeek Gateway
        apiKeyEnv: TOKLESS_QWENCODER_API_KEY
        api: openai-completions
        baseURL: https://api.qwencoder.cloud/v1
        models:
          - id: deepseek-v4.1-flash
            contextWindow: 200000
`

type dshWireDoc struct {
	ID     string `yaml:"id"`
	Name   string `yaml:"name"`
	Config struct {
		Providers map[string]dshWireProvider `yaml:"providers"`
	} `yaml:"config"`
}

type dshWireProvider struct {
	DisplayName string            `yaml:"displayName"`
	APIKeyEnv   string            `yaml:"apiKeyEnv"`
	API         string            `yaml:"api"`
	BaseURL     string            `yaml:"baseURL"`
	Models      []dshWireModel    `yaml:"models"`
	Headers     map[string]string `yaml:"headers"`
}

type dshWireModel struct {
	ID            string `yaml:"id"`
	ContextWindow int    `yaml:"contextWindow"`
}

func dshWireParse(t *testing.T, raw string) dshWireProvider {
	t.Helper()
	var docs []dshWireDoc
	if err := yaml.Unmarshal([]byte(raw), &docs); err != nil {
		t.Fatalf("wired config is not valid YAML: %v\n%s", err, raw)
	}
	for _, d := range docs {
		if d.ID != "llm-pi-ai" {
			continue
		}
		p, ok := d.Config.Providers["gateway"]
		if !ok {
			t.Fatalf("gateway provider missing after wiring:\n%s", raw)
		}
		return p
	}
	t.Fatalf("llm-pi-ai plugin block missing after wiring:\n%s", raw)
	return dshWireProvider{}
}

// TestDshWireProducesValidSingleHeadersMap covers A1: the writer must emit one
// parseable headers map holding all three override headers, and keep models a
// non-empty list.
func TestDshWireProducesValidSingleHeadersMap(t *testing.T) {
	path := dshProxyTestHome(t)
	if err := util.WriteFile(path, dshWireSeed); err != nil {
		t.Fatal(err)
	}
	changed, file := ConfigureDshProxy()
	if !changed || file != path {
		t.Fatalf("ConfigureDshProxy = %v, %q; want true, %q", changed, file, path)
	}
	raw, _ := util.ReadFileSafe(path)

	prov := dshWireParse(t, raw)

	if n := strings.Count(raw, "\n        headers:"); n != 1 {
		t.Fatalf("want exactly one provider headers map, got %d:\n%s", n, raw)
	}
	want := map[string]string{
		dshHeadroomBaseURLHeader:   "https://api.qwencoder.cloud",
		dshHeadroomOriginalPathHdr: "/api/v1/chat/completions",
	}
	for k, v := range want {
		if got := prov.Headers[k]; got != v {
			t.Fatalf("header %q = %q, want %q\n%s", k, got, v, raw)
		}
	}
	if len(prov.Headers) != 3 {
		t.Fatalf("want 3 headers, got %d: %v\n%s", len(prov.Headers), prov.Headers, raw)
	}
	if route := prov.Headers[byokRouteHeaderKey]; !strings.HasPrefix(route, "dsh:gateway.") {
		t.Fatalf("X-Tokless-Route = %q, want dsh:gateway.<token>\n%s", route, raw)
	}

	if len(prov.Models) == 0 {
		t.Fatalf("models is null/empty after wiring:\n%s", raw)
	}
	if prov.Models[0].ID != "deepseek-v4.1-flash" || prov.Models[0].ContextWindow != 200000 {
		t.Fatalf("models = %+v, want the original deepseek-v4.1-flash entry\n%s", prov.Models, raw)
	}
	if prov.BaseURL != ProxyEndpointFor("dsh") {
		t.Fatalf("baseURL = %q, want %q", prov.BaseURL, ProxyEndpointFor("dsh"))
	}
}

// TestDshWireDoesNotDestroyInput covers the regression guard: the original
// provider is rewritten once, not destructively.
func TestDshWireDoesNotDestroyInput(t *testing.T) {
	path := dshProxyTestHome(t)
	if err := util.WriteFile(path, dshWireSeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureDshProxy(); !changed {
		t.Fatal("ConfigureDshProxy = false")
	}
	raw, _ := util.ReadFileSafe(path)
	if strings.Contains(raw, "https://api.qwencoder.cloud/api/v1") {
		t.Fatalf("original baseURL survived the rewrite:\n%s", raw)
	}
	if n := strings.Count(raw, "baseURL:"); n != 1 {
		t.Fatalf("want exactly one baseURL key, got %d:\n%s", n, raw)
	}
	if !strings.Contains(raw, "- id: agent-default-model") {
		t.Fatalf("sibling plugin block damaged:\n%s", raw)
	}
	if err := util.WriteFile(path, raw); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureDshProxy(); changed {
		t.Fatalf("second wiring modified a correct file:\n%s", raw)
	}
}

// TestDshWireIdempotentByteIdentical covers required test 4: a second run is a
// byte-identical no-op that keeps the route registered and reports success.
func TestDshWireIdempotentByteIdentical(t *testing.T) {
	path := dshProxyTestHome(t)
	if err := util.WriteFile(path, dshWireSeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureDshProxy(); !changed {
		t.Fatal("first ConfigureDshProxy = false")
	}
	first, _ := util.ReadFileSafe(path)

	changed, _ := ConfigureDshProxy()
	if changed {
		t.Fatal("second ConfigureDshProxy = true, want a no-op")
	}
	second, _ := util.ReadFileSafe(path)
	if first != second {
		t.Fatalf("second run is not byte-identical:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !DshProxyWired() {
		t.Fatal("DshProxyWired = false after re-run")
	}
	if !ConfigureProxyAgent("dsh") {
		t.Fatal("ConfigureProxyAgent(dsh) = false after re-run (reported as failure)")
	}
	if _, ok := util.ReadBYOKRoute("dsh:gateway"); !ok {
		t.Fatal("re-run un-registered the dsh:gateway route")
	}
}

// TestDshAlreadyWiredReportsSuccess covers A2: when the file is already correct
// the writer must report success and keep the route/stash registered instead of
// rolling back.
func TestDshAlreadyWiredReportsSuccess(t *testing.T) {
	path := dshProxyTestHome(t)
	if err := util.WriteFile(path, dshWireV1Seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureDshProxy(); !changed {
		t.Fatal("first ConfigureDshProxy = false")
	}
	before, _ := util.ReadFileSafe(path)

	if err := os.Remove(proxyRouteStashPath("dsh")); err != nil {
		t.Fatal(err)
	}

	changed, _ := ConfigureDshProxy()
	if changed {
		t.Fatal("re-run of an already-correct file reported a change")
	}
	if !DshProxyWired() {
		t.Fatal("DshProxyWired = false on an already-correct re-run")
	}
	if !ConfigureProxyAgent("dsh") {
		t.Fatal("ConfigureProxyAgent(dsh) = false on an already-correct config")
	}
	if _, ok := util.ReadBYOKRoute("dsh:gateway"); !ok {
		t.Fatal("re-run un-registered the dsh:gateway route")
	}
	if _, ok := loadProxyRouteStash("dsh")["gateway"]; !ok {
		t.Fatal("re-run did not persist the provider stash")
	}
	after, _ := util.ReadFileSafe(path)
	if after != before {
		t.Fatalf("re-run rewrote an already-correct file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
