package agents

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

func TestDiscoverAndWireOpenCodeBYOK(t *testing.T) {
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

	found := DiscoverOpenCodeBYOK()
	if len(found) != 2 {
		t.Fatalf("discover = %d %+v", len(found), found)
	}

	changed, routes := wireOpenCodeBYOK()
	if !changed {
		t.Fatal("wire reported no change")
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %d", len(routes))
	}
	if !OpenCodeProxyWired() {
		t.Fatal("expected BYOK wired")
	}

	raw, _ := util.ReadFileSafe(filepath.Join(dir, "config.json"))
	if !strings.Contains(raw, `"baseURL": "http://127.0.0.1:8787/v1"`) {
		t.Fatalf("baseURL not rewritten: %s", raw)
	}
	if strings.Contains(raw, "provider-a.test") || strings.Contains(raw, "provider-b.test") {
		t.Fatalf("real hosts still in config: %s", raw)
	}
	if !strings.Contains(raw, "prov-a-test-key") || !strings.Contains(raw, "prov-b-test-key") {
		t.Fatalf("keys lost: %s", raw)
	}

	base, ok := headroom.LookupRoute("prov-a-test-key")
	if !ok || base != "https://api.provider-a.test/v1" {
		t.Fatalf("prov-a route = %q ok=%v", base, ok)
	}
	base, ok = headroom.LookupRoute("prov-b-test-key")
	if !ok || base != "https://api.provider-b.test/v1" {
		t.Fatalf("prov-b route = %q ok=%v", base, ok)
	}
	mapRaw, _ := util.ReadFileSafe(filepath.Join(util.HeadroomPathsResolved().Root, "proxy.routes.json"))
	if strings.Contains(mapRaw, "prov-a-test") || strings.Contains(mapRaw, "prov-b-test") {
		t.Fatalf("raw key leaked into route map: %s", mapRaw)
	}

	if !RemoveOpenCodeProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(filepath.Join(dir, "config.json"))
	if !strings.Contains(raw, "provider-a.test") || !strings.Contains(raw, "provider-b.test") {
		t.Fatalf("hosts not restored: %s", raw)
	}
	if OpenCodeProxyWired() {
		t.Fatal("still wired after remove")
	}
	if _, ok := headroom.LookupRoute("prov-a-test-key"); ok {
		t.Fatal("route map not cleared")
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
