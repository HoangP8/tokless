package agents

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// nativeV2UserAcme is the acme provider body of nativeV2UserSeed.
const nativeV2UserAcme = `{"name":"Acme Corp",` +
	`"package":"@opencode/ai/providers/openai-compatible",` +
	`"options":{"temperature":0.5},"env":{"ACME_MODE":"fast"},` +
	`"settings":{"baseURL":"https://api.acme.test/v1","apiKey":"{env:RT_TEST_KEY}"},` +
	`"headers":{"X-Custom":"keep-me"},` +
	`"models":{"m1":{"modelID":"m1","name":"M1"}}}`

// nativeV2UserSeed is a native v2 provider the way a user writes it.
const nativeV2UserSeed = `{"providers":{"acme":` + nativeV2UserAcme + `}}`

// TestOpenCodeV2WireTwiceUnwireRestoresNativeProvider pins that a native v2 provider survives a wire, a second wire and the unwire with its upstream, raw key, package, name and fields.
func TestOpenCodeV2WireTwiceUnwireRestoresNativeProvider(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	t.Setenv("RT_TEST_KEY", "super-secret-value")
	seed := `{"providers":{"acme":` + nativeV2UserAcme + `,"plain":{"name":"Plain",` +
		`"package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"https://api.plain.test/v1","apiKey":"plain-key"}}}}`
	v2Path := writeNativeV2Config(t, v2Dir, seed)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the first wire reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if strings.Contains(raw, "super-secret-value") {
		t.Fatalf("the resolved secret leaked into the config: %s", raw)
	}
	entry := v2ProviderEntry(t, raw, "acme")
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != ProxyEndpointFor("opencode2") {
		t.Fatalf("the entry was not taken over: baseURL=%v", base)
	}
	headers, _ := mapChild(entry, "headers")
	if v, _ := headers.Get("X-Custom"); v != "keep-me" {
		t.Fatalf("the user header was dropped: %v", v)
	}
	if _, ok := headers.Get("X-Tokless-Route"); !ok {
		t.Fatalf("the route header is missing: %s", raw)
	}
	models, _ := mapChild(entry, "models")
	modelValue, _ := models.Get("m1")
	model := modelValue.(*util.OrderedMap)
	modelSettings, _ := mapChild(model, "settings")
	if base, _ := modelSettings.Get("baseURL"); base != ProxyEndpointFor("opencode2") {
		t.Fatalf("model settings.baseURL=%v want the proxy", base)
	}
	modelHeaders, _ := mapChild(model, "headers")
	for k, w := range map[string]string{headroomBaseURLHeader: "https://api.acme.test", openCodeV2OriginalPathHeader: "/v1/chat/completions"} {
		if v, _ := modelHeaders.Get(k); v != w {
			t.Fatalf("model %s=%v want %s", k, v, w)
		}
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); changed {
		t.Fatal("the second wire changed the config")
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove reported no change")
	}
	raw, _ = util.ReadFileSafe(v2Path)
	if strings.Contains(raw, "super-secret-value") {
		t.Fatalf("the resolved secret leaked into the config: %s", raw)
	}
	entry = v2ProviderEntry(t, raw, "acme")
	settings, _ = mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != "https://api.acme.test/v1" {
		t.Fatalf("baseURL=%v want the original upstream", base)
	}
	if key, _ := settings.Get("apiKey"); key != "{env:RT_TEST_KEY}" {
		t.Fatalf("apiKey=%v want the raw key", key)
	}
	if name, _ := entry.Get("name"); name != "Acme Corp" {
		t.Fatalf("name=%v want Acme Corp", name)
	}
	if pkg, _ := entry.Get("package"); pkg != "@opencode/ai/providers/openai-compatible" {
		t.Fatalf("package=%v want the user package", pkg)
	}
	headers, _ = mapChild(entry, "headers")
	if v, _ := headers.Get("X-Custom"); v != "keep-me" {
		t.Fatalf("the user header was dropped: %v", v)
	}
	for _, name := range []string{"X-Tokless-Route", headroomBaseURLHeader, openCodeV2OriginalPathHeader} {
		if _, ok := headers.Get(name); ok {
			t.Fatalf("the tokless header %s survived the unwire: %s", name, raw)
		}
	}
	options, ok := mapChild(entry, "options")
	if !ok {
		t.Fatalf("the options were dropped: %s", raw)
	}
	if v, _ := options.Get("temperature"); fmt.Sprint(v) != "0.5" {
		t.Fatalf("options.temperature=%v want 0.5", v)
	}
	env, ok := mapChild(entry, "env")
	if !ok {
		t.Fatalf("the env was dropped: %s", raw)
	}
	if v, _ := env.Get("ACME_MODE"); v != "fast" {
		t.Fatalf("env.ACME_MODE=%v want fast", v)
	}
	models, ok = mapChild(entry, "models")
	if !ok {
		t.Fatalf("the models were dropped: %s", raw)
	}
	modelValue, ok = models.Get("m1")
	if !ok {
		t.Fatalf("the model m1 was dropped: %s", raw)
	}
	model = modelValue.(*util.OrderedMap)
	if _, ok := mapChild(model, "settings"); ok {
		t.Fatalf("the model mirror settings survived the unwire: %s", raw)
	}
	if _, ok := mapChild(model, "headers"); ok {
		t.Fatalf("the model mirror headers survived the unwire: %s", raw)
	}
	plain := v2ProviderEntry(t, raw, "plain")
	if _, ok := mapChild(plain, "headers"); ok {
		t.Fatalf("providers.plain kept the tokless headers: %s", raw)
	}
	plainSettings, _ := mapChild(plain, "settings")
	if base, _ := plainSettings.Get("baseURL"); base != "https://api.plain.test/v1" {
		t.Fatalf("providers.plain settings.baseURL=%v want https://api.plain.test/v1", base)
	}
}

