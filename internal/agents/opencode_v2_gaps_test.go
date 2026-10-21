package agents

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// captureStdout returns what fn printed on stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := <-done
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestOpenCodeV2DuplicateIDMirrorTakesWinnerIdentity pins that the mirror of a duplicated id speaks the winner.
func TestOpenCodeV2DuplicateIDMirrorTakesWinnerIdentity(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	if err := util.WriteFile(filepath.Join(v2Dir, "config.json"), `{"provider":{"acme":{"npm":"@ai-sdk/anthropic",`+
		`"options":{"baseURL":"https://api.v1shape.test/v1","apiKey":"v1-key"}}}}`); err != nil {
		t.Fatal(err)
	}
	v2Path := writeNativeV2Config(t, v2Dir, `{"providers":{"acme":{"name":"Acme",`+
		`"package":"@opencode/ai/providers/openai-compatible",`+
		`"settings":{"baseURL":"https://api.native.test/v1","apiKey":"native-key"}}}}`)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	route, ok := util.ReadBYOKRoute("opencode2:acme")
	if !ok {
		t.Fatal("the provider got no route")
	}
	if route.Protocol != "anthropic-messages" {
		t.Fatalf("route protocol=%q want anthropic-messages", route.Protocol)
	}
	if route.Upstream != "https://api.v1shape.test/v1" {
		t.Fatalf("route upstream=%q want the v1-shape upstream", route.Upstream)
	}
	raw, _ := util.ReadFileSafe(v2Path)
	entry := v2ProviderEntry(t, raw, "acme")
	headers, _ := mapChild(entry, "headers")
	if origin, _ := headers.Get(headroomBaseURLHeader); origin != "https://api.v1shape.test" {
		t.Fatalf("the mirror origin=%v want the v1-shape upstream", origin)
	}
	settings, _ := mapChild(entry, "settings")
	if key, _ := settings.Get("apiKey"); key != "v1-key" {
		t.Fatalf("apiKey=%v want the winner key v1-key", key)
	}
	if pkg, _ := entry.Get("package"); pkg != "@opencode/ai/providers/anthropic" {
		t.Fatalf("package=%v want the winner package", pkg)
	}
	if name, _ := entry.Get("name"); name != "Acme" {
		t.Fatalf("the native entry was replaced: name=%v", name)
	}
}

// TestOpenCodeV2MirrorKeepsRawEnvKey pins that a v1-shape provider with an env key is mirrored with the raw key, never with the resolved secret.
func TestOpenCodeV2MirrorKeepsRawEnvKey(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	t.Setenv("RT_TEST_KEY", "super-secret-value")
	if err := util.WriteFile(filepath.Join(v2Dir, "config.json"), `{"provider":{"acme":{"npm":"@ai-sdk/openai-compatible",`+
		`"options":{"baseURL":"https://api.v1shape.test/v1","apiKey":"{env:RT_TEST_KEY}"}}}}`); err != nil {
		t.Fatal(err)
	}
	v2Path := writeNativeV2Config(t, v2Dir, nativeV2Seed)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if strings.Contains(raw, "super-secret-value") {
		t.Fatalf("the resolved secret leaked into the config: %s", raw)
	}
	entry := v2ProviderEntry(t, raw, "acme")
	settings, _ := mapChild(entry, "settings")
	if key, _ := settings.Get("apiKey"); key != "{env:RT_TEST_KEY}" {
		t.Fatalf("apiKey=%v want {env:RT_TEST_KEY}", key)
	}
}

// TestRemoveOpenCodeProxyV2RestoresMovedLane pins that the unwire restores the mirror of the lane the wire used, even when the lane moved.
func TestRemoveOpenCodeProxyV2RestoresMovedLane(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, nativeV2Seed)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if !strings.Contains(raw, "X-Tokless-Route") {
		t.Fatalf("the mirror was not written: %s", raw)
	}
	movedDir := filepath.Join(t.TempDir(), "opencode2")
	if err := util.EnsureDir(movedDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG_DIR_V2", movedDir)
	RemoveOpenCodeProxyV2()
	raw, _ = util.ReadFileSafe(v2Path)
	if strings.Contains(raw, "X-Tokless-Route") {
		t.Fatalf("the old mirror stayed wired: %s", raw)
	}
	if strings.Contains(raw, "headroom_retrieve") {
		t.Fatalf("the old config kept the retrieve suppression: %s", raw)
	}
	if util.Exists(byokStashPathFor("opencode2")) {
		t.Fatal("the v2 stash survived the remove")
	}
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); ok {
		t.Fatal("the route survived the remove")
	}
}

