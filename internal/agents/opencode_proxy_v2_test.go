package agents

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// openCodeV2StubBinary installs an opencode2 stub binary so the v2 provider mirror is reachable.
func openCodeV2StubBinary(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "opencode2")
	if util.IsWin {
		bin += ".bat"
		t.Setenv("USERPROFILE", util.Home())
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'opencode v2.0.6\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// openCodeV2TestHome isolates HOME, the v1 config dir and the v2 config dir, and installs an opencode2 stub binary.
func openCodeV2TestHome(t *testing.T) string {
	t.Helper()
	opencodeProxyTestHome(t)
	t.Setenv("HOME", util.Home())
	if util.IsWin {
		t.Setenv("USERPROFILE", util.Home())
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	v2Dir := filepath.Join(t.TempDir(), "opencode2")
	t.Setenv("OPENCODE_CONFIG_DIR_V2", v2Dir)
	if err := util.EnsureDir(v2Dir); err != nil {
		t.Fatal(err)
	}
	openCodeV2StubBinary(t)
	return v2Dir
}

// v2ProviderSeed is a v1-shape provider block in a v2 config file.
const v2ProviderSeed = `{"provider":{"acme":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"},"models":{"m1":{"name":"M1"}}}}}`

// managedOpenCodePluginJSON renders a managed transport-plugin entry.
func managedOpenCodePluginJSON(routes string) string {
	return `{"plugin":[["` + openCodeTransportPluginURL() + `",{"proxyUrl":"` +
		strings.TrimSuffix(ProxyEndpointFor("opencode2"), "/v1") + `"` + routes +
		`,"upstreams":{},"byokGatewayUrl":"` + util.BYOKGatewayEndpoint() + `"}]],"tools":{"headroom_retrieve":false}}`
}

// v2MirrorSeed is a v2 config whose provider mirror points at the proxy.
func v2MirrorSeed() string {
	return `{"providers":{"acme":{"name":"acme","package":"@opencode/ai/providers/openai-compatible","settings":{"baseURL":"` +
		ProxyEndpointFor("opencode2") +
		`"},"headers":{"X-Tokless-Route":"opencode2:acme.token"}}},"tools":{"headroom_retrieve":false}}`
}

func TestOpenCodeProxyV2WiredNeedsV2Config(t *testing.T) {
	openCodeV2TestHome(t)
	if OpenCodeProxyV2Wired() {
		t.Fatal("empty v2 config must not report wired")
	}
	v1Path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(v1Path, managedOpenCodePluginJSON("")); err != nil {
		t.Fatal(err)
	}
	if OpenCodeProxyV2Wired() {
		t.Fatal("v1 config must not satisfy the v2 wired check")
	}
	if !OpenCodeProxyWired() {
		t.Fatal("v1 config must satisfy the v1 wired check")
	}
	v2Path := util.OpenCodePathsV2().Config
	if err := util.WriteFile(v2Path, v2MirrorSeed()); err != nil {
		t.Fatal(err)
	}
	if !OpenCodeProxyV2Wired() {
		t.Fatal("expected v2 wired")
	}
	if err := util.WriteFile(v2Path, managedOpenCodePluginJSON("")); err != nil {
		t.Fatal(err)
	}
	if OpenCodeProxyV2Wired() {
		t.Fatal("a plugin tuple must not satisfy the v2 wired check")
	}
	if err := util.WriteFile(v2Path, strings.Replace(v2MirrorSeed(), `"headroom_retrieve":false`, `"headroom_retrieve":true`, 1)); err != nil {
		t.Fatal(err)
	}
	if OpenCodeProxyV2Wired() {
		t.Fatal("v2 wired must require the retrieve tool suppression")
	}
}

func TestConfigureOpenCodeProxyV2LeavesV1Untouched(t *testing.T) {
	openCodeV2TestHome(t)
	v1Path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(v1Path, `{"theme":"dark"}`); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(util.OpenCodePathsV2().Config, v2ProviderSeed); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(v1Path)
	if err != nil {
		t.Fatal(err)
	}
	changed, file := ConfigureOpenCodeProxyV2()
	if !changed {
		t.Fatal("configure reported no change")
	}
	if file != util.OpenCodePathsV2().Config {
		t.Fatalf("configure wrote %s, want %s", file, util.OpenCodePathsV2().Config)
	}
	after, err := os.ReadFile(v1Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("v1 config mutated:\n%s", after)
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsV2().Config)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("invalid v2 config: %s", raw)
	}
	toolsValue, ok := cfg.Get("tools")
	if !ok {
		t.Fatalf("tools missing: %s", raw)
	}
	tools, ok := toolsValue.(*util.OrderedMap)
	if !ok {
		t.Fatalf("tools has wrong type: %T", toolsValue)
	}
	if value, ok := tools.Get("headroom_retrieve"); !ok || value != false {
		t.Fatalf("retrieve tool not disabled: %s", raw)
	}
	if _, ok := cfg.Get("plugin"); ok {
		t.Fatalf("the v2 lane must not write a plugin tuple: %s", raw)
	}
	if !OpenCodeProxyV2Wired() {
		t.Fatal("expected v2 wired after configure")
	}
	if OpenCodeProxyWired() {
		t.Fatal("v1 must stay unwired")
	}
	if util.Exists(byokStashPath()) {
		t.Fatal("v2 flow wrote the v1 BYOK stash")
	}
	if util.Exists(openCodeRetrieveStatePath()) {
		t.Fatal("v2 flow wrote the v1 retrieve stash")
	}
	if !util.Exists(openCodeRetrieveStatePathFor("opencode2")) {
		t.Fatal("v2 flow did not write its own retrieve stash")
	}
}

// TestConfigureOpenCodeProxyV2DropsManagedPluginTuple pins that the v2 lane removes the managed plugin tuple an older version wrote and keeps every other plugin.
func TestConfigureOpenCodeProxyV2DropsManagedPluginTuple(t *testing.T) {
	openCodeV2TestHome(t)
	v2Path := util.OpenCodePathsV2().Config
	existing := `{"plugin":[["` + openCodeTransportPluginURL() + `",{"proxyUrl":"` +
		strings.TrimSuffix(ProxyEndpointFor("opencode2"), "/v1") +
		`","routes":{"acme":"opencode2:acme.token"},"upstreams":{"acme":"https://api.acme.test/v1"}}],"oh-my-opencode-slim@2.2.24"],"tools":{"headroom_retrieve":false}}`
	if err := util.WriteFile(v2Path, existing); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure must report the dropped tuple")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if strings.Contains(raw, openCodeTransportPluginURL()) {
		t.Fatalf("the managed plugin tuple survived: %s", raw)
	}
	if !strings.Contains(raw, "oh-my-opencode-slim@2.2.24") {
		t.Fatalf("a foreign plugin was dropped: %s", raw)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); changed {
		t.Fatal("second configure must be idempotent")
	}
}