// TestOpenCodeV2NativeRemoteBaseURLWinsOverHeader pins that a native provider keeps its own remote baseURL as the upstream, even when a headroom origin header is present.
func TestOpenCodeV2NativeRemoteBaseURLWinsOverHeader(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, `{"providers":{"acme":{"name":"Acme",`+
		`"package":"@opencode/ai/providers/openai-compatible",`+
		`"settings":{"baseURL":"https://api.remote.test/v1","apiKey":"acme-key"},`+
		`"headers":{"x-headroom-base-url":"https://api.stale.test"}}}}`)
	byoks := discoverNativeV2()
	if len(byoks) != 1 {
		t.Fatalf("discovered %d providers, want 1", len(byoks))
	}
	if byoks[0].BaseURL != "https://api.remote.test/v1" {
		t.Fatalf("upstream=%q want https://api.remote.test/v1", byoks[0].BaseURL)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	route, ok := util.ReadBYOKRoute("opencode2:acme")
	if !ok {
		t.Fatal("the native provider got no route")
	}
	if route.Upstream != "https://api.remote.test/v1" {
		t.Fatalf("route upstream=%q want https://api.remote.test/v1", route.Upstream)
	}
	raw, _ := util.ReadFileSafe(v2Path)
	entry := v2ProviderEntry(t, raw, "acme")
	headers, _ := mapChild(entry, "headers")
	if origin, _ := headers.Get(headroomBaseURLHeader); origin != "https://api.remote.test" {
		t.Fatalf("the mirror origin=%v want https://api.remote.test", origin)
	}
}

// TestOpenCodeV2MirrorEntryHasNoSelfLoop pins that a mirrored entry never resolves the proxy endpoint as its own upstream.
func TestOpenCodeV2MirrorEntryHasNoSelfLoop(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, nativeV2Seed)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	entry := v2ProviderEntry(t, raw, "acme")
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != ProxyEndpointFor("opencode2") {
		t.Fatalf("the mirror does not point at the proxy: %v", base)
	}
	byoks := discoverNativeV2()
	if len(byoks) != 1 {
		t.Fatalf("discovered %d providers, want 1", len(byoks))
	}
	if byoks[0].BaseURL == ProxyEndpointFor("opencode2") {
		t.Fatalf("the mirror entry resolves the proxy as its upstream: %s", byoks[0].BaseURL)
	}
	if byoks[0].BaseURL != "https://api.acme.test/v1" {
		t.Fatalf("upstream=%q want https://api.acme.test/v1", byoks[0].BaseURL)
	}
	route, ok := util.ReadBYOKRoute("opencode2:acme")
	if !ok {
		t.Fatal("the native provider got no route")
	}
	if route.Upstream != "https://api.acme.test/v1" {
		t.Fatalf("route upstream=%q want https://api.acme.test/v1", route.Upstream)
	}
}

// TestOpenCodeV2UnknownPackageIsSkipped pins that a provider with an unknown package is skipped and never mirrored.
func TestOpenCodeV2UnknownPackageIsSkipped(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, `{"providers":{"acme":{"name":"Acme",`+
		`"package":"@acme/ai/providers/custom",`+
		`"settings":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"}}}}`)
	if byoks := discoverNativeV2(); len(byoks) != 0 {
		t.Fatalf("discovered %d providers, want 0", len(byoks))
	}
	ConfigureOpenCodeProxyV2()
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); ok {
		t.Fatal("the unknown package got a route")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	entry := v2ProviderEntry(t, raw, "acme")
	if pkg, _ := entry.Get("package"); pkg != "@acme/ai/providers/custom" {
		t.Fatalf("package=%v want the user package", pkg)
	}
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != "https://api.acme.test/v1" {
		t.Fatalf("the entry was rewritten: baseURL=%v", base)
	}
	if headers, ok := mapChild(entry, "headers"); ok {
		if _, ok := headers.Get("X-Tokless-Route"); ok {
			t.Fatalf("the entry gained a route header: %s", raw)
		}
	}
	cfg := util.TryParseJsonc(raw)
	providers, _ := mapChild(cfg, "providers")
	if providers.Len() != 1 {
		t.Fatalf("the providers map was rewritten: %s", raw)
	}
}

