package agents

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// nativeV2Seed is a native OpenCode v2 config: only a "providers" map, no v1-shape "provider" map.
const nativeV2Seed = `{"providers":{"acme":{"name":"Acme","package":"@opencode/ai/providers/openai-compatible",` +
	`"settings":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"},` +
	`"models":{"m1":{"modelID":"m1","name":"M1","limit":{"context":1000,"output":100}}}}}}`

func writeNativeV2Config(t *testing.T, dir string, content string) string {
	t.Helper()
	path := filepath.Join(dir, "opencode.json")
	if err := util.WriteFile(path, content); err != nil {
		t.Fatal(err)
	}
	if util.OpenCodePathsV2().Config != path {
		t.Fatalf("v2 config=%s want %s", util.OpenCodePathsV2().Config, path)
	}
	return path
}

func discoverNativeV2() []openCodeBYOK {
	byoks, _ := discoverOpenCodeV2BYOKIn(openCodeV2ConfigFiles(), "opencode2:", byokStashPathFor("opencode2"))
	return byoks
}

// TestDiscoverOpenCodeV2NativeProvider pins that a native v2 config yields the provider with its upstream, key and protocol.
func TestDiscoverOpenCodeV2NativeProvider(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	writeNativeV2Config(t, v2Dir, nativeV2Seed)
	byoks := discoverNativeV2()
	if len(byoks) != 1 {
		t.Fatalf("discovered %d providers, want 1", len(byoks))
	}
	b := byoks[0]
	if b.ID != "acme" {
		t.Fatalf("provider id=%q want acme", b.ID)
	}
	if b.BaseURL != "https://api.acme.test/v1" {
		t.Fatalf("upstream=%q want https://api.acme.test/v1", b.BaseURL)
	}
	if b.APIKey != "acme-key" {
		t.Fatalf("api key=%q want acme-key", b.APIKey)
	}
	if b.Protocol != "openai-chat" {
		t.Fatalf("protocol=%q want openai-chat", b.Protocol)
	}
	if b.Npm != "@ai-sdk/openai-compatible" {
		t.Fatalf("npm=%q want @ai-sdk/openai-compatible", b.Npm)
	}
	if b.File != util.OpenCodePathsV2().Config {
		t.Fatalf("file=%q want %q", b.File, util.OpenCodePathsV2().Config)
	}
}

// TestDiscoverOpenCodeV2NativeProviderFromHeadroomHeaders pins that a provider already pointing at the proxy resolves its upstream from the headroom origin and original-path headers.
func TestDiscoverOpenCodeV2NativeProviderFromHeadroomHeaders(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	seed := `{"providers":{"acme":{"name":"Acme","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"` + ProxyEndpointFor("opencode2") + `","apiKey":"acme-key"},` +
		`"headers":{"x-headroom-base-url":"http://127.0.0.1:9099","x-headroom-original-path":"/v1/chat/completions"}}}}`
	writeNativeV2Config(t, v2Dir, seed)
	byoks := discoverNativeV2()
	if len(byoks) != 1 {
		t.Fatalf("discovered %d providers, want 1", len(byoks))
	}
	if byoks[0].BaseURL != "http://127.0.0.1:9099/v1" {
		t.Fatalf("upstream=%q want http://127.0.0.1:9099/v1", byoks[0].BaseURL)
	}
}

