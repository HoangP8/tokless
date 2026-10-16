package agents

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// kimiProxyTestHome pins KIMI_CODE_HOME under a temp home and returns config.toml.
func kimiProxyTestHome(t *testing.T) string {
	t.Setenv("TOKLESS_TEST", "1")
	t.Helper()
	pinToklessProxyEnv(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, ".kimi-code"))
	t.Setenv("HOME", home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	return util.KimiPathsResolved().Config
}

const kimiProxySeed = `[providers."acme"]
type = "openai"
api_key = "acme-key"
base_url = "https://api.acme.example/v1"

[providers."other"]
type = "anthropic"
api_key = "other-key"
base_url = "https://api.anthropic.example"
`

func TestKimiProxyRoundTrip(t *testing.T) {
	cfgPath := kimiProxyTestHome(t)
	if err := util.WriteFile(cfgPath, kimiProxySeed); err != nil {
		t.Fatal(err)
	}

	changed, file := ConfigureKimiProxy()
	if !changed || file != cfgPath {
		t.Fatalf("ConfigureKimiProxy = %v, %q; want true, %q", changed, file, cfgPath)
	}
	if !KimiProxyWired() {
		t.Fatal("KimiProxyWired = false after configure")
	}
	if got := DetectProxy("kimi").State; got != ProxyStateManaged {
		t.Fatalf("DetectProxy(kimi) = %v, want managed", got)
	}
	raw, _ := util.ReadFileSafe(cfgPath)
	for _, want := range []string{`[providers."acme"]`, ProxyEndpointFor("kimi"), kimiRouteHeader, `api_key = "acme-key"`, kimiHeadroomBaseURLHeader, kimiHeadroomOriginalPathHdr, `"https://api.acme.example"`, `"/v1/chat/completions"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("configured config missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "https://api.acme.example/v1") {
		t.Fatalf("acme base URL not rewritten:\n%s", raw)
	}
	if !strings.Contains(raw, "https://api.anthropic.example") {
		t.Fatalf("non-OpenAI provider must stay untouched:\n%s", raw)
	}
	route, ok := util.ReadBYOKRoute("kimi:acme")
	if !ok || route.Protocol != "openai-chat" || route.Upstream != "https://api.acme.example" {
		t.Fatalf("kimi:acme route = %+v ok=%v", route, ok)
	}
	if _, ok := util.ReadBYOKRoute("kimi:other"); ok {
		t.Fatal("anthropic provider must not be routed")
	}

	if changed, _ := ConfigureKimiProxy(); changed {
		t.Fatal("second configure should be a no-op")
	}
	if !KimiProxyWired() {
		t.Fatal("KimiProxyWired became false on second configure")
	}

	if !RemoveKimiProxy() {
		t.Fatal("RemoveKimiProxy = false")
	}
	if KimiProxyWired() {
		t.Fatal("KimiProxyWired = true after remove")
	}
	restored, _ := util.ReadFileSafe(cfgPath)
	if strings.Contains(restored, util.BYOKGatewayEndpoint()) || strings.Contains(restored, kimiRouteHeader) ||
		strings.Contains(restored, ProxyEndpointFor("kimi")) || strings.Contains(restored, kimiHeadroomBaseURLHeader) ||
		strings.Contains(restored, kimiHeadroomOriginalPathHdr) {
		t.Fatalf("residue after remove:\n%s", restored)
	}
	if !strings.Contains(restored, "https://api.acme.example/v1") {
		t.Fatalf("acme base URL not restored:\n%s", restored)
	}
	if _, ok := util.ReadBYOKRoute("kimi:acme"); ok {
		t.Fatal("kimi:acme route left behind")
	}
}

func TestKimiProxyMergesInlineCustomHeaders(t *testing.T) {
	cfgPath := kimiProxyTestHome(t)
	seed := `[providers."acme"]
type = "openai"
api_key = "k"
base_url = "https://api.acme.example/v1"
custom_headers = { "X-Custom" = "keep" }
`
	if err := util.WriteFile(cfgPath, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureKimiProxy(); !changed {
		t.Fatal("configure changed = false")
	}
	raw, _ := util.ReadFileSafe(cfgPath)
	if !strings.Contains(raw, `"X-Custom"`) || !strings.Contains(raw, kimiRouteHeader) {
		t.Fatalf("inline custom_headers not merged:\n%s", raw)
	}
	if !RemoveKimiProxy() {
		t.Fatal("remove failed")
	}
	restored, _ := util.ReadFileSafe(cfgPath)
	if !strings.Contains(restored, `"X-Custom"`) {
		t.Fatalf("user header lost on remove:\n%s", restored)
	}
	if strings.Contains(restored, kimiRouteHeader) {
		t.Fatalf("managed header left behind:\n%s", restored)
	}
}

func TestKimiProxyRestoresPreexistingRouteHeader(t *testing.T) {
	cfgPath := kimiProxyTestHome(t)
	seed := `[providers."acme"]
type = "openai"
api_key = "k"
base_url = "https://api.acme.example/v1"
custom_headers = { "X-Tokless-Route" = "user-owned" }
`
	if err := util.WriteFile(cfgPath, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureKimiProxy(); !changed {
		t.Fatal("configure changed = false")
	}
	if !RemoveKimiProxy() {
		t.Fatal("remove failed")
	}
	restored, _ := util.ReadFileSafe(cfgPath)
	if !strings.Contains(restored, `"X-Tokless-Route" = "user-owned"`) {
		t.Fatalf("pre-existing header not restored:\n%s", restored)
	}
}

func TestKimiDetectForeignGatewayConflict(t *testing.T) {
	cfgPath := kimiProxyTestHome(t)
	seed := `[providers."acme"]
type = "openai"
api_key = "k"
base_url = "` + util.BYOKGatewayEndpoint() + `/v1"
custom_headers = { "X-Tokless-Route" = "kimi:acme.deadbeef" }
`
	if err := util.WriteFile(cfgPath, seed); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("kimi").State; got != ProxyStateConflict {
		t.Fatalf("DetectProxy(kimi) = %v, want conflict", got)
	}
	if changed, _ := ConfigureKimiProxy(); changed {
		t.Fatal("configure must not adopt a gateway provider without a stash")
	}
	if KimiProxyWired() {
		t.Fatal("must not report wired without a stash")
	}
}

func TestKimiProxyRefusesMalformedStash(t *testing.T) {
	cfgPath := kimiProxyTestHome(t)
	if err := util.WriteFile(cfgPath, kimiProxySeed); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(proxyRouteStashPath("kimi"), `{"providers":"junk"}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureKimiProxy(); changed {
		t.Fatal("configure must refuse a malformed stash")
	}
	raw, _ := util.ReadFileSafe(cfgPath)
	if raw != kimiProxySeed {
		t.Fatalf("config mutated despite malformed stash:\n%s", raw)
	}
}

func TestKimiDiscoveryResolvesAPIKeyEnv(t *testing.T) {
	cfgPath := kimiProxyTestHome(t)
	seed := `[providers."acme"]
type = "openai"
api_key_env = "KIMI_TEST_KEY"
base_url = "https://api.acme.example/v1"
`
	if err := util.WriteFile(cfgPath, seed); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIMI_TEST_KEY", "env-key")
	got := DiscoverKimiBYOK()
	if len(got) != 1 || got[0].ID != "acme" || got[0].APIKey != "env-key" {
		t.Fatalf("DiscoverKimiBYOK = %+v", got)
	}
	t.Setenv("KIMI_TEST_KEY", "")
	if got := DiscoverKimiBYOK(); len(got) != 0 {
		t.Fatalf("provider without a resolvable key must be skipped: %+v", got)
	}
}

func TestKimiProxyCapability(t *testing.T) {
	cap, ok := ProxyCapabilities()["kimi"]
	if !ok || cap.WireKind != ProxyWireAdditiveProvider || cap.Protocol != ProxyProtocolOpenAICompatible {
		t.Fatalf("kimi capability = %+v ok=%v", cap, ok)
	}
	if ProxyEndpointFor("kimi") == "" {
		t.Fatal("kimi endpoint should be OpenAI-compatible")
	}
}

func TestKimiProxyMergesSubCustomHeaders(t *testing.T) {
	cfgPath := kimiProxyTestHome(t)
	seed := `[providers.'acme']
type = "openai"
api_key = "k"
base_url = "https://api.acme.example/v1"

[providers.'acme'.custom_headers]
X-Custom = "keep"
`
	if err := util.WriteFile(cfgPath, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureKimiProxy(); !changed {
		t.Fatal("configure changed = false")
	}
	raw, _ := util.ReadFileSafe(cfgPath)
	if !strings.Contains(raw, `X-Custom = "keep"`) || !strings.Contains(raw, kimiRouteHeader+" = ") {
		t.Fatalf("sub-table custom_headers not merged:\n%s", raw)
	}
	if !RemoveKimiProxy() {
		t.Fatal("remove failed")
	}
	restored, _ := util.ReadFileSafe(cfgPath)
	if !strings.Contains(restored, `X-Custom = "keep"`) {
		t.Fatalf("user sub-header lost on remove:\n%s", restored)
	}
	if strings.Contains(restored, kimiRouteHeader) {
		t.Fatalf("managed header left in sub-table:\n%s", restored)
	}
	if !strings.Contains(restored, "https://api.acme.example/v1") {
		t.Fatalf("base URL not restored:\n%s", restored)
	}
}

func TestKimiResponseProviderUsesResponsesProtocol(t *testing.T) {
	cfgPath := kimiProxyTestHome(t)
	seed := `[providers."resp"]
type = "openai_responses"
api_key = "k"
base_url = "https://api.resp.example/v1"
`
	if err := util.WriteFile(cfgPath, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureKimiProxy(); !changed {
		t.Fatal("configure changed = false")
	}
	route, ok := util.ReadBYOKRoute("kimi:resp")
	if !ok || route.Protocol != "openai-responses" {
		t.Fatalf("kimi:resp route = %+v ok=%v", route, ok)
	}
	if !RemoveKimiProxy() {
		t.Fatal("remove failed")
	}
}

func TestDetectionOnlyHarnesses(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("ZCODE_DIST_HOME", "")
	t.Setenv("COMMAND_CODE_DIR", "")
	t.Setenv("DSH_HOME", "")
	t.Cleanup(func() { util.SetHomeOverride("") })

	// zcode and dsh are wired through the headroom front like cmd.
	if spec, ok := proxySpecFor("zcode"); !ok || spec.Configure == nil || spec.Remove == nil || spec.Wired == nil {
		t.Fatalf("zcode spec must expose wire functions: %+v ok=%v", spec, ok)
	}
	if cap := ProxyCapabilities()["zcode"]; cap.WireKind != ProxyWireAdditiveProvider {
		t.Fatalf("zcode wire kind = %v, want additive-provider", cap.WireKind)
	}
	if !ProxyAgentUsesHeadroom("zcode") {
		t.Fatal("zcode must route through shared Headroom")
	}

	if spec, ok := proxySpecFor("dsh"); !ok || spec.Configure == nil || spec.Remove == nil || spec.Wired == nil {
		t.Fatalf("dsh spec must expose wire functions: %+v ok=%v", spec, ok)
	}
	if cap := ProxyCapabilities()["dsh"]; cap.WireKind != ProxyWireAdditiveProvider {
		t.Fatalf("dsh wire kind = %v, want additive-provider", cap.WireKind)
	}
	if !ProxyAgentUsesHeadroom("dsh") {
		t.Fatal("dsh must route through shared Headroom")
	}
}
