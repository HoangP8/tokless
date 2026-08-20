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
	// Real BYOK lives in config.json (OpenCode loads it first).
	cfg := `{
  "provider": {
    "mustc": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://htmustc.id.vn/v1",
        "apiKey": "mustc-secret-key"
      },
      "models": {"gpt-5.6-luna": {"name": "Luna"}}
    },
    "nvidia_nim": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://integrate.api.nvidia.com/v1",
        "apiKey": "nvapi-test-key"
      }
    }
  }
}`
	if err := util.WriteFile(filepath.Join(dir, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	// Minimal opencode.json so OpenCodePathsResolved picks a file.
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
	if strings.Contains(raw, "htmustc.id.vn") || strings.Contains(raw, "nvidia.com") {
		t.Fatalf("real hosts still in config: %s", raw)
	}
	// Secrets stay put.
	if !strings.Contains(raw, "mustc-secret-key") || !strings.Contains(raw, "nvapi-test-key") {
		t.Fatalf("keys lost: %s", raw)
	}

	// Route map: fingerprint → original host (no raw key).
	base, ok := headroom.LookupRoute("mustc-secret-key")
	if !ok || base != "https://htmustc.id.vn/v1" {
		t.Fatalf("mustc route = %q ok=%v", base, ok)
	}
	base, ok = headroom.LookupRoute("nvapi-test-key")
	if !ok || !strings.Contains(base, "nvidia.com") {
		t.Fatalf("nvidia route = %q ok=%v", base, ok)
	}
	mapRaw, _ := util.ReadFileSafe(filepath.Join(util.HeadroomPathsResolved().Root, "proxy.routes.json"))
	if strings.Contains(mapRaw, "mustc-secret") || strings.Contains(mapRaw, "nvapi-test") {
		t.Fatalf("raw key leaked into route map: %s", mapRaw)
	}

	if !RemoveOpenCodeProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(filepath.Join(dir, "config.json"))
	if !strings.Contains(raw, "htmustc.id.vn") || !strings.Contains(raw, "nvidia.com") {
		t.Fatalf("hosts not restored: %s", raw)
	}
	if OpenCodeProxyWired() {
		t.Fatal("still wired after remove")
	}
	if _, ok := headroom.LookupRoute("mustc-secret-key"); ok {
		t.Fatal("route map not cleared")
	}
}