func TestConfigureOpenCodeProxyV2DoesNotPruneProviders(t *testing.T) {
	openCodeV2TestHome(t)
	v2Path := util.OpenCodePathsV2().Config
	owned := `{"name":"acme","package":"@opencode/ai/providers/openai-compatible","settings":{"baseURL":"` +
		strings.TrimSuffix(ProxyEndpointFor("opencode2"), "/v1") +
		`"},"headers":{"X-Tokless-Route":"opencode2:acme.token"}}`
	foreign := `{"name":"manual","package":"@opencode/ai/providers/openai-compatible","settings":{"baseURL":"https://api.manual.test/v1"}}`
	seed := `{"providers":{"acme":` + owned + `,"manual":` + foreign + `}}`
	if err := util.WriteFile(v2Path, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	cfg := util.TryParseJsonc(raw)
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("providers map was dropped: %s", raw)
	}
	if _, ok := providers.Get("acme"); !ok {
		t.Fatalf("tokless-owned provider was pruned: %s", raw)
	}
	if _, ok := providers.Get("manual"); !ok {
		t.Fatalf("user provider was pruned: %s", raw)
	}
}

func TestOpenCodeProxyV2RoutesUseOwnNamespace(t *testing.T) {
	openCodeV2TestHome(t)
	v2Path := util.OpenCodePathsV2().Config
	seed := `{"provider":{"acme":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"}}}}`
	if err := util.WriteFile(v2Path, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	route, ok := util.ReadBYOKRoute("opencode2:acme")
	if !ok {
		t.Fatal("v2 route was not namespaced")
	}
	if route.Upstream != "https://api.acme.test/v1" {
		t.Fatalf("route upstream=%s", route.Upstream)
	}
	if _, ok := util.ReadBYOKRoute("opencode:acme"); ok {
		t.Fatal("v2 flow must not create opencode: routes")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if !strings.Contains(raw, `"opencode2:acme.`) {
		t.Fatalf("v2 plugin entry misses the route: %s", raw)
	}
	if strings.Contains(raw, `"opencode:acme`) {
		t.Fatalf("v2 plugin entry uses the v1 namespace: %s", raw)
	}
}

func TestRemoveOpenCodeProxyV2RemovesOnlyV2State(t *testing.T) {
	openCodeV2TestHome(t)
	v1Path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(v1Path, `{"theme":"dark"}`); err != nil {
		t.Fatal(err)
	}
	v2Path := util.OpenCodePathsV2().Config
	seed := `{"provider":{"acme":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"}}}}`
	if err := util.WriteFile(v2Path, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	if _, _, err := util.UpsertBYOKRoute("opencode:keep", "openai-chat", "https://api.keep.test/v1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := util.UpsertBYOKRoute("opencode2:keep", "openai-chat", "https://api.keep.test/v1"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(v1Path)
	if err != nil {
		t.Fatal(err)
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove failed")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if strings.Contains(raw, openCodeTransportPluginURL()) {
		t.Fatalf("v2 transport plugin survived remove: %s", raw)
	}
	if strings.Contains(raw, "headroom_retrieve") {
		t.Fatalf("retrieve tool setting survived remove: %s", raw)
	}
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); ok {
		t.Fatal("opencode2: route survived remove")
	}
	if _, ok := util.ReadBYOKRoute("opencode2:keep"); ok {
		t.Fatal("opencode2: route survived remove")
	}
	if _, ok := util.ReadBYOKRoute("opencode:keep"); !ok {
		t.Fatal("v1 route was removed by the v2 flow")
	}
	after, err := os.ReadFile(v1Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("v1 config mutated by the v2 remove:\n%s", after)
	}
	if util.Exists(byokStashPathFor("opencode2")) {
		t.Fatal("v2 BYOK stash survived remove")
	}
}

func TestConfigureOpenCodeProxyV2DelegatesToSharedConfig(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv("HOME", util.Home())
	if util.IsWin {
		t.Setenv("USERPROFILE", util.Home())
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENCODE_CONFIG_DIR_V2", "")
	if !openCodeV2SharesV1Config() {
		t.Skip("v2 config dir does not fall back to the v1 dir in this environment")
	}
	changed, file := ConfigureOpenCodeProxyV2()
	if !changed {
		t.Fatal("configure reported no change")
	}
	if file != util.OpenCodePathsResolved().Config {
		t.Fatalf("configure wrote %s, want %s", file, util.OpenCodePathsResolved().Config)
	}
	if !OpenCodeProxyV2Wired() || !OpenCodeProxyWired() {
		t.Fatal("expected the shared config to be wired for both versions")
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove failed")
	}
	if OpenCodeProxyV2Wired() || OpenCodeProxyWired() {
		t.Fatal("still wired after remove")
	}
}

func TestConfigureOpenCodeProxyV2SharedConfigMirrorsProviders(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv("HOME", util.Home())
	if util.IsWin {
		t.Setenv("USERPROFILE", util.Home())
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENCODE_CONFIG_DIR_V2", "")
	if !openCodeV2SharesV1Config() {
		t.Skip("v2 config dir does not fall back to the v1 dir in this environment")
	}
	dir := openCodeHeadroomPluginDir()
	if err := os.Remove(filepath.Join(dir, "entry.opencode.js")); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "index.js"), "export default { id: \"headroom\", server: async () => ({}), setup: async () => ({}) }\n"); err != nil {
		t.Fatal(err)
	}
	shared := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(shared, v2ProviderSeed); err != nil {
		t.Fatal(err)
	}
	if changed, file := ConfigureOpenCodeProxyV2(); !changed || file != shared {
		t.Fatalf("configure changed=%v file=%s, want true %s", changed, file, shared)
	}
	if !OpenCodeProxyWired() || !OpenCodeProxyV2Wired() {
		t.Fatal("shared config not wired for both versions")
	}
	providers := openCodeV2ProvidersOf(shared)
	if providers == nil {
		raw, _ := util.ReadFileSafe(shared)
		t.Fatalf("v2 provider mirror missing: %s", raw)
	}
	value, ok := providers.Get("acme")
	entry, isMap := value.(*util.OrderedMap)
	if !ok || !isMap {
		raw, _ := util.ReadFileSafe(shared)
		t.Fatalf("mirrored provider missing: %s", raw)
	}
	if base, _ := openCodeV2ProviderBaseAndKey(entry); base != openCodeProxyEndpointFor("opencode2:") {
		t.Fatalf("mirrored provider baseURL = %q, want the proxy endpoint", base)
	}
	headers, _ := mapChild(entry, "headers")
	route, _ := headers.Get("X-Tokless-Route")
	if route, _ := route.(string); !strings.HasPrefix(route, "opencode2:acme") {
		t.Fatalf("mirrored provider route = %v, want the opencode2 namespace", route)
	}
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); !ok {
		t.Fatal("opencode2 route was not registered")
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove failed")
	}
	after, _ := util.ReadFileSafe(shared)
	if strings.Contains(after, `"providers"`) || strings.Contains(after, "X-Tokless-Route") {
		t.Fatalf("v2 mirror survived remove: %s", after)
	}
	if !strings.Contains(after, "https://api.acme.test/v1") {
		t.Fatalf("v1 provider was not restored: %s", after)
	}
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); ok {
		t.Fatal("opencode2 route survived remove")
	}
}

func TestConfigureOpenCodeProxyDoesNotTouchV2Dir(t *testing.T) {
	openCodeV2TestHome(t)
	v2Dir := util.OpenCodePathsV2().Dir
	v2Path := util.OpenCodePathsV2().Config
	seed := `{"theme":"light","providers":{"manual":{"name":"manual"}}}`
	if err := util.WriteFile(v2Path, seed); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	if err := filepath.Walk(v2Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		before[path] = raw
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("configure reported no change")
	}
	after := map[string][]byte{}
	if err := filepath.Walk(v2Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		after[path] = raw
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for path, raw := range before {
		now, ok := after[path]
		if !ok {
			t.Fatalf("v2 file %s disappeared", path)
		}
		if !bytes.Equal(raw, now) {
			t.Fatalf("v1 flow modified the v2 config %s:\n%s", path, now)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Fatalf("v1 flow wrote a new v2 file %s", path)
		}
	}
	if util.Exists(byokStashPathFor("opencode2")) {
		t.Fatal("v1 flow wrote the v2 BYOK stash")
	}
	if util.Exists(openCodeRetrieveStatePathFor("opencode2")) {
		t.Fatal("v1 flow wrote the v2 retrieve stash")
	}
}

// legacyV2Seed is a v2 config carrying a legacy opencode:-prefixed mirror entry and a legacy plugin entry.
func legacyV2Seed() string {
	return `{"provider":{"acme":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"},"models":{"m1":{"name":"M1"}}}},"providers":{"acme":{"name":"acme","package":"@opencode/ai/providers/openai-compatible","settings":{"baseURL":"` +
		ProxyEndpointFor("opencode2") +
		`"},"headers":{"X-Tokless-Route":"opencode:acme.OLDTOKEN"}},"old":{"name":"old","package":"@opencode/ai/providers/openai-compatible","settings":{"baseURL":"` +
		ProxyEndpointFor("opencode2") +
		`"},"headers":{"X-Tokless-Route":"opencode:old.OLDTOKEN"}}},"plugin":[["` +
		openCodeTransportPluginURL() + `",{"proxyUrl":"` +
		strings.TrimSuffix(ProxyEndpointFor("opencode2"), "/v1") +
		`","routes":{"acme":"opencode:acme.OLDTOKEN"},"upstreams":{"acme":"https://api.acme.test/v1"}}]]}`
}

// TestConfigureOpenCodeProxyV2MigratesLegacyRoutePrefix pins that legacy opencode:-prefixed mirror entries are migrated to the opencode2: namespace.
func TestConfigureOpenCodeProxyV2MigratesLegacyRoutePrefix(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := filepath.Join(v2Dir, "opencode.json")
	if err := util.WriteFile(v2Path, legacyV2Seed()); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	cfg := util.TryParseJsonc(raw)
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
	headers, _ := mapChild(entry, "headers")
	route, _ := headers.Get("X-Tokless-Route")
	header, _ := route.(string)
	if !strings.HasPrefix(header, "opencode2:acme.") {
		t.Fatalf("legacy route header was not migrated: %q", header)
	}
	if strings.Contains(raw, "opencode:acme.OLDTOKEN") {
		t.Fatalf("legacy route survived the migration: %s", raw)
	}
	if !strings.Contains(raw, "opencode:old.OLDTOKEN") {
		t.Fatalf("the unmigratable legacy entry was dropped by the wire: %s", raw)
	}
}

// TestRemoveOpenCodeProxyV2PrunesLegacyRoutePrefix pins that an unwire prunes legacy opencode:-prefixed mirror entries and leaves the v1 routes alone.
func TestRemoveOpenCodeProxyV2PrunesLegacyRoutePrefix(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := filepath.Join(v2Dir, "opencode.json")
	if err := util.WriteFile(v2Path, legacyV2Seed()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := util.UpsertBYOKRoute("opencode:keep", "openai-chat", "https://api.keep.test/v1"); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove failed")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if strings.Contains(raw, "OLDTOKEN") {
		t.Fatalf("legacy mirror entry survived the unwire: %s", raw)
	}
	cfg := util.TryParseJsonc(raw)
	if cfg != nil {
		if providers, ok := mapChild(cfg, "providers"); ok {
			if _, exists := providers.Get("acme"); exists {
				t.Fatalf("legacy mirror entry survived the unwire: %s", raw)
			}
			if _, exists := providers.Get("old"); exists {
				t.Fatalf("unmigrated legacy mirror entry survived the unwire: %s", raw)
			}
		}
	}
	if _, ok := util.ReadBYOKRoute("opencode:keep"); !ok {
		t.Fatal("the v1 route was removed by the v2 flow")
	}
}

// TestConfigureOpenCodeProxyV2SharedDirKeepsV1ConfigUntouched pins that the v2 flow never writes the v1 config file, even when both versions share a directory.
func TestConfigureOpenCodeProxyV2SharedDirKeepsV1ConfigUntouched(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv("HOME", util.Home())
	if util.IsWin {
		t.Setenv("USERPROFILE", util.Home())
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENCODE_CONFIG_DIR_V2", "")
	openCodeV2StubBinary(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	v1Path := filepath.Join(dir, "opencode.json")
	v2Path := filepath.Join(dir, "opencode2.json")
	if err := util.WriteFile(v1Path, `{"theme":"dark"}`); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(v2Path, v2ProviderSeed); err != nil {
		t.Fatal(err)
	}
	if util.OpenCodePathsV2().Dir != dir {
		t.Skipf("v2 dir %s does not share the v1 dir %s", util.OpenCodePathsV2().Dir, dir)
	}
	if util.OpenCodePathsResolved().Config != v1Path {
		t.Fatalf("v1 config=%s want %s", util.OpenCodePathsResolved().Config, v1Path)
	}
	if util.OpenCodePathsV2().Config != v2Path {
		t.Fatalf("v2 config=%s want %s", util.OpenCodePathsV2().Config, v2Path)
	}
	before, err := os.ReadFile(v1Path)
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	after, err := os.ReadFile(v1Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("v1 config mutated by the v2 flow:\n%s", after)
	}
	v2Raw, _ := util.ReadFileSafe(v2Path)
	v2Cfg := util.TryParseJsonc(v2Raw)
	if v2Cfg == nil {
		t.Fatalf("invalid v2 config: %s", v2Raw)
	}
	providers, ok := mapChild(v2Cfg, "providers")
	if !ok {
		t.Fatalf("the v2 mirror did not land in the v2 config: %s", v2Raw)
	}
	if _, ok := providers.Get("acme"); !ok {
		t.Fatalf("the v2 mirror did not land in the v2 config: %s", v2Raw)
	}
}

// TestRemoveOpenCodeProxyV2RestoresMirrorOnRouteFailure pins that a failed route delete restores the provider mirror the unwire had already pruned.
func TestRemoveOpenCodeProxyV2RestoresMirrorOnRouteFailure(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := filepath.Join(v2Dir, "opencode.json")
	if err := util.WriteFile(v2Path, v2ProviderSeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	routesPath := filepath.Join(util.HeadroomPathsResolved().Root, "byok.routes.json")
	util.SetWriteFileOverride(func(p, content string) error {
		if p == routesPath {
			return errors.New("route registry write blocked")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(content), 0o644)
	})
	defer util.SetWriteFileOverride(nil)
	if RemoveOpenCodeProxyV2() {
		t.Fatal("remove must fail when the route delete fails")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if !strings.Contains(raw, "X-Tokless-Route") {
		t.Fatalf("the mirror was not restored after the failed remove: %s", raw)
	}
}

// TestConfigureOpenCodeProxyV2RefreshesStaleRoutedEntry pins that a mirror entry written under an old endpoint keeps its route and gets the current endpoint.
func TestConfigureOpenCodeProxyV2RefreshesStaleRoutedEntry(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := filepath.Join(v2Dir, "opencode.json")
	entry := `{"providers":{"acme":{"name":"acme","package":"@opencode/ai/providers/openai-compatible","settings":{"baseURL":"http://127.0.0.1:9999/v1","apiKey":"acme-key"},"headers":{"X-Tokless-Route":"opencode2:acme.token","x-headroom-base-url":"https://api.acme.test/v1"},"models":{"resp-1":{"modelID":"resp-1","name":"Resp One"}}}}}`
	if err := util.WriteFile(v2Path, entry); err != nil {
		t.Fatal(err)
	}
	if util.OpenCodePathsV2().Config != v2Path {
		t.Fatalf("v2 config=%s want %s", util.OpenCodePathsV2().Config, v2Path)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if !strings.Contains(raw, ProxyEndpointFor("opencode2")) {
		t.Fatalf("stale endpoint was not refreshed: %s", raw)
	}
	if strings.Contains(raw, "127.0.0.1:9999") {
		t.Fatalf("stale endpoint survived: %s", raw)
	}
	if !strings.Contains(raw, `"opencode2:acme.`) {
		t.Fatalf("existing route was dropped: %s", raw)
	}
	if !strings.Contains(raw, `"x-headroom-base-url": "https://api.acme.test"`) {
		t.Fatalf("the upstream was dropped: %s", raw)
	}
	cfg := util.TryParseJsonc(raw)
	providers, _ := mapChild(cfg, "providers")
	value, _ := providers.Get("acme")
	models, _ := mapChild(value.(*util.OrderedMap), "models")
	modelValue, _ := models.Get("resp-1")
	modelSettings, _ := mapChild(modelValue.(*util.OrderedMap), "settings")
	if base, _ := modelSettings.Get("baseURL"); base != ProxyEndpointFor("opencode2") {
		t.Fatalf("stale entry kept a model without the proxy endpoint: %s", raw)
	}
	if !OpenCodeProxyV2Wired() {
		t.Fatalf("v2 must be wired after the refresh: %s", raw)
	}
}

// TestSyncOpenCodeV2BYOKPrunesEntryWithOldEndpoint pins that ownership alone (the route header) makes a mirror entry prunable, even under an old port.
func TestSyncOpenCodeV2BYOKPrunesEntryWithOldEndpoint(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv("HOME", util.Home())
	if util.IsWin {
		t.Setenv("USERPROFILE", util.Home())
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	path := filepath.Join(t.TempDir(), "opencode.json")
	seed := `{"providers":{"acme":{"name":"acme","settings":{"baseURL":"http://127.0.0.1:9999/v1"},"headers":{"X-Tokless-Route":"opencode2:acme.OLDTOKEN"}}}}`
	if err := util.WriteFile(path, seed); err != nil {
		t.Fatal(err)
	}
	changed, ok := syncOpenCodeV2BYOKFor(nil, path, "opencode2:", true)
	if !ok || !changed {
		t.Fatalf("prune changed=%v ok=%v", changed, ok)
	}
	raw, _ := util.ReadFileSafe(path)
	if strings.Contains(raw, "acme") {
		t.Fatalf("entry under an old endpoint survived the prune: %s", raw)
	}
}

// TestRemoveOpenCodeProxyV2RestoresNativeEntryShadowedByV1Shape pins that a native v2 entry the user wrote is given back on the unwire, not deleted, even when a v1-shape file carries the same id.
func TestRemoveOpenCodeProxyV2RestoresNativeEntryShadowedByV1Shape(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, `{"providers":{"acme":{"name":"Acme Corp",`+
		`"package":"@opencode/ai/providers/openai-compatible",`+
		`"settings":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"},`+
		`"models":{"m1":{"modelID":"m1"}}}}}`)
	if err := util.WriteFile(filepath.Join(v2Dir, "config.json"), v2ProviderSeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("configure reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if !strings.Contains(raw, "X-Tokless-Route") {
		t.Fatalf("the native entry was not taken over by the proxy: %s", raw)
	}
	if !RemoveOpenCodeProxyV2() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(v2Path)
	if strings.Contains(raw, "X-Tokless-Route") || strings.Contains(raw, "x-headroom-") {
		t.Fatalf("tokless headers survived the unwire: %s", raw)
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("invalid v2 config: %s", raw)
	}
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("the providers map was dropped: %s", raw)
	}
	value, ok := providers.Get("acme")
	if !ok {
		t.Fatalf("the native entry was deleted instead of given back: %s", raw)
	}
	entry, _ := value.(*util.OrderedMap)
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != "https://api.acme.test/v1" {
		t.Fatalf("baseURL=%v want the original upstream", base)
	}
	if key, _ := settings.Get("apiKey"); key != "acme-key" {
		t.Fatalf("apiKey=%v want acme-key", key)
	}
	if name, _ := entry.Get("name"); name != "Acme Corp" {
		t.Fatalf("name=%v want Acme Corp", name)
	}
	if models, ok := mapChild(entry, "models"); !ok {
		t.Fatalf("the native entry lost its models: %s", raw)
	} else if _, ok := models.Get("m1"); !ok {
		t.Fatalf("the native entry lost model m1: %s", raw)
	}
}

// TestSyncOpenCodeV2BYOKWritesWithoutV2Binary pins that the mirror is written even when no opencode2 binary is installed.
func TestSyncOpenCodeV2BYOKWritesWithoutV2Binary(t *testing.T) {
	opencodeProxyTestHome(t)
	t.Setenv("HOME", util.Home())
	if util.IsWin {
		t.Setenv("USERPROFILE", util.Home())
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "opencode.json")
	b := openCodeBYOK{ID: "acme", File: path, BaseURL: "https://api.example.test/api/v1", APIKey: "secret", Npm: "@ai-sdk/openai", RouteHeader: "opencode2:acme.token"}
	changed, ok := syncOpenCodeV2BYOKFor([]openCodeBYOK{b}, path, "opencode2:", false)
	if !ok || !changed {
		t.Fatalf("mirror not written without the v2 binary: changed=%v ok=%v", changed, ok)
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, "opencode2:acme.token") {
		t.Fatalf("mirror entry missing: %s", raw)
	}
}
