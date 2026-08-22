package agents

import (
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
	raw, _ := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if !strings.Contains(raw, `"plugin"`) || !strings.Contains(raw, `"proxyUrl": "http://127.0.0.1:8787"`) {
		t.Fatalf("transport plugin missing: %s", raw)
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
	if !changed || len(routes) != 3 {
		t.Fatalf("wire changed=%v routes=%d", changed, len(routes))
	}
	raw, _ := util.ReadFileSafe(path)
	if strings.Count(raw, `"baseURL": "http://127.0.0.1:8787/v1"`) != 3 {
		t.Fatalf("not all wired:\n%s", raw)
	}
	if !unwireOpenCodeBYOK() {
		t.Fatal("unwire no change")
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
	// second wire/unwire must stay stable
	if _, routes = wireOpenCodeBYOK(); len(routes) != 3 {
		t.Fatalf("rewire routes=%d", len(routes))
	}
	if !unwireOpenCodeBYOK() {
		t.Fatal("second unwire failed")
	}
	raw2, _ := util.ReadFileSafe(path)
	if raw2 != raw {
		t.Fatalf("second round-trip drifted\n--- first ---\n%s\n--- second ---\n%s", raw, raw2)
	}
}

func TestUnwireOpenCodeBYOKDoesNotOverwriteUserEdit(t *testing.T) {
	opencodeProxyTestHome(t)
	dir := util.OpenCodePathsResolved().Dir
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := util.WriteFile(path, `{"provider":{"prov-a":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://api.provider-a.test/v1","apiKey":"key-a"},"models":{}}}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := wireOpenCodeBYOK(); !changed {
		t.Fatal("wire no change")
	}
	if err := util.WriteFile(path, `{"provider":{"prov-a":{"options":{"baseURL":"https://user-edited.example/v1"}}}}`); err != nil {
		t.Fatal(err)
	}
	if unwireOpenCodeBYOK() {
		t.Fatal("unwire should ignore user-edited provider")
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, "https://user-edited.example/v1") {
		t.Fatalf("user edit overwritten: %s", raw)
	}
}
