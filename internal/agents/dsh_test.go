package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// dshProxyTestHome pins DSH_HOME under a temp home and returns the
// cordis.patch.yml path.
func dshProxyTestHome(t *testing.T) string {
	t.Helper()
	pinToklessProxyEnv(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("DSH_HOME", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	return dshPatchFile()
}

const dshProxySeed = `# tokless wiring: BYOK provider (override existing rows) + core MCP servers (insert new rows).
- id: llm-pi-ai
  name: '@deepseek-ai/dsh-llm-pi-ai'
  config:
    providers:
      gateway:
        displayName: Tokless Gateway
        apiKeyEnv: TOKLESS_QWENCODER_API_KEY
        api: openai-completions
        baseURL: https://api.acme.example/v1
        headers:
          X-Custom: keep
        models:
          - id: deepseek-v4.1-flash
            contextWindow: 200000
- id: agent-default-model
  name: '@deepseek-ai/dsh-agent-default-model'
  config:
    provider: gateway
    model: deepseek-v4.1-flash
`

func TestDshRtkRoundTrip(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	_ = os.MkdirAll(filepath.Dir(dshPatchFile()), 0o755)
	_ = util.WriteFile(dshPatchFile(), dshProxySeed)
	exe := "/usr/local/bin/tokless"

	changed, _, err := ConfigureDshRtk(exe)
	if err != nil || !changed {
		t.Fatalf("configure=%v %v", changed, err)
	}
	if !DshRtkWired() {
		t.Fatal("rtk not wired")
	}
	patchRaw, _ := util.ReadFileSafe(dshPatchFile())
	if !strings.Contains(patchRaw, "id: tokless-rtk") {
		t.Fatalf("plugin row missing: %s", patchRaw)
	}
	if !strings.Contains(patchRaw, "name: "+yamlQuote(dshRtkPluginFile())) {
		t.Fatal("plugin row name wrong")
	}
	pluginRaw, ok := util.ReadFileSafe(dshRtkPluginFile())
	if !ok || !strings.Contains(pluginRaw, yamlQuote(exe)) {
		t.Fatalf("plugin module missing tokless bin: %q", pluginRaw)
	}
	if !strings.Contains(pluginRaw, "tools/pre-execute") {
		t.Fatal("plugin module missing pre-execute hook")
	}

	// Idempotent rewire.
	changed2, _, err := ConfigureDshRtk(exe)
	if err != nil || changed2 {
		t.Fatalf("rewire=%v %v", changed2, err)
	}

	// Unwire removes the plugin module and row but keeps the rest of the patch.
	if !RemoveDshRtk() {
		t.Fatal("unwire failed")
	}
	if _, ok := util.ReadFileSafe(dshRtkPluginFile()); ok {
		t.Fatal("plugin module survived unwire")
	}
	patchRaw2, _ := util.ReadFileSafe(dshPatchFile())
	if strings.Contains(patchRaw2, "tokless-rtk") {
		t.Fatalf("plugin row survived unwire: %s", patchRaw2)
	}
	if !strings.Contains(patchRaw2, "- id: llm-pi-ai") {
		t.Fatal("unwire dropped existing patch content")
	}
}

func TestDshRtkLegacyHookMigration(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	_ = os.MkdirAll(filepath.Dir(dshPatchFile()), 0o755)
	// Legacy wiring: bridge row + hooks.json carrying the deny hook.
	_ = util.WriteFile(dshPatchFile(), dshProxySeed+"\n- insert:\n    - id: hooks-claude-code\n      name: '@deepseek-ai/dsh-hooks-claude-code'\n      config:\n        configPath: "+yamlQuote(dshHooksFile())+"\n        pluginRoot: /usr/local/bin/tokless\n")
	_ = os.MkdirAll(filepath.Dir(dshHooksFile()), 0o755)
	_ = util.WriteFile(dshHooksFile(), `{"hooks":{"PreToolUse":[{"matcher":"bash","hooks":[{"type":"command","command":"tokless rtk-hook dsh"}]}]}}`)

	changed, _, err := ConfigureDshRtk("/usr/local/bin/tokless")
	if err != nil || !changed {
		t.Fatalf("configure=%v %v", changed, err)
	}
	if !DshRtkWired() {
		t.Fatal("plugin not wired after migration")
	}
	if _, ok := util.ReadFileSafe(dshHooksFile()); ok {
		t.Fatal("legacy hooks.json survived migration")
	}
	patchRaw, _ := util.ReadFileSafe(dshPatchFile())
	if strings.Contains(patchRaw, "dsh-hooks-claude-code") {
		t.Fatalf("legacy bridge row survived migration: %s", patchRaw)
	}
}

func TestDshRtkForeignPatchRefused(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	_ = os.MkdirAll(filepath.Dir(dshPatchFile()), 0o755)
	// Bridge row present but pointing at a foreign hooks.json.
	_ = util.WriteFile(dshPatchFile(), "- insert:\n    - id: hooks-claude-code\n      name: '@deepseek-ai/dsh-hooks-claude-code'\n      config:\n        configPath: /foreign/hooks.json\n")
	if RemoveDshBridgeRow() {
		t.Fatal("unwire removed a foreign bridge row")
	}
	raw, _ := util.ReadFileSafe(dshPatchFile())
	if !strings.Contains(raw, "/foreign/hooks.json") {
		t.Fatal("foreign bridge row lost")
	}
	if RemoveDshRtk() {
		t.Fatal("unwire removed a foreign plugin row")
	}
	if _, ok := util.ReadFileSafe(dshRtkPluginFile()); ok {
		t.Fatal("plugin module removed though never installed")
	}
}

func TestDshProxyRoundTrip(t *testing.T) {
	path := dshProxyTestHome(t)
	if err := util.WriteFile(path, dshProxySeed); err != nil {
		t.Fatal(err)
	}

	changed, file := ConfigureDshProxy()
	if !changed || file != path {
		t.Fatalf("ConfigureDshProxy = %v, %q; want true, %q", changed, file, path)
	}
	if !DshProxyWired() {
		t.Fatal("DshProxyWired = false after configure")
	}
	if got := DetectProxy("dsh").State; got != ProxyStateManaged {
		t.Fatalf("DetectProxy(dsh) = %v, want managed", got)
	}
	raw, _ := util.ReadFileSafe(path)
	for _, want := range []string{ProxyEndpointFor("dsh"), byokRouteHeaderKey, "X-Custom", dshHeadroomBaseURLHeader, dshHeadroomOriginalPathHdr, "https://api.acme.example", "/v1/chat/completions"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("configured cordis.patch.yml missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "https://api.acme.example/v1") {
		t.Fatalf("provider baseURL not rewritten:\n%s", raw)
	}

	route, ok := util.ReadBYOKRoute("dsh:gateway")
	if !ok || route.Protocol != "openai-chat" || route.Upstream != "https://api.acme.example" {
		t.Fatalf("dsh:gateway route = %+v ok=%v", route, ok)
	}

	if changed, _ := ConfigureDshProxy(); changed {
		t.Fatal("second configure should be a no-op")
	}
	if !DshProxyWired() {
		t.Fatal("DshProxyWired became false on second configure")
	}

	if !RemoveDshProxy() {
		t.Fatal("RemoveDshProxy = false")
	}
	if DshProxyWired() {
		t.Fatal("DshProxyWired = true after remove")
	}
	restored, _ := util.ReadFileSafe(path)
	for _, bad := range []string{util.BYOKGatewayEndpoint(), byokRouteHeaderKey, ProxyEndpointFor("dsh"), dshHeadroomBaseURLHeader, dshHeadroomOriginalPathHdr} {
		if strings.Contains(restored, bad) {
			t.Fatalf("residue %q after remove:\n%s", bad, restored)
		}
	}
	if !strings.Contains(restored, "https://api.acme.example/v1") {
		t.Fatalf("provider baseURL not restored:\n%s", restored)
	}
	if !strings.Contains(restored, "X-Custom: keep") {
		t.Fatalf("pre-existing header dropped:\n%s", restored)
	}
	if !strings.Contains(restored, "- id: agent-default-model") {
		t.Fatalf("sibling plugin block damaged:\n%s", restored)
	}
}

// TestDshPreservesSiblingPlugins guards that header/baseURL surgery never
// bleeds into other plugin blocks of the YAML list.
func TestDshPreservesSiblingPlugins(t *testing.T) {
	path := dshProxyTestHome(t)
	if err := util.WriteFile(path, dshProxySeed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureDshProxy(); !changed {
		t.Fatal("ConfigureDshProxy = false")
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, "- id: agent-default-model") || !strings.Contains(raw, "provider: gateway") {
		t.Fatalf("sibling plugin block damaged:\n%s", raw)
	}
}

func TestDshUnparseableBaseURLSkipped(t *testing.T) {
	path := dshProxyTestHome(t)
	seed := "- id: llm-pi-ai\n  config:\n    providers:\n      bad:\n        api: openai-completions\n        baseURL: not-a-url\n"
	if err := util.WriteFile(path, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureDshProxy(); changed {
		t.Fatal("ConfigureDshProxy changed a config with no valid upstream")
	}
}

func TestDshNoPluginBlock(t *testing.T) {
	path := dshProxyTestHome(t)
	if err := util.WriteFile(path, "- id: something-else\n  config: {}\n"); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureDshProxy(); changed {
		t.Fatal("ConfigureDshProxy changed a file with no llm-pi-ai block")
	}
	if got := DetectProxy("dsh").State; got != ProxyStateUnconfigured {
		t.Fatalf("DetectProxy(dsh) = %v, want unconfigured", got)
	}
}