// TestConfigureOpenCodeProxyV2MirrorsNativeProvider pins that wiring a native v2 config registers the route and rewrites the provider entry to the proxy.
func TestConfigureOpenCodeProxyV2MirrorsNativeProvider(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, nativeV2Seed)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	route, ok := util.ReadBYOKRoute("opencode2:acme")
	if !ok {
		t.Fatal("native v2 provider got no route")
	}
	if route.Upstream != "https://api.acme.test/v1" {
		t.Fatalf("route upstream=%q want https://api.acme.test/v1", route.Upstream)
	}
	if route.Protocol != "openai-chat" {
		t.Fatalf("route protocol=%q want openai-chat", route.Protocol)
	}
	raw, _ := util.ReadFileSafe(v2Path)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("invalid v2 config: %s", raw)
	}
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("providers map missing: %s", raw)
	}
	value, ok := providers.Get("acme")
	if !ok {
		t.Fatalf("providers.acme missing: %s", raw)
	}
	entry, ok := value.(*util.OrderedMap)
	if !ok {
		t.Fatalf("providers.acme has wrong type: %T", value)
	}
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != ProxyEndpointFor("opencode2") {
		t.Fatalf("providers.acme settings.baseURL=%v want %v", base, ProxyEndpointFor("opencode2"))
	}
	if key, _ := settings.Get("apiKey"); key != "acme-key" {
		t.Fatalf("providers.acme settings.apiKey=%v want acme-key", key)
	}
	headers, _ := mapChild(entry, "headers")
	routeHeader, _ := headers.Get("X-Tokless-Route")
	header, _ := routeHeader.(string)
	if !strings.HasPrefix(header, "opencode2:acme.") {
		t.Fatalf("providers.acme route header=%q", header)
	}
	if header != util.BYOKRouteHeader(route) {
		t.Fatalf("providers.acme route header=%q want %q", header, util.BYOKRouteHeader(route))
	}
	upstream, _ := headers.Get(headroomBaseURLHeader)
	if upstream != "https://api.acme.test" {
		t.Fatalf("providers.acme %s=%v want https://api.acme.test", headroomBaseURLHeader, upstream)
	}
	if path, _ := headers.Get(openCodeV2OriginalPathHeader); path != "/v1/chat/completions" {
		t.Fatalf("providers.acme %s=%v want /v1/chat/completions", openCodeV2OriginalPathHeader, path)
	}
	if pkg, _ := entry.Get("package"); pkg != "@opencode/ai/providers/openai-compatible" {
		t.Fatalf("providers.acme package=%v want @opencode/ai/providers/openai-compatible", pkg)
	}
	models, ok := mapChild(entry, "models")
	if !ok {
		t.Fatalf("providers.acme lost its models: %s", raw)
	}
	if _, ok := models.Get("m1"); !ok {
		t.Fatalf("providers.acme models lost m1: %s", raw)
	}
}

// TestConfigureOpenCodeProxyV2MigratesLegacyNativeRoute pins that a native v2 entry carrying a legacy opencode.
func TestConfigureOpenCodeProxyV2MigratesLegacyNativeRoute(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, `{"providers":{"acme":{"name":"Acme","package":"@opencode/ai/providers/openai-compatible",`+
		`"settings":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"},`+
		`"headers":{"X-Tokless-Route":"opencode:acme.OLDTOKEN"}}}}`)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	if _, ok := util.ReadBYOKRoute("opencode:acme"); ok {
		t.Fatal("the legacy route id was reused")
	}
	route, ok := util.ReadBYOKRoute("opencode2:acme")
	if !ok {
		t.Fatal("native v2 provider got no route")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	cfg := util.TryParseJsonc(raw)
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("providers map missing: %s", raw)
	}
	value, _ := providers.Get("acme")
	entry, _ := value.(*util.OrderedMap)
	headers, _ := mapChild(entry, "headers")
	routeHeader, _ := headers.Get("X-Tokless-Route")
	header, _ := routeHeader.(string)
	if !strings.HasPrefix(header, "opencode2:acme.") {
		t.Fatalf("legacy route header was not migrated: %q", header)
	}
	if header != util.BYOKRouteHeader(route) {
		t.Fatalf("migrated route header=%q want %q", header, util.BYOKRouteHeader(route))
	}
	if strings.Contains(raw, "opencode:acme.OLDTOKEN") {
		t.Fatalf("legacy route survived the migration: %s", raw)
	}
}

// TestOpenCodeV2ModelsFromNativeProvidersMap pins that the models of a native v2 provider are read from the "providers" map.
func TestOpenCodeV2ModelsFromNativeProvidersMap(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, nativeV2Seed)
	models := openCodeV2Models(v2Path, "acme")
	value, ok := models.Get("m1")
	if !ok {
		t.Fatalf("model m1 missing: %v", models.Keys())
	}
	entry, ok := value.(*util.OrderedMap)
	if !ok {
		t.Fatalf("model m1 has wrong type: %T", value)
	}
	if modelID, _ := entry.Get("modelID"); modelID != "m1" {
		t.Fatalf("model m1 modelID=%v want m1", modelID)
	}
	if name, _ := entry.Get("name"); name != "M1" {
		t.Fatalf("model m1 name=%v want M1", name)
	}
	limit, ok := mapChild(entry, "limit")
	if !ok {
		t.Fatalf("model m1 lost its limit")
	}
	if context, _ := limit.Get("context"); fmt.Sprint(context) != "1000" {
		t.Fatalf("model m1 limit.context=%v want 1000", context)
	}
}