// TestRemoveOpenCodeProxyV2UsesRecordedLaneFiles pins that the unwire walks the files the wire recorded, not only the files that exist now.
func TestRemoveOpenCodeProxyV2UsesRecordedLaneFiles(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	writeNativeV2Config(t, v2Dir, nativeV2Seed)
	secondary := filepath.Join(v2Dir, "config.json")
	owned := `{"providers":{"acme":{"name":"acme","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"` + ProxyEndpointFor("opencode2") + `"},` +
		`"headers":{"X-Tokless-Route":"opencode2:acme.OLDTOKEN"}}}}`
	if err := util.WriteFile(secondary, owned); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	movedDir := filepath.Join(t.TempDir(), "opencode2")
	if err := util.EnsureDir(movedDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG_DIR_V2", movedDir)
	RemoveOpenCodeProxyV2()
	raw, _ := util.ReadFileSafe(secondary)
	if strings.Contains(raw, "X-Tokless-Route") {
		t.Fatalf("the recorded file kept the route header: %s", raw)
	}
}

// TestRemoveOpenCodeProxyV2ToleratesMissingStashedFile pins that a recorded file that is gone does not block the unwire.
func TestRemoveOpenCodeProxyV2ToleratesMissingStashedFile(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	v2Path := writeNativeV2Config(t, v2Dir, nativeV2Seed)
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	if err := os.Remove(v2Path); err != nil {
		t.Fatal(err)
	}
	RemoveOpenCodeProxyV2()
	if util.Exists(byokStashPathFor("opencode2")) {
		t.Fatal("the v2 stash survived the remove")
	}
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); ok {
		t.Fatal("the route survived the remove")
	}
}

// TestConfigureOpenCodeProxyV2RejectsCommentedConfig pins that a comment-bearing primary config stops the wire before a route exists.
func TestConfigureOpenCodeProxyV2RejectsCommentedConfig(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	commented := "{\n// the user comments\n" + `{"providers":{"acme":{"name":"Acme",` +
		`"package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"}}}}` + "\n"
	v2Path := writeNativeV2Config(t, v2Dir, commented)
	_, _, err := configureOpenCodeProxyV2Locked()
	if err == nil {
		t.Fatal("the wire accepted a comment-bearing config")
	}
	if !strings.Contains(err.Error(), "unsupported comment-bearing config") {
		t.Fatalf("err=%v want the unsupported comment-bearing config error", err)
	}
	if !strings.Contains(err.Error(), v2Path) {
		t.Fatalf("err=%v want the config path %s", err, v2Path)
	}
	changed, _ := ConfigureOpenCodeProxyV2()
	if changed {
		t.Fatal("the wire reported a change")
	}
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); ok {
		t.Fatal("a route was registered")
	}
	if util.Exists(byokStashPathFor("opencode2")) {
		t.Fatal("the stash was written")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	if !strings.Contains(raw, "// the user comments") {
		t.Fatalf("the config was rewritten: %s", raw)
	}
}

// TestConfigureOpenCodeProxyV2KeepsCommentedSecondaryConfig pins that a comment-bearing secondary config does not stop the wire.
func TestConfigureOpenCodeProxyV2KeepsCommentedSecondaryConfig(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	writeNativeV2Config(t, v2Dir, nativeV2Seed)
	secondary := filepath.Join(v2Dir, "config.json")
	if err := util.WriteFile(secondary, "{\n// secondary comment\n\"theme\":\"dark\"\n}"); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxyV2(); !changed {
		t.Fatal("the wire reported no change")
	}
	if _, ok := util.ReadBYOKRoute("opencode2:acme"); !ok {
		t.Fatal("the provider got no route")
	}
}

// TestConfigureOpenCodeProxyPrunesLegacyV2Mirror pins that the v1 wire removes the v2-shaped mirror an older tokless left in the v1 config and keeps a foreign entry.
func TestConfigureOpenCodeProxyPrunesLegacyV2Mirror(t *testing.T) {
	opencodeProxyTestHome(t)
	v1Path := util.OpenCodePathsResolved().Config
	legacy := `{"providers":{"legacy":{"name":"legacy","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"` + ProxyEndpointFor("opencode") + `"},` +
		`"headers":{"X-Tokless-Route":"opencode:legacy.OLDTOKEN"}},` +
		`"foreign":{"name":"foreign","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"https://api.foreign.test/v1"}}}}`
	if err := util.WriteFile(v1Path, legacy); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("the wire reported no change")
	}
	raw, _ := util.ReadFileSafe(v1Path)
	if strings.Contains(raw, "OLDTOKEN") {
		t.Fatalf("the legacy mirror survived the wire: %s", raw)
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("invalid v1 config: %s", raw)
	}
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("the providers map was dropped: %s", raw)
	}
	if _, ok := providers.Get("legacy"); ok {
		t.Fatalf("the legacy entry survived the wire: %s", raw)
	}
	value, ok := providers.Get("foreign")
	if !ok {
		t.Fatalf("the foreign entry was dropped: %s", raw)
	}
	entry, _ := value.(*util.OrderedMap)
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != "https://api.foreign.test/v1" {
		t.Fatalf("the foreign entry was rewritten: baseURL=%v", base)
	}
}

