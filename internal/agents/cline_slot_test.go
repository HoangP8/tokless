package agents

import (
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

const clineUserProviderSeed = `{"providers":{"openai-compatible":{"settings":{"provider":"openai-compatible","apiKey":"sk-ZxWFEqTwUSER","model":"gpt-4o"},"updatedAt":"2026-10-17T09:36:31.592Z","tokenSource":"migration"}}}`

func clineProvidersOf(t *testing.T, raw string) *util.OrderedMap {
	t.Helper()
	cfg, err := util.ParseJsonc(raw)
	if err != nil {
		t.Fatalf("parse providers: %v", err)
	}
	value, ok := cfg.Get("providers")
	if !ok {
		t.Fatalf("no providers object in %s", raw)
	}
	providers, ok := value.(*util.OrderedMap)
	if !ok {
		t.Fatalf("providers is not an object in %s", raw)
	}
	return providers
}

func clineEntryBytes(providers *util.OrderedMap, name string) string {
	value, ok := providers.Get(name)
	if !ok {
		return ""
	}
	return util.StringifyJSON(value)
}

// A user-owned entry under the old reserved name must survive a wire byte-for-
// byte, while tokless takes a namespaced slot for its own provider.
func TestClineWiresNamespacedSlotBesideUserProvider(t *testing.T) {
	clineProxyTestHome(t)
	file := clineProvidersFile()
	if err := util.WriteFile(file, clineUserProviderSeed); err != nil {
		t.Fatal(err)
	}
	userBefore := clineEntryBytes(clineProvidersOf(t, clineUserProviderSeed), clineLegacyProviderName)

	if changed, _ := ConfigureClineProxy(); !changed {
		t.Fatal("configure did not wire the namespaced slot")
	}
	if !ClineProxyWired() {
		t.Fatal("cline proxy not wired")
	}
	raw, _ := util.ReadFileSafe(file)
	providers := clineProvidersOf(t, raw)
	if got := clineEntryBytes(providers, clineLegacyProviderName); got != userBefore {
		t.Fatalf("user-owned provider modified:\nwant %s\ngot  %s", userBefore, got)
	}
	managed, ok := providers.Get(clineProviderName)
	if !ok || !clineManagedValues(managed) {
		t.Fatalf("managed provider missing from %s slot:\n%s", clineProviderName, raw)
	}
	cfg, _ := util.ParseJsonc(raw)
	if selected, _ := cfg.Get("lastUsedProvider"); selected != clineProviderName {
		t.Fatalf("lastUsedProvider = %v, want %s", selected, clineProviderName)
	}
}

// A user-owned entry in the legacy slot is foreign data, not our slot, so it can
// never block `tokless proxy up` for every other wired agent.
func TestClineDetectReportsUserProviderAsUnconfiguredNotConflict(t *testing.T) {
	clineProxyTestHome(t)
	if err := util.WriteFile(clineProvidersFile(), clineUserProviderSeed); err != nil {
		t.Fatal(err)
	}
	got := DetectProxy("cline")
	if got.State == ProxyStateConflict || got.State == ProxyStateForeignBYOK {
		t.Fatalf("user-owned provider reported blocking state %s (%s)", got.State, got.Detail)
	}
	if got.State != ProxyStateUnconfigured {
		t.Fatalf("state = %s (%s), want %s", got.State, got.Detail, ProxyStateUnconfigured)
	}
	if changed, _ := ConfigureClineProxy(); !changed {
		t.Fatal("configure refused a user-owned legacy provider")
	}
	if wired := DetectProxy("cline"); wired.State != ProxyStateManaged {
		t.Fatalf("after configure state = %s (%s), want %s", wired.State, wired.Detail, ProxyStateManaged)
	}
}

// A provider entry tokless wrote under the legacy name is still ours: it is
// detected as wired, migrated into the namespaced slot, and unwired cleanly.
func TestClineLegacyManagedProviderIsMigratedAndRemoved(t *testing.T) {
	clineProxyTestHome(t)
	file := clineProvidersFile()
	seed := `{"providers":{"openai-compatible":{"settings":{"provider":"openai-compatible","apiKey":"` + proxyWireKey() + `","model":"deepseek-v4-flash","baseUrl":"` + ProxyEndpointFor("cline") + `"}}}}`
	if err := util.WriteFile(file, seed); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(clineProviderStateFile(), util.StringifyJSON(map[string]any{
		"provider":         nil,
		"lastUsedProvider": nil,
	})); err != nil {
		t.Fatal(err)
	}
	legacyRoute, err := util.NewBYOKRoute("cline:"+clineLegacyProviderName, "openai-chat", "https://legacy.example/api")
	if err != nil {
		t.Fatal(err)
	}
	if err := util.SaveBYOKRoutes([]util.BYOKRoute{legacyRoute}); err != nil {
		t.Fatal(err)
	}
	if !ClineProxyWired() {
		t.Fatal("legacy managed provider not detected as wired")
	}
	if got := DetectProxy("cline"); got.State != ProxyStateManaged {
		t.Fatalf("legacy wiring state = %s (%s), want %s", got.State, got.Detail, ProxyStateManaged)
	}
	if changed, _ := ConfigureClineProxy(); !changed {
		t.Fatal("legacy managed provider was not migrated")
	}
	raw, _ := util.ReadFileSafe(file)
	providers := clineProvidersOf(t, raw)
	if _, ok := providers.Get(clineLegacyProviderName); ok {
		t.Fatalf("legacy slot still present after migration:\n%s", raw)
	}
	if _, ok := providers.Get(clineProviderName); !ok {
		t.Fatalf("namespaced slot missing after migration:\n%s", raw)
	}
	if route, ok := util.ReadBYOKRoute("cline:" + clineProviderName); !ok || route.Upstream != "https://legacy.example/api" {
		t.Fatalf("BYOK route not migrated onto the namespaced slot: ok=%v route=%+v", ok, route)
	}
	if route, ok := util.ReadBYOKRoute("cline:" + clineLegacyProviderName); ok {
		t.Fatalf("legacy BYOK route left behind: %+v", route)
	}
	if !RemoveClineProxy() {
		t.Fatal("remove failed for migrated legacy wiring")
	}
	if ClineProxyWired() {
		t.Fatal("cline still wired after removal")
	}
	raw, _ = util.ReadFileSafe(file)
	if got := clineProvidersOf(t, raw); got.Len() != 0 {
		t.Fatalf("providers not drained after removal:\n%s", raw)
	}
	if util.Exists(clineProviderStateFile()) {
		t.Fatal("provider stash file still present")
	}
}

func TestClineManagedValuesAcceptsBothReservedSlots(t *testing.T) {
	endpoint := ProxyEndpointFor("cline")
	for _, name := range []string{clineProviderName, clineLegacyProviderName} {
		entry := util.NewOrderedMap()
		settings := util.NewOrderedMap()
		settings.Set("provider", clineProviderType)
		settings.Set("apiKey", proxyWireKey())
		settings.Set("model", "deepseek-v4-flash")
		settings.Set("baseUrl", endpoint)
		entry.Set("settings", settings)
		if !clineManagedValues(entry) {
			t.Fatalf("%s slot (%s): desired provider not recognized as managed", name, util.StringifyJSON(entry))
		}
	}
	routed := util.NewOrderedMap()
	routedSettings := util.NewOrderedMap()
	routedSettings.Set("provider", clineProviderType)
	routedSettings.Set("apiKey", "user-key")
	routedSettings.Set("model", "user-model")
	routedSettings.Set("baseUrl", endpoint)
	headers := util.NewOrderedMap()
	headers.Set(headroomBaseURLHeader, "https://api.user.test")
	routedSettings.Set("headers", headers)
	routed.Set("settings", routedSettings)
	if !clineManagedValues(routed) {
		t.Fatal("routed provider not recognized as managed")
	}
	foreign := util.NewOrderedMap()
	foreignSettings := util.NewOrderedMap()
	foreignSettings.Set("provider", clineProviderType)
	foreignSettings.Set("apiKey", "user-key")
	foreignSettings.Set("model", "gpt-4o")
	foreignSettings.Set("baseUrl", endpoint)
	foreign.Set("settings", foreignSettings)
	if clineManagedValues(foreign) {
		t.Fatal("user-owned provider sharing the endpoint recognized as managed")
	}
}

func TestClineRoutedProviderKeepsProviderTypeForBothSlots(t *testing.T) {
	endpoint := ProxyEndpointFor("cline")
	upstream := "https://api.user.test/v1"
	for _, providerType := range []string{clineProviderName, clineLegacyProviderName} {
		existing := util.NewOrderedMap()
		settings := util.NewOrderedMap()
		settings.Set("provider", providerType)
		settings.Set("apiKey", "user-key")
		settings.Set("model", "user-model")
		settings.Set("baseUrl", upstream)
		headers := util.NewOrderedMap()
		headers.Set("x-user", "keep")
		settings.Set("headers", headers)
		existing.Set("settings", settings)
		routed, ok, _ := clineRoutedProvider(existing, false)
		if !ok {
			t.Fatalf("%s provider: not routable", providerType)
		}
		routedSettings, _ := routed.Get("settings")
		routedMap, _ := routedSettings.(*util.OrderedMap)
		if got, _ := routedMap.Get("provider"); got != clineProviderType {
			t.Fatalf("%s provider: routed provider = %v, want %s", providerType, got, clineProviderType)
		}
		if got, _ := routedMap.Get("baseUrl"); got != endpoint {
			t.Fatalf("%s provider: routed baseUrl = %v, want %s", providerType, got, endpoint)
		}
		routedHeaders, _ := routedMap.Get("headers")
		routedHeaderMap, _ := routedHeaders.(*util.OrderedMap)
		if value, _ := routedHeaderMap.Get(headroomBaseURLHeader); value != "https://api.user.test" {
			t.Fatalf("%s provider: routed upstream header = %v", providerType, value)
		}
		if value, _ := routedHeaderMap.Get("x-user"); value != "keep" {
			t.Fatalf("%s provider: user header dropped", providerType)
		}
	}
}

// The namespaced slot itself stays conflict-protected: foreign content there is
// never overwritten, and it is the only slot that can block wiring.
func TestClineNamespacedSlotReportsConflictWithoutOverwrite(t *testing.T) {
	clineProxyTestHome(t)
	file := clineProvidersFile()
	seed := `{"providers":{"` + clineProviderName + `":{"settings":{"provider":"` + clineProviderType + `","apiKey":"user-key","model":"gpt-4o","baseUrl":"https://api.user.test/v1"}}}}`
	if err := util.WriteFile(file, seed); err != nil {
		t.Fatal(err)
	}
	got := DetectProxy("cline")
	if got.State != ProxyStateConflict || got.Detail != "reserved provider differs" {
		t.Fatalf("state = %s (%s), want %s (%s)", got.State, got.Detail, ProxyStateConflict, "reserved provider differs")
	}
	if changed, _ := ConfigureClineProxy(); changed {
		t.Fatal("foreign namespaced slot was adopted")
	}
	if raw, _ := util.ReadFileSafe(file); raw != seed {
		t.Fatalf("foreign namespaced slot changed:\n%s", raw)
	}
	if ClineProxyWired() {
		t.Fatal("foreign namespaced slot counted as wired")
	}
}

// A user-owned provider that shares the proxy endpoint must survive wiring and
// unwiring under every name, including the old reserved one.
func TestClineNeverOverwritesUserProviderSharingEndpoint(t *testing.T) {
	clineProxyTestHome(t)
	file := clineProvidersFile()
	endpoint := ProxyEndpointFor("cline")
	seed := `{"lastUsedProvider":"openai-compatible","providers":{` +
		`"openai-compatible":{"settings":{"provider":"` + clineProviderType + `","apiKey":"user-key","model":"gpt-4o","baseUrl":"` + endpoint + `"},"tokenSource":"manual"},` +
		`"user-direct":{"settings":{"provider":"` + clineProviderType + `","apiKey":"user-key","model":"gpt-4o","baseUrl":"` + endpoint + `"},"tokenSource":"manual"}}}`
	if err := util.WriteFile(file, seed); err != nil {
		t.Fatal(err)
	}
	providers := clineProvidersOf(t, seed)
	wantLegacy := clineEntryBytes(providers, clineLegacyProviderName)
	wantDirect := clineEntryBytes(providers, "user-direct")

	if changed, _ := ConfigureClineProxy(); !changed {
		t.Fatal("configure refused with user providers sharing the endpoint")
	}
	if !ClineProxyWired() {
		t.Fatal("cline proxy not wired")
	}
	raw, _ := util.ReadFileSafe(file)
	after := clineProvidersOf(t, raw)
	if got := clineEntryBytes(after, clineLegacyProviderName); got != wantLegacy {
		t.Fatalf("legacy-named user provider overwritten:\nwant %s\ngot  %s", wantLegacy, got)
	}
	if got := clineEntryBytes(after, "user-direct"); got != wantDirect {
		t.Fatalf("user provider overwritten:\nwant %s\ngot  %s", wantDirect, got)
	}
	if _, ok := after.Get(clineProviderName); !ok {
		t.Fatalf("namespaced slot not written:\n%s", raw)
	}
	if !RemoveClineProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(file)
	after = clineProvidersOf(t, raw)
	if got := clineEntryBytes(after, clineLegacyProviderName); got != wantLegacy {
		t.Fatalf("user provider deleted by unwire:\n%s", raw)
	}
	if got := clineEntryBytes(after, "user-direct"); got != wantDirect {
		t.Fatalf("user provider deleted by unwire:\n%s", raw)
	}
	if _, ok := after.Get(clineProviderName); ok {
		t.Fatalf("managed provider left behind:\n%s", raw)
	}
}
