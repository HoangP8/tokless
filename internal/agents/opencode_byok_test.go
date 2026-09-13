package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestConfigureOpenCodeProxyUsesTransportPlugin(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	cfg := `{
  "provider": {
    "prov-a": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://api.provider-a.test/v1",
        "apiKey": "prov-a-test-key"
      },
      "models": {"m1": {"name": "M1"}}
    },
    "prov-b": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://api.provider-b.test/v1",
        "apiKey": "prov-b-test-key"
      }
    }
  }
}`
	if err := util.WriteFile(filepath.Join(dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(dir, "opencode.json"), `{"$schema":"https://opencode.ai/config.json"}`); err != nil {
		t.Fatal(err)
	}

	changed, _ := ConfigureOpenCodeProxy()
	if !changed {
		t.Fatal("configure reported no change")
	}
	if !OpenCodeProxyWired() {
		t.Fatal("expected transport plugin wired")
	}

	providerRaw, _ := util.ReadFileSafe(filepath.Join(dir, "config.json"))
	for _, upstream := range []string{"https://api.provider-a.test/v1", "https://api.provider-b.test/v1"} {
		if !strings.Contains(providerRaw, `"baseURL": "`+upstream+`"`) {
			t.Fatalf("upstream changed: %s", providerRaw)
		}
	}
	if strings.Contains(providerRaw, "127.0.0.1:8787") || strings.Contains(providerRaw, headroomBaseURLHeader) {
		t.Fatalf("provider config must remain transport-independent: %s", providerRaw)
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	parsedCfg := util.TryParseJsonc(raw)
	if parsedCfg == nil {
		t.Fatalf("invalid OpenCode config: %s", raw)
	}
	toolsValue, ok := parsedCfg.Get("tools")
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
	pluginsValue, ok := parsedCfg.Get("plugin")
	if !ok {
		t.Fatalf("plugin missing: %s", raw)
	}
	plugins, ok := pluginsValue.([]any)
	if !ok || len(plugins) != 1 || !isOpenCodeTransportPluginEntry(plugins[0]) {
		t.Fatalf("transport plugin malformed: %s", raw)
	}
	if !OpenCodeProxyWired() {
		t.Fatal("OpenCode proxy must require retrieve tool suppression")
	}
	if !strings.Contains(providerRaw, "prov-a-test-key") || !strings.Contains(providerRaw, "prov-b-test-key") {
		t.Fatalf("keys lost: %s", providerRaw)
	}
	if changed, _ := ConfigureOpenCodeProxy(); changed {
		t.Fatal("second configure was not idempotent")
	}

	if !RemoveOpenCodeProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	providerRaw, _ = util.ReadFileSafe(filepath.Join(dir, "config.json"))
	if !strings.Contains(providerRaw, "provider-a.test") || !strings.Contains(providerRaw, "provider-b.test") {
		t.Fatalf("provider upstream changed: %s", providerRaw)
	}
	if strings.Contains(raw, `"plugin"`) {
		t.Fatalf("transport plugin was not removed: %s", raw)
	}
	if OpenCodeProxyWired() {
		t.Fatal("still wired after remove")
	}
	raw, _ = util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	parsedCfg = util.TryParseJsonc(raw)
	if _, ok := parsedCfg.Get("tools"); ok {
		t.Fatal("remove must restore absent retrieve-tool setting")
	}
}

func TestOpenCodeRetrieveSettingRestoresUserValue(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(path, `{"tools":{"headroom_retrieve":true}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("configure no change")
	}
	if !RemoveOpenCodeProxy() {
		t.Fatal("remove no change")
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, `"headroom_retrieve": true`) {
		t.Fatalf("retrieve setting not restored: %s", raw)
	}
}

func TestRewriteProviderBaseURLPreservesCompactStyle(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	cfg := `{"provider":{"prov-a":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.provider-a.test/v1","apiKey":"k"},"models":{}}}}`
	path := filepath.Join(dir, "config.json")
	if err := util.WriteFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	if !rewriteProviderBaseURL(path, "prov-a", "http://127.0.0.1:8787/v1") {
		t.Fatal("rewrite no change")
	}
	raw, _ := util.ReadFileSafe(path)
	if strings.Count(raw, "\n") > 1 {
		t.Fatalf("pretty-printed compact config:\n%s", raw)
	}
	if !strings.Contains(raw, `"baseURL":"http://127.0.0.1:8787/v1"`) {
		t.Fatalf("baseURL not swapped: %s", raw)
	}
	if !strings.Contains(raw, `"apiKey":"k"`) || !strings.Contains(raw, `"prov-a"`) {
		t.Fatalf("body corrupted: %s", raw)
	}
	if !rewriteProviderBaseURL(path, "prov-a", "https://api.provider-a.test/v1") {
		t.Fatal("restore no change")
	}
	raw, _ = util.ReadFileSafe(path)
	if strings.Count(raw, "\n") > 1 || !strings.Contains(raw, `"baseURL":"https://api.provider-a.test/v1"`) {
		t.Fatalf("restore failed: %s", raw)
	}
}

func TestRewriteProviderBaseURLScopedNoCrossWire(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	proxy := "http://127.0.0.1:8787/v1"
	cfg := `{
  "provider": {
    "prov-a": {
      "options": {
        "baseURL": "` + proxy + `",
        "apiKey": "ka"
      }
    },
    "prov-b": {
      "options": {
        "baseURL": "` + proxy + `",
        "apiKey": "kb"
      }
    }
  }
}`
	path := filepath.Join(dir, "config.json")
	if err := util.WriteFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	if !rewriteProviderBaseURL(path, "prov-b", "https://api.provider-b.test/v1") {
		t.Fatal("scoped restore no change")
	}
	raw, _ := util.ReadFileSafe(path)
	lines := strings.Count(raw, "\n")
	if lines != strings.Count(cfg, "\n") {
		t.Fatalf("line count smashed: got %d want %d\n%s", lines, strings.Count(cfg, "\n"), raw)
	}
	if !strings.Contains(raw, `"baseURL": "https://api.provider-b.test/v1"`) {
		t.Fatalf("prov-b not restored: %s", raw)
	}
	// prov-a must still be proxy (first match would have corrupted it)
	if strings.Count(raw, `"baseURL": "`+proxy+`"`) != 1 {
		t.Fatalf("cross-wire: proxy base count wrong:\n%s", raw)
	}
	if strings.Contains(raw, "provider-a.test") {
		t.Fatalf("prov-a wrongly changed: %s", raw)
	}
}

func TestRewriteProviderBaseURLNoCrashOnBadConfig(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	cases := []string{
		`{ not json`,
		`{"provider":{"x":{"options":{"apiKey":"k"}}}}`,
		`// comment\n{"provider":{}}`,
		``,
	}
	for _, c := range cases {
		_ = util.WriteFile(path, c)
		if rewriteProviderBaseURL(path, "x", "http://127.0.0.1:8787/v1") {
			t.Fatalf("should no-op on bad/missing base: %q", c)
		}
		raw, _ := util.ReadFileSafe(path)
		if raw != c && c != "" {
			// empty may not write; others must stay byte-identical
			if c != "" && raw != c {
				t.Fatalf("config mutated: got %q want %q", raw, c)
			}
		}
	}
}

func TestWireUnwireRoundTripNoCrossWire(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	cfg := `{
  "provider": {
    "prov-a": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://api.provider-a.test/v1",
        "apiKey": "key-a"
      }
    },
    "prov-b": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://api.provider-b.test/v1",
        "apiKey": "key-b"
      }
    },
    "prov-c": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://api.provider-c.test/v1",
        "apiKey": "key-c"
      }
    }
  }
}`
	path := filepath.Join(dir, "config.json")
	if err := util.WriteFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	changed, routes := wireOpenCodeBYOK()
	if changed || len(routes) != 3 {
		t.Fatalf("wire changed=%v routes=%d", changed, len(routes))
	}
	raw, _ := util.ReadFileSafe(path)
	if strings.Count(raw, `"baseURL": "https://api.provider-`) != 3 {
		t.Fatalf("provider routes changed:\n%s", raw)
	}
	if unwireOpenCodeBYOK() {
		t.Fatal("unwire changed transport-only config")
	}
	raw, _ = util.ReadFileSafe(path)
	for _, host := range []string{"provider-a.test", "provider-b.test", "provider-c.test"} {
		if !strings.Contains(raw, host) {
			t.Fatalf("host %s not restored:\n%s", host, raw)
		}
	}
	if strings.Contains(raw, "127.0.0.1:8787") {
		t.Fatalf("proxy left after unwire:\n%s", raw)
	}
	// repeated configure must stay stable
	if changed, routes = wireOpenCodeBYOK(); changed || len(routes) != 3 {
		t.Fatalf("rewire routes=%d", len(routes))
	}
	raw2, _ := util.ReadFileSafe(path)
	if raw2 != raw {
		t.Fatalf("second configure drifted\n--- first ---\n%s\n--- second ---\n%s", raw, raw2)
	}
}

func TestUnwireOpenCodeBYOKDoesNotOverwriteUserEdit(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	providerConfig := `{"provider":{"prov-a":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.provider-a.test/v1","apiKey":"key-a"},"models":{}}}}`
	if err := util.WriteFile(path, providerConfig); err != nil {
		t.Fatal(err)
	}
	if changed, routes := wireOpenCodeBYOK(); changed || len(routes) != 1 {
		t.Fatalf("transport-only wire changed=%v routes=%d", changed, len(routes))
	}
	userEdited := "{\"provider\":{\"prov-a\":{\"options\":{\"baseURL\":\"https://user-edited.example/v1\"}}}}"
	if err := util.WriteFile(path, userEdited); err != nil {
		t.Fatal(err)
	}
	if unwireOpenCodeBYOK() {
		t.Fatal("transport-only unwire changed provider")
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, "https://user-edited.example/v1") {
		t.Fatalf("user edit overwritten: %s", raw)
	}
}

func TestWireOpenCodeBYOKRefusesMalformedStash(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	original := `{"provider":{"prov-a":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.provider-a.test/v1","apiKey":"key-a"}}}}`
	if err := util.WriteFile(path, original); err != nil {
		t.Fatal(err)
	}
	if err := util.EnsureDir(filepath.Dir(byokStashPath())); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(byokStashPath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, routes := wireOpenCodeBYOK(); changed || len(routes) != 0 {
		t.Fatalf("malformed stash was adopted: changed=%v routes=%d", changed, len(routes))
	}
	raw, _ := util.ReadFileSafe(path)
	if raw != original {
		t.Fatalf("provider config mutated: %s", raw)
	}
}

func TestWireOpenCodeBYOKRollsBackPartialFailure(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	first := "{\"provider\":{\"first\":{\"options\":{\"baseURL\":\"https://first.example/v1\",\"apiKey\":\"key\"}}}}"
	second := "{\"provider\":{\"second\":{\"options\":{\"baseURL\":\"https://second.example/v1\",\"apiKey\":\"key\",\"headers\":[]}}}}"
	firstPath := filepath.Join(dir, "config.json")
	secondPath := filepath.Join(dir, "opencode.json")
	if err := util.WriteFile(firstPath, first); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(secondPath, second); err != nil {
		t.Fatal(err)
	}
	if changed, routes := wireOpenCodeBYOK(); changed || len(routes) != 2 {
		t.Fatalf("transport-only wire changed=%v routes=%d", changed, len(routes))
	}
	raw, _ := util.ReadFileSafe(firstPath)
	if raw != first {
		t.Fatalf("first provider config changed: %s", raw)
	}
	raw, _ = util.ReadFileSafe(secondPath)
	if raw != second {
		t.Fatalf("second provider config changed: %s", raw)
	}
}

func TestRemoveOpenCodeProxyRefusesMalformedRetrieveStash(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(path, "{\"plugin\":[[\""+openCodeTransportPluginURL()+"\",{\"proxyUrl\":\"http://127.0.0.1:8787\"}]],\"tools\":{\"headroom_retrieve\":false}}"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFileMode(openCodeRetrieveStatePath(), "{", 0o600); err != nil {
		t.Fatal(err)
	}
	if RemoveOpenCodeProxy() {
		t.Fatal("malformed retrieve stash was ignored")
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, openCodeTransportPluginURL()) {
		t.Fatal("plugin removed despite malformed stash")
	}
}

func TestRemoveOpenCodeProxyRefusesMalformedBYOKStash(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(path, "{\"plugin\":[[\""+openCodeTransportPluginURL()+"\",{\"proxyUrl\":\"http://127.0.0.1:8787\"}]],\"tools\":{\"headroom_retrieve\":false}}"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFileMode(byokStashPath(), "{", 0o600); err != nil {
		t.Fatal(err)
	}
	if RemoveOpenCodeProxy() {
		t.Fatal("malformed BYOK stash was ignored")
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, openCodeTransportPluginURL()) {
		t.Fatal("plugin removed despite malformed BYOK stash")
	}
}

func TestRemoveOpenCodeProxyUnwiresStalePluginAfterPortChange(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(path, `{"plugin":["file://user-plugin.js"],"theme":"dark"}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("configure reported no change")
	}
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "9999")
	if openCodeTransportPluginWired() {
		t.Fatal("stale plugin URL must not count as wired")
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, openCodeTransportPluginURL()) {
		t.Fatal("expected leftover tokless plugin before remove")
	}
	if !RemoveOpenCodeProxy() {
		t.Fatal("stale tokless plugin must unwire")
	}
	raw, _ = util.ReadFileSafe(path)
	if strings.Contains(raw, openCodeTransportPluginURL()) {
		t.Fatalf("stale plugin left behind:\n%s", raw)
	}
	if !strings.Contains(raw, "file://user-plugin.js") || !strings.Contains(raw, `"theme": "dark"`) {
		t.Fatalf("user config lost:\n%s", raw)
	}
	if OpenCodeProxyWired() {
		t.Fatal("expected unwired after stale remove")
	}
	if _, exists := util.ReadFileSafe(openCodeRetrieveStatePath()); exists {
		t.Fatal("retrieve stash not cleared")
	}
}

func TestConfigureOpenCodeProxyRollsBackPluginWhenBYOKFails(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := util.OpenCodePathsResolved().Config
	original := "{\"tools\":{\"headroom_retrieve\":true}}"
	if err := util.WriteFile(path, original); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFileMode(byokStashPath(), "{", 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); changed {
		t.Fatal("configure reported success")
	}
	raw, _ := util.ReadFileSafe(path)
	if raw != original {
		t.Fatalf("plugin config not rolled back: %s", raw)
	}
	if _, exists := util.ReadFileSafe(openCodeRetrieveStatePath()); exists {
		t.Fatal("retrieve stash not rolled back")
	}
}