// TestRemoveOpenCodeProxyPrunesLegacyV2Mirror pins that the v1 unwire removes the v2-shaped mirror of the v1 config and keeps a foreign entry.
func TestRemoveOpenCodeProxyPrunesLegacyV2Mirror(t *testing.T) {
	opencodeProxyTestHome(t)
	v1Path := util.OpenCodePathsResolved().Config
	legacy := `{"providers":{"legacy":{"name":"legacy","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"` + ProxyEndpointFor("opencode") + `"},` +
		`"headers":{"X-Tokless-Route":"opencode:legacy.OLDTOKEN"}},` +
		`"foreign":{"name":"foreign","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"https://api.foreign.test/v1"}}}}`
	if err := util.WriteFile(v1Path, legacy); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("the wire reported no change")
	}
	if !RemoveOpenCodeProxy() {
		t.Fatal("remove reported no change")
	}
	raw, _ := util.ReadFileSafe(v1Path)
	if strings.Contains(raw, "OLDTOKEN") {
		t.Fatalf("the legacy mirror survived the unwire: %s", raw)
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("invalid v1 config: %s", raw)
	}
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		t.Fatalf("the providers map was dropped: %s", raw)
	}
	if _, ok := providers.Get("foreign"); !ok {
		t.Fatalf("the foreign entry was dropped: %s", raw)
	}
}

// TestConfigureOpenCodeProxyRestoresStashedV2Entry pins that a v2-shaped entry the v2 lane took over is given back instead of deleted.
func TestConfigureOpenCodeProxyRestoresStashedV2Entry(t *testing.T) {
	opencodeProxyTestHome(t)
	v1Path := util.OpenCodePathsResolved().Config
	mirror := `{"providers":{"legacy":{"name":"legacy","package":"@opencode/ai/providers/openai-compatible",` +
		`"settings":{"baseURL":"` + ProxyEndpointFor("opencode2") + `"},` +
		`"headers":{"X-Tokless-Route":"opencode2:legacy.OLDTOKEN"}}}}`
	if err := util.WriteFile(v1Path, mirror); err != nil {
		t.Fatal(err)
	}
	if err := saveBYOKStashAt(byokStashPathFor("opencode2"), map[string]byokStashEntry{
		"legacy": {File: v1Path, BaseURL: "https://api.legacy.test/v1", Native: true, APIKey: "legacy-key"},
	}); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); !changed {
		t.Fatal("the wire reported no change")
	}
	raw, _ := util.ReadFileSafe(v1Path)
	entry := v2ProviderEntry(t, raw, "legacy")
	settings, _ := mapChild(entry, "settings")
	if base, _ := settings.Get("baseURL"); base != "https://api.legacy.test/v1" {
		t.Fatalf("the user entry was not restored: baseURL=%v", base)
	}
	if key, _ := settings.Get("apiKey"); key != "legacy-key" {
		t.Fatalf("the user entry lost its key: %v", key)
	}
	headers, ok := mapChild(entry, "headers")
	if ok {
		if _, ok := headers.Get("X-Tokless-Route"); ok {
			t.Fatalf("the route header survived: %s", raw)
		}
	}
}

// TestOpenCodeV2SkippedProvidersAreReported pins that the providers the v2 lane cannot wire are reported with their reason.
func TestOpenCodeV2SkippedProvidersAreReported(t *testing.T) {
	v2Dir := openCodeV2TestHome(t)
	writeNativeV2Config(t, v2Dir, `{"providers":{"unknown":{"name":"unknown","package":"@acme/ai/providers/custom",`+
		`"settings":{"baseURL":"https://api.acme.test/v1","apiKey":"acme-key"}},`+
		`"noUpstream":{"name":"no upstream","package":"@opencode/ai/providers/openai-compatible",`+
		`"settings":{"baseURL":"`+ProxyEndpointFor("opencode2")+`","apiKey":"acme-key"}}}}`)
	secondary := filepath.Join(v2Dir, "config.json")
	if err := util.WriteFile(secondary, "{\n// secondary comment\n\"theme\":\"dark\"\n}"); err != nil {
		t.Fatal(err)
	}
	skipped := OpenCodeV2SkippedProviders()
	joined := strings.Join(skipped, "; ")
	if !strings.Contains(joined, "unknown: unknown package") {
		t.Fatalf("the unknown package was not reported: %v", skipped)
	}
	if !strings.Contains(joined, "noUpstream: no upstream") {
		t.Fatalf("the unresolvable upstream was not reported: %v", skipped)
	}
	if !strings.Contains(joined, "comment-bearing config "+secondary) {
		t.Fatalf("the comment-bearing config was not reported: %v", skipped)
	}
	out := captureStdout(t, func() { ConfigureOpenCodeProxyV2() })
	if !strings.Contains(out, "opencode2 skipped providers") {
		t.Fatalf("the wire did not report the skipped providers: %s", out)
	}
	if !strings.Contains(out, "unknown: unknown package") {
		t.Fatalf("the wire did not report the skipped ids: %s", out)
	}
}
