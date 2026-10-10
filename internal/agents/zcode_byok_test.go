package agents

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// zcodeProxyTestHome pins the zcode config dir under a temp home and
// returns the provider_config.json path.
func zcodeProxyTestHome(t *testing.T) string {
	t.Helper()
	pinToklessProxyEnv(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	return zcodeProviderConfigPath()
}

const zcodeProxySeed = `{
  "schemaVersion": 1,
  "config": {
    "providerConfigRules": {
      "providerRules": [
        {
          "providerId": "acme",
          "providerName": "Acme AI",
          "config": {
            "api": {
              "type": "openai-chat-completions",
              "baseUrl": "https://api.acme.example/v1",
              "headers": { "X-Custom": "keep" }
            },
            "access": { "type": "api-key", "apiKey": "$ACME_API_KEY" },
            "models": { "m1": { "name": "M1" } }
          }
        },
        {
          "providerId": "resp",
          "providerName": "Resp AI",
          "config": {
            "api": { "type": "openai-responses", "baseURL": "https://api.resp.example/v1" },
            "access": { "type": "api-key", "apiKey": false }
          }
        }
      ]
    }
  }
}`

func TestZcodeProxyRoundTrip(t *testing.T) {
	path := zcodeProxyTestHome(t)
	if err := util.WriteFile(path, zcodeProxySeed); err != nil {
		t.Fatal(err)
	}

	changed, file := ConfigureZcodeProxy()
	if !changed || file != path {
		t.Fatalf("ConfigureZcodeProxy = %v, %q; want true, %q", changed, file, path)
	}
	if !ZcodeProxyWired() {
		t.Fatal("ZcodeProxyWired = false after configure")
	}
	if got := DetectProxy("zcode").State; got != ProxyStateManaged {
		t.Fatalf("DetectProxy(zcode) = %v, want managed", got)
	}
	raw, _ := util.ReadFileSafe(path)
	for _, want := range []string{ProxyEndpointFor("zcode"), byokRouteHeaderKey, `"X-Custom"`, zcodeHeadroomBaseURLHeader, zcodeHeadroomOriginalPathHdr, `"https://api.acme.example"`, `"/v1/chat/completions"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("configured provider_config.json missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "https://api.acme.example/v1") || strings.Contains(raw, "https://api.resp.example/v1") {
		t.Fatalf("provider base URL not rewritten:\n%s", raw)
	}

	route, ok := util.ReadBYOKRoute("zcode:acme")
	if !ok || route.Protocol != "openai-chat" || route.Upstream != "https://api.acme.example" {
		t.Fatalf("zcode:acme route = %+v ok=%v", route, ok)
	}
	routeResp, ok := util.ReadBYOKRoute("zcode:resp")
	if !ok || routeResp.Protocol != "openai-responses" || routeResp.Upstream != "https://api.resp.example" {
		t.Fatalf("zcode:resp route = %+v ok=%v", routeResp, ok)
	}

	if changed, _ := ConfigureZcodeProxy(); changed {
		t.Fatal("second configure should be a no-op")
	}
	if !ZcodeProxyWired() {
		t.Fatal("ZcodeProxyWired became false on second configure")
	}

	if !RemoveZcodeProxy() {
		t.Fatal("RemoveZcodeProxy = false")
	}
	if ZcodeProxyWired() {
		t.Fatal("ZcodeProxyWired = true after remove")
	}
	restored, _ := util.ReadFileSafe(path)
	for _, bad := range []string{util.BYOKGatewayEndpoint(), byokRouteHeaderKey, ProxyEndpointFor("zcode"), zcodeHeadroomBaseURLHeader, zcodeHeadroomOriginalPathHdr} {
		if strings.Contains(restored, bad) {
			t.Fatalf("residue %q after remove:\n%s", bad, restored)
		}
	}
	if !strings.Contains(restored, "https://api.acme.example/v1") || !strings.Contains(restored, "https://api.resp.example/v1") {
		t.Fatalf("provider base URL not restored:\n%s", restored)
	}
	if !strings.Contains(restored, `"baseUrl"`) {
		t.Fatalf("original baseUrl key spelling not preserved:\n%s", restored)
	}
}

// TestZcodeBaseURLKeySpellingPreserved guards the two accepted spellings:
// the seeded row uses baseUrl, tokless must not leave a baseURL twin.
func TestZcodeBaseURLKeySpellingPreserved(t *testing.T) {
	path := zcodeProxyTestHome(t)
	if err := util.WriteFile(path, zcodeProxySeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureZcodeProxy(); !changed {
		t.Fatal("ConfigureZcodeProxy = false")
	}
	raw, _ := util.ReadFileSafe(path)
	// acme seeded baseUrl, resp seeded baseURL; each row keeps its own
	// spelling and gains no twin key.
	if strings.Count(raw, `"baseUrl"`) != 1 || strings.Count(raw, `"baseURL"`) != 1 {
		t.Fatalf("baseUrl/baseURL keys not normalized correctly:\n%s", raw)
	}
}

func TestZcodeUnparseableBaseURLSkipped(t *testing.T) {
	path := zcodeProxyTestHome(t)
	if err := util.WriteFile(path, `{"config":{"providerConfigRules":{"providerRules":[{"providerId":"bad","config":{"api":{"type":"openai-chat-completions","baseUrl":"not-a-url"},"access":{"apiKey":"k"}}}]}}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureZcodeProxy(); changed {
		t.Fatal("ConfigureZcodeProxy changed a config with no valid upstream")
	}
}

func TestZcodeProviderConfigPath(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	want := filepath.Join(home, ".zcode", "v2", "provider_config.json")
	if got := zcodeProviderConfigPath(); got != want {
		t.Fatalf("zcodeProviderConfigPath = %q, want %q", got, want)
	}
}