// TestDiscoverOpenCodeV2SkipsUnresolvableProvider pins that a provider whose upstream cannot be resolved is skipped and its config entry left untouched.
func TestDiscoverOpenCodeV2SkipsUnresolvableProvider(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, `{"providers":{"acme":{"name":"Acme","package":"@opencode/ai/providers/openai-compatible",`+
		`"settings":{"baseURL":"`+ProxyEndpointFor("opencode2")+`","apiKey":"acme-key"}}}}`)
	if byoks := discoverNativeV2(); len(byoks) != 0 {
		t.Fatalf("discovered %d providers, want 0", len(byoks))
	}
	if _, _ = ConfigureOpenCodeProxyV2(); true {
	}
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); ok {
		t.Fatal("the unresolvable provider got a route")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("invalid v2 config: %s", raw)
	}
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("providers map missing: %s", raw)
	}
	value, ok := providers.Get("acme")
	if !ok {
		t.Fatalf("providers.acme was dropped: %s", raw)
	}
	entry, _ := value.(*util.OrderedMap)
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != ProxyEndpointFor("opencode2") {
		t.Fatalf("providers.acme settings.baseURL=%v want %v", base, ProxyEndpointFor("opencode2"))
	}
	if headers, ok := mapChild(entry, "headers"); ok {
		if route, _ := headers.Get("X-Tokless-Route"); route != nil {
			t.Fatalf("providers.acme gained a route header: %v", route)
		}
	}
}

// nativeV2EnvSeed is a native v2 provider whose key is an env ref, the way users write it.
const nativeV2EnvSeed = `{"providers":{"acme":{"name":"Acme Corp","package":"@opencode/ai/providers/openai-compatible",` +
	`"settings":{"baseURL":"http://127.0.0.1:9099/v1","apiKey":"{env:RT_TEST_KEY}"},` +
	`"models":{"m1":{"modelID":"m1","name":"acme-m1"}}}}}`

// v2ProviderEntry reads one provider entry of the native v2 "providers" map.
func v2ProviderEntry(t *testing.T, raw string, id string) *util.OrderedMap {
	t.Helper()
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("invalid v2 config: %s", raw)
	}
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("providers map missing: %s", raw)
	}
	value, ok := providers.Get(id)
	if !ok {
		t.Fatalf("providers.%s missing: %s", id, raw)
	}
	entry, ok := value.(*util.OrderedMap)
	if !ok {
		t.Fatalf("providers.%s has wrong type: %T", id, value)
	}
	return entry
}

// TestConfigureOpenCodeProxyV2NativeMirrorKeepsRawKeyAndName pins that the mirror of a native v2 provider keeps the raw "{env:VAR}" key and the provider name the user wrote.
func TestConfigureOpenCodeProxyV2NativeMirrorKeepsRawKeyAndName(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	t.Setenv("RT_TEST_KEY", "super-secret-value")
	v2Path := writeNativeV2Config(t, v2Dir, nativeV2EnvSeed)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if strings.Contains(raw, "super-secret-value") {
		t.Fatalf("the resolved secret leaked into the config: %s", raw)
	}
	entry := v2ProviderEntry(t, raw, "acme")
	if name, _ := entry.Get("name"); name != "Acme Corp" {
		t.Fatalf("providers.acme name=%v want Acme Corp", name)
	}
	settings, _ := mapChild(entry, "settings")
	if key, _ := settings.Get("apiKey"); key != "{env:RT_TEST_KEY}" {
		t.Fatalf("providers.acme settings.apiKey=%v want {env:RT_TEST_KEY}", key)
	}
}

// TestRemoveOpenCodeProxyV2PrunesMirrorEntryAndKeepsForeign pins that unwiring still deletes a tokless-created mirror entry and leaves a foreign entry alone.
func TestRemoveOpenCodeProxyV2PrunesMirrorEntryAndKeepsForeign(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, `{"provider":{"acme":{"npm":"@ai-sdk/openai-compatible",`+
		`"options":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"}}},`+
		`"providers":{"manual":{"name":"Manual","package":"@opencode/ai/providers/openai-compatible",`+
		`"settings":{"baseURL":"https://api.manual.test/v1"}}}}`)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if entry := v2ProviderEntry(t, raw, "acme"); entry == nil {
		t.Fatalf("the mirror entry was not created: %s", raw)
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove reported no change")
	}
	raw, _ = util.ReadFileSafe(v2Path)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("invalid v2 config: %s", raw)
	}
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("the providers map was dropped: %s", raw)
	}
	if _, ok := providers.Get("acme"); ok {
		t.Fatalf("the tokless-created mirror entry survived: %s", raw)
	}
	value, ok := providers.Get("manual")
	if !ok {
		t.Fatalf("the foreign entry was dropped: %s", raw)
	}
	entry, _ := value.(*util.OrderedMap)
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != "https://api.manual.test/v1" {
		t.Fatalf("the foreign entry was rewritten: baseURL=%v", base)
	}
	if _, ok := mapChild(entry, "headers"); ok {
		t.Fatalf("the foreign entry gained headers: %s", raw)
	}
}