// TestRemoveOpenCodeProxyV2RestoresSecondaryConfigFile pins that the unwire walks every v2 config file, giving a secondary user entry back and pruning a tokless-owned one.
func TestRemoveOpenCodeProxyV2RestoresSecondaryConfigFile(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	writeNativeV2Config(t, v2Dir, nativeV2Seed)
	secondary := filepath.Join(v2Dir, "config.json")
	owned := `{"providers":{"keep":{"name":"Keep","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"` + ProxyEndpointFor("opencode2") + `"},` +
		`"headers":{"X-Tokless-Route":"opencode2:keep.token"}},` +
		`"drop":{"name":"Drop","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"` + ProxyEndpointFor("opencode2") + `"},` +
		`"headers":{"X-Tokless-Route":"opencode2:drop.token"}}}}`
	if err := util.WriteFile(secondary, owned); err != nil {
		t.Fatal(err)
	}
	if err := saveBYOKStashAt(byokStashPathFor("opencode2"), map[string]byokStashEntry{
		"keep": {File: secondary, BaseURL: "https://api.keep.test/v1", Native: true, APIKey: "keep-key"},
	}); err != nil {
		t.Fatal(err)
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove reported no change")
	}
	raw, _ := util.ReadFileSafe(secondary)
	entry := v2ProviderEntry(t, raw, "keep")
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != "https://api.keep.test/v1" {
		t.Fatalf("the secondary entry was not restored: baseURL=%v", base)
	}
	if key, _ := settings.Get("apiKey"); key != "keep-key" {
		t.Fatalf("the secondary entry lost its key: %v", key)
	}
	if headers, ok := mapChild(entry, "headers"); ok {
		if _, ok := headers.Get("X-Tokless-Route"); ok {
			t.Fatalf("the tokless headers survived the unwire: %s", raw)
		}
	}
	cfg := util.TryParseJsonc(raw)
	providers, _ := mapChild(cfg, "providers")
	if _, ok := providers.Get("drop"); ok {
		t.Fatalf("the unowned secondary entry survived the unwire: %s", raw)
	}
}

// TestOpenCodeV2UnwireRestoresUserEntryAfterMirrorFileMoved pins that the user entry is given back in the file it was written in, even when the mirror file moved.
func TestOpenCodeV2UnwireRestoresUserEntryAfterMirrorFileMoved(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	t.Setenv("RT_TEST_KEY", "super-secret-value")
	secondary := filepath.Join(v2Dir, "config.json")
	if err := util.WriteFile(secondary, nativeV2UserSeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	mirror := filepath.Join(v2Dir, "opencode.json")
	if err := util.WriteFile(mirror, `{"providers":{}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the second wire reported no change")
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove reported no change")
	}
	raw, _ := util.ReadFileSafe(secondary)
	entry := v2ProviderEntry(t, raw, "acme")
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != "https://api.acme.test/v1" {
		t.Fatalf("the user entry was not restored: baseURL=%v", base)
	}
	if key, _ := settings.Get("apiKey"); key != "{env:RT_TEST_KEY}" {
		t.Fatalf("the user entry lost its raw key: %v", key)
	}
	headers, _ := mapChild(entry, "headers")
	if _, ok := headers.Get("X-Tokless-Route"); ok {
		t.Fatalf("the tokless headers survived the unwire: %s", raw)
	}
}

// TestRemoveOpenCodeProxyV2LeavesV1PluginAlone pins that the v2 lane never writes the v1 plugin file and never removes it.
func TestRemoveOpenCodeProxyV2LeavesV1PluginAlone(t *testing.T) {
	openCodeV2TestHome(t)
	pluginPath := openCodeBYOKPluginPath()
	content := []byte("// v1 transport plugin\n")
	if err := os.WriteFile(pluginPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(util.OpenCodePathsV2().Config, v2ProviderSeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove reported no change")
	}
	after, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, after) {
		t.Fatalf("the v1 plugin changed:\n%s", after)
	}
	if err := os.Remove(pluginPath); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(util.OpenCodePathsV2().Config, v2ProviderSeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove reported no change")
	}
	if util.Exists(pluginPath) {
		t.Fatal("the v2 lane created the v1 plugin file")
	}
}

// TestDetectProxyOpenCode2ReadsNativeProviders pins that the opencode2 detection reads the native "providers" map.
func TestDetectProxyOpenCode2ReadsNativeProviders(t *testing.T) {
	openCodeV2TestHome(t)
	if err := util.WriteFile(util.OpenCodePathsV2().Config, `{"providers":{"acme":{"name":"Acme",`+
		`"package":"@opencode/ai/providers/openai-compatible",`+
		`"settings":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"}}}}`); err != nil {
		t.Fatal(err)
	}
	detection := DetectProxy("opencode2")
	if detection.State != ProxyStateUnconfigured {
		t.Fatalf("state=%s want %s (%s)", detection.State, ProxyStateUnconfigured, detection.Detail)
	}
	if !strings.Contains(detection.Detail, "native provider") {
		t.Fatalf("the native providers map was not read: %s", detection.Detail)
	}
}
