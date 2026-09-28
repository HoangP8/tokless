package agents

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// TestConfigureKiloProxyUsesTransportPlugin wires kilo through the plugin
// without touching user providers, reserved block, or configured model.
func TestConfigureKiloProxyUsesTransportPlugin(t *testing.T) {
	kiloProxyTestHome(t)
	dir := util.KiloPathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	configJSON := `{"provider":{"cliproxyapi":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"http://127.0.0.1:2000/v1","apiKey":"cliproxy-key"},"models":{}},"acme":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.acme.example/api/v1","apiKey":"acme-key"},"models":{}}}}`
	kiloJSONC := `{"model":"acme/pilot-x","provider":{"tokless-headroom":{"npm":"@ai-sdk/openai-compatible","name":"Headroom Proxy (BYOK)","options":{"baseURL":"http://127.0.0.1:8787/v1","apiKey":"acme-secret","headers":{"x-headroom-base-url":"https://api.acme.example/api"}},"models":{"pilot-x":{"name":"Pilot X"}}}}}`
	opencodeJSONC := `{"$schema":"https://app.kilo.ai/config.json","plugin":["@tarquinen/opencode-dcp@3.0.4","user-plugin"]}`
	if err := util.WriteFile(filepath.Join(dir, "config.json"), configJSON); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(util.KiloPathsResolved().Config, kiloJSONC); err != nil {
		t.Fatal(err)
	}
	opencodeConfig := filepath.Join(dir, "opencode.jsonc")
	if err := util.WriteFile(opencodeConfig, opencodeJSONC); err != nil {
		t.Fatal(err)
	}

	changed, file := ConfigureKiloProxy()
	if !changed {
		t.Fatal("configure reported no change")
	}
	if file != opencodeConfig {
		t.Fatalf("configure file = %q, want %q", file, opencodeConfig)
	}
	if !KiloProxyWired() {
		t.Fatal("expected transport plugin wired")
	}
	if got := DetectProxy("kilo").State; got != ProxyStateManaged {
		t.Fatalf("detect state = %v, want managed", got)
	}

	if raw, _ := util.ReadFileSafe(filepath.Join(dir, "config.json")); raw != configJSON {
		t.Fatalf("config.json must stay untouched:\n%s", raw)
	}
	if raw, _ := util.ReadFileSafe(util.KiloPathsResolved().Config); raw != kiloJSONC {
		t.Fatalf("kilo.jsonc must stay untouched:\n%s", raw)
	}
	raw, _ := util.ReadFileSafe(opencodeConfig)
	for _, want := range []string{`"@tarquinen/opencode-dcp@3.0.4"`, `"user-plugin"`, kiloTransportPluginEntry(), "tokless-byok.kilo.js"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("opencode.jsonc missing %q:\n%s", want, raw)
		}
	}
	wrapper, ok := util.ReadFileSafe(kiloTransportPluginPath())
	if !ok || !strings.Contains(wrapper, `"proxyUrl": "http://127.0.0.1:8787"`) ||
		!strings.Contains(wrapper, "kilo:acme.") || !strings.Contains(wrapper, "ToklessBYOKPlugin(input, OPTIONS)") {
		t.Fatalf("kilo wrapper missing config:\n%s", wrapper)
	}
	route, ok := util.ReadBYOKRoute("kilo:acme")
	if !ok || route.Upstream != "https://api.acme.example/api/v1" {
		t.Fatalf("acme route = %+v ok=%v", route, ok)
	}
	if route, ok := util.ReadBYOKRoute("kilo:cliproxyapi"); !ok || route.Upstream != "http://127.0.0.1:2000/v1" {
		t.Fatalf("cliproxyapi route = %+v ok=%v", route, ok)
	}

	if changed, _ := ConfigureKiloProxy(); changed {
		t.Fatal("second configure should be a no-op")
	}

	if !RemoveKiloProxy() || KiloProxyWired() {
		t.Fatal("remove failed")
	}
	if raw, _ := util.ReadFileSafe(filepath.Join(dir, "config.json")); raw != configJSON {
		t.Fatalf("config.json changed by remove:\n%s", raw)
	}
	if raw, _ := util.ReadFileSafe(util.KiloPathsResolved().Config); raw != kiloJSONC {
		t.Fatalf("kilo.jsonc changed by remove:\n%s", raw)
	}
	raw, _ = util.ReadFileSafe(opencodeConfig)
	if !strings.Contains(raw, `"@tarquinen/opencode-dcp@3.0.4"`) || !strings.Contains(raw, `"user-plugin"`) {
		t.Fatalf("user plugins lost on remove:\n%s", raw)
	}
	if strings.Contains(raw, "tokless-byok.kilo.js") {
		t.Fatalf("plugin entry left behind:\n%s", raw)
	}
	if util.Exists(kiloTransportPluginPath()) {
		t.Fatal("kilo wrapper file left behind")
	}
	if _, ok := util.ReadBYOKRoute("kilo:acme"); ok {
		t.Fatal("kilo route left behind")
	}
}
