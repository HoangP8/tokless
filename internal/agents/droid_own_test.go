package agents

import (
	"fmt"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// TestDroidProxyUpgradesOwnDriftedEntry reproduces B1: Droid re-serializes the
// entry Tokless wrote and adds its own id/index/noImageSupport fields.
func TestDroidProxyUpgradesOwnDriftedEntry(t *testing.T) {
	droidProxyTestHome(t)
	endpoint := ProxyEndpointFor("droid")
	drifted := droidProxyEntry(endpoint)
	drifted.Set("id", "custom:Headroom-Proxy-0")
	drifted.Set("index", 1)
	drifted.Set("noImageSupport", true)
	cfg := util.NewOrderedMap()
	cfg.Set("customModels", []any{drifted})
	if err := util.WriteFile(droidSettingsFile(), util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	changed, file := ConfigureDroidProxy()
	if !changed || file != droidSettingsFile() {
		t.Fatalf("stale owned entry not upgraded: changed=%v file=%q", changed, file)
	}
	if !DroidProxyWired() {
		t.Fatal("upgraded entry must report wired")
	}
	raw, _ := util.ReadFileSafe(droidSettingsFile())
	if strings.Contains(raw, "noImageSupport") || strings.Contains(raw, `"index"`) {
		t.Fatalf("upgrade must rewrite the canonical shape:\n%s", raw)
	}
	if !strings.Contains(raw, `"baseUrl": "`+endpoint+`"`) {
		t.Fatalf("upgraded entry lost its endpoint:\n%s", raw)
	}
}

// TestDroidProxyRefusesUnmarkedLookalike is the safety half of B1: a
// user-authored entry that shares endpoint and identity but carries no
// ownership marker must never be clobbered.
func TestDroidProxyRefusesUnmarkedLookalike(t *testing.T) {
	droidProxyTestHome(t)
	seed := `{"customModels":[{"model":"headroom","displayName":"Headroom Proxy","baseUrl":"` + ProxyEndpointFor("droid") + `","provider":"generic-chat-completion-api","extraHeaders":{"x-user":"keep"}}]}`
	if err := util.WriteFile(droidSettingsFile(), seed); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureDroidProxy()
	if changed {
		t.Fatal("unmarked lookalike must not be rewritten")
	}
	if DroidProxyWired() {
		t.Fatal("unmarked lookalike must not count as wired")
	}
	raw, _ := util.ReadFileSafe(droidSettingsFile())
	if raw != seed {
		t.Fatalf("unmarked lookalike changed:\n got: %q\nwant: %q", raw, seed)
	}
	if RemoveDroidProxy() {
		t.Fatal("unmarked lookalike must not be removed")
	}
	if raw, _ = util.ReadFileSafe(droidSettingsFile()); raw != seed {
		t.Fatalf("unmarked lookalike changed by remove:\n%s", raw)
	}
}

func TestDroidProxyLegacyUnmarkedEntryPolicy(t *testing.T) {
	cases := []struct {
		name string
		seed string
	}{
		{
			name: "exact legacy shape",
			seed: `{"customModels":[{"model":"headroom","displayName":"Headroom Proxy","baseUrl":"` + ProxyEndpointFor("droid") + `","provider":"generic-chat-completion-api"}]}`,
		},
		{
			name: "legacy shape with droid normalization fields",
			seed: `{"customModels":[{"model":"headroom","displayName":"Headroom Proxy","id":"custom:Headroom-Proxy-0","index":1,"baseUrl":"` + ProxyEndpointFor("droid") + `","noImageSupport":true,"provider":"generic-chat-completion-api"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			droidProxyTestHome(t)
			if err := util.WriteFile(droidSettingsFile(), tc.seed); err != nil {
				t.Fatal(err)
			}
			changed, _ := ConfigureDroidProxy()
			if changed {
				t.Fatal("legacy unmarked entry must stay byte-identical")
			}
			raw, _ := util.ReadFileSafe(droidSettingsFile())
			if raw != tc.seed {
				t.Fatalf("legacy entry rewritten:\n got: %q\nwant: %q", raw, tc.seed)
			}
			if !DroidProxyWired() {
				t.Fatal("legacy unmarked entry must still report wired")
			}
			if !RemoveDroidProxy() {
				t.Fatal("legacy unmarked entry must still be removable")
			}
			if DroidProxyWired() {
				t.Fatal("still wired after remove")
			}
		})
	}
}

// TestDroidProxyReregistersLostRouteFromStash reproduces B2: the entry is
// already rewired to the gateway and the stash survives, but the route
// registry lost the route.
func TestDroidProxyReregistersLostRouteFromStash(t *testing.T) {
	droidProxyTestHome(t)
	t.Cleanup(func() { _ = saveProxyRouteStash("droid", nil) })
	const id = "deepseek-v4.1-flash"
	const upstream = "https://api.provider-a.test/api"
	route, _, err := util.UpsertBYOKRoute("droid:"+id, "openai-chat", upstream)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveProxyRouteStash("droid", map[string]proxyRouteStashEntry{
		id: {File: droidSettingsFile(), Provider: id, BaseURL: upstream + "/v1", Upstream: upstream},
	}); err != nil {
		t.Fatal(err)
	}
	if err := util.DeleteBYOKRoute("droid:" + id); err != nil {
		t.Fatal(err)
	}
	seed := fmt.Sprintf(`{"customModels":[{"model":%q,"id":"custom:QwenCoder-0","index":0,"baseUrl":"%s/v1","apiKey":"user-key","displayName":"QwenCoder","noImageSupport":true,"provider":"generic-chat-completion-api","extraHeaders":{"x-headroom-base-url":%q,"X-Tokless-Route":%q}}]}`,
		id, util.BYOKGatewayEndpoint(), upstream, util.BYOKRouteHeader(route))
	if err := util.WriteFile(droidSettingsFile(), seed); err != nil {
		t.Fatal(err)
	}

	if changed, _ := ConfigureDroidProxy(); !changed || !DroidProxyWired() {
		t.Fatalf("configure: changed=%v wired=%v", changed, DroidProxyWired())
	}
	got, ok := util.ReadBYOKRoute("droid:" + id)
	if !ok || got.Upstream != upstream {
		t.Fatalf("route must be re-registered from the stash: %+v ok=%v", got, ok)
	}
	if stash := loadProxyRouteStash("droid"); stash[id].Upstream != upstream || stash[id].BaseURL != upstream+"/v1" {
		t.Fatalf("stash must keep the provider origin: %+v", stash[id])
	}

	if changed, _ := ConfigureDroidProxy(); changed {
		t.Fatal("second run must be idempotent")
	}
	if !DroidProxyWired() {
		t.Fatal("wired must stay true on the second run")
	}
	again, ok := util.ReadBYOKRoute("droid:" + id)
	if !ok || again.Token != got.Token {
		t.Fatalf("second run must not un-register or rotate the route: %+v ok=%v", again, ok)
	}
}

// TestDroidProxyRemoveRestoresStashBothFormats covers removal for both the
// marked and the legacy unmarked owned entry: RemoveDroidProxy must drop the
// owned entry and restore the stashed native provider.
func TestDroidProxyRemoveRestoresStashBothFormats(t *testing.T) {
	native := `{"model":"provider-model","displayName":"Provider Model","baseUrl":"https://provider.example/v1","apiKey":"user-key","provider":"generic-chat-completion-api"}`
	cases := []struct {
		name          string
		seedOwned     bool
		ownedUnmarked bool
	}{
		{name: "marked format"},
		{name: "legacy unmarked format", seedOwned: true, ownedUnmarked: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			droidProxyTestHome(t)
			native := util.TryParseJsonc(`{"customModels":[` + native + `]}`)
			nativeModels, _ := native.Get("customModels")
			models := append([]any{}, nativeModels.([]any)...)
			if tc.seedOwned {
				legacy := droidProxyEntry(ProxyEndpointFor("droid"))
				legacy.Delete(droidProxyMarkerKey)
				if tc.ownedUnmarked {
					models = append(models, legacy)
				}
			}
			want := util.StringifyJSON(native)
			seed := util.NewOrderedMap()
			seed.Set("customModels", models)
			if err := util.WriteFile(droidSettingsFile(), util.StringifyJSON(seed)); err != nil {
				t.Fatal(err)
			}

			if changed, _ := ConfigureDroidProxy(); !changed || !DroidProxyWired() {
				t.Fatalf("configure: changed=%v wired=%v", changed, DroidProxyWired())
			}
			configuredRaw, _ := util.ReadFileSafe(droidSettingsFile())
			if strings.Contains(configuredRaw, `"Headroom Proxy"`) {
				t.Fatalf("configure must not append bare headroom entry when native model exists:\n%s", configuredRaw)
			}
			if !RemoveDroidProxy() {
				t.Fatal("remove must drop the owned entry and restore the stash")
			}
			if DroidProxyWired() {
				t.Fatal("still wired after remove")
			}
			got, _ := util.ReadFileSafe(droidSettingsFile())
			if got != want {
				t.Fatalf("native provider not restored:\n got: %s\nwant: %s", got, want)
			}
			if _, ok := util.ReadBYOKRoute("droid:provider-model"); ok {
				t.Fatal("route must be deleted on removal")
			}
		})
	}
}
