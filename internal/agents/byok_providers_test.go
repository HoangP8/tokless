package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestBYOKProvidersForStashOnly(t *testing.T) {
	setupBYOKProviderHome(t)
	config := filepath.Join(util.Home(), "fixture.json")
	if err := os.WriteFile(config, []byte(`{"alpha":{},"beta":{},"gamma":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	proxyAgentSpecs["fixture"] = ProxyAgentSpec{ID: "fixture", ConfigFiles: func() []string { return []string{config} }}
	defer delete(proxyAgentSpecs, "fixture")
	writeProviderStash(t, "fixture", map[string]proxyRouteStashEntry{
		"alpha": {Provider: "alpha", BaseURL: "https://alpha.example"},
		"beta":  {Provider: "beta", BaseURL: "https://beta.example"},
		"gamma": {Provider: "gamma", BaseURL: "https://gamma.example"},
	})
	got := BYOKProvidersFor("fixture")
	if len(got) != 3 {
		t.Fatalf("providers = %+v", got)
	}
	for _, p := range got {
		if p.Stale || p.File != config {
			t.Errorf("provider = %+v", p)
		}
	}
}

func TestBYOKProvidersForMarksAbsentRegistryKeyStale(t *testing.T) {
	setupBYOKProviderHome(t)
	config := filepath.Join(util.Home(), "fixture.json")
	if err := os.WriteFile(config, []byte(`{"present":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	proxyAgentSpecs["fixture"] = ProxyAgentSpec{ID: "fixture", ConfigFiles: func() []string { return []string{config} }}
	defer delete(proxyAgentSpecs, "fixture")
	writeProviderRoutes(t, []util.BYOKRoute{
		{ID: "fixture:absent", Upstream: "https://absent.example"},
		{ID: "fixture:present", Upstream: "https://present.example"},
	})
	got := BYOKProvidersFor("fixture")
	if len(got) != 2 || !got[0].Stale || got[1].Stale || got[1].Provider != "present" {
		t.Fatalf("providers = %+v", got)
	}
}

func TestBYOKProvidersForManagedRouteWithoutStash(t *testing.T) {
	setupBYOKProviderHome(t)
	config := filepath.Join(util.Home(), "fixture.json")
	if err := os.WriteFile(config, []byte(`{"gateway": {"baseUrl":"http://127.0.0.1:18787/v1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	proxyAgentSpecs["fixture"] = ProxyAgentSpec{ID: "fixture", ConfigFiles: func() []string { return []string{config} }}
	defer delete(proxyAgentSpecs, "fixture")
	writeProviderRoutes(t, []util.BYOKRoute{{ID: "fixture:gateway", Upstream: "https://upstream.example"}})
	got := BYOKProvidersFor("fixture")
	want := []BYOKProvider{{Provider: "gateway", Upstream: "https://upstream.example", File: config}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("providers = %+v, want %+v", got, want)
	}
}

func TestBYOKProvidersForMissingConfigMarksAllStale(t *testing.T) {
	setupBYOKProviderHome(t)
	config := filepath.Join(util.Home(), "missing.json")
	proxyAgentSpecs["fixture"] = ProxyAgentSpec{ID: "fixture", ConfigFiles: func() []string { return []string{config} }}
	defer delete(proxyAgentSpecs, "fixture")
	writeProviderStash(t, "fixture", map[string]proxyRouteStashEntry{"alpha": {Provider: "alpha"}})
	got := BYOKProvidersFor("fixture")
	if len(got) != 1 || !got[0].Stale || got[0].File != "" {
		t.Fatalf("missing config must mark candidate stale: %+v", got)
	}
}

func TestBYOKProvidersForUnknownAgent(t *testing.T) {
	setupBYOKProviderHome(t)
	if got := BYOKProvidersFor("nonexistent"); len(got) != 0 {
		t.Fatalf("providers = %+v", got)
	}
}

// TestBYOKProvidersForUpstreamHostIsNotAppliedProvider guards the real-world
// false positive: a provider key that only occurs inside its own upstream
// hostname (e.g. "qwencoder" in "https://api.qwencoder.cloud") must stay stale.
func TestBYOKProvidersForUpstreamHostIsNotAppliedProvider(t *testing.T) {
	setupBYOKProviderHome(t)
	config := filepath.Join(util.Home(), "fixture.yml")
	body := "providers:\n  gateway:\n    displayName: DeepSeek Gateway\n    headers:\n      x-headroom-base-url: https://api.qwencoder.cloud/api\n"
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	proxyAgentSpecs["fixture"] = ProxyAgentSpec{ID: "fixture", ConfigFiles: func() []string { return []string{config} }}
	defer delete(proxyAgentSpecs, "fixture")
	writeProviderRoutes(t, []util.BYOKRoute{
		{ID: "fixture:qwencoder", Upstream: "https://api.qwencoder.cloud/api"},
		{ID: "fixture:gateway", Upstream: "https://api.qwencoder.cloud/api"},
	})
	got := BYOKProvidersFor("fixture")
	if len(got) != 2 || got[0].Provider != "gateway" || got[0].Stale || got[1].Provider != "qwencoder" || !got[1].Stale {
		t.Fatalf("url-only key must be stale, real key applied: %+v", got)
	}
}

// TestBYOKProvidersForKeyOutsideURLStillMatches is the positive control: URL
// stripping must not drop a key that is also present as a genuine config token.
func TestBYOKProvidersForKeyOutsideURLStillMatches(t *testing.T) {
	setupBYOKProviderHome(t)
	config := filepath.Join(util.Home(), "fixture.toml")
	body := "[model_providers.qwencoder]\nname = \"qwencoder\"\nbase_url = \"https://api.qwencoder.cloud/api\"\n"
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	proxyAgentSpecs["fixture"] = ProxyAgentSpec{ID: "fixture", ConfigFiles: func() []string { return []string{config} }}
	defer delete(proxyAgentSpecs, "fixture")
	writeProviderRoutes(t, []util.BYOKRoute{{ID: "fixture:qwencoder", Upstream: "https://api.qwencoder.cloud/api"}})
	got := BYOKProvidersFor("fixture")
	if len(got) != 1 || got[0].Stale || got[0].File != config {
		t.Fatalf("genuine config token must not be dropped by URL stripping: %+v", got)
	}
}

func setupBYOKProviderHome(t *testing.T) {
	t.Helper()
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func writeProviderStash(t *testing.T, id string, providers map[string]proxyRouteStashEntry) {
	t.Helper()
	if err := os.MkdirAll(util.HeadroomPathsResolved().Root, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(proxyRouteStashFile{Providers: providers})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proxyRouteStashPath(id), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeProviderRoutes(t *testing.T, routes []util.BYOKRoute) {
	t.Helper()
	if err := os.MkdirAll(util.HeadroomPathsResolved().Root, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(struct {
		Routes []util.BYOKRoute `json:"routes"`
	}{Routes: routes})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(util.HeadroomPathsResolved().Root, "byok.routes.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
