package util

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHeadroomPathsEnvAndSpawnAreToklessOwned(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	p := HeadroomPathsResolved()
	root := filepath.Join(home, ".local", "share", "tokless", "headroom")
	for _, path := range []string{p.Root, p.UV, p.Tools, p.Bin, p.Python, HeadroomBin()} {
		if !strings.HasPrefix(path, root) {
			t.Fatalf("path outside Tokless root: %q", path)
		}
	}
	for _, env := range HeadroomEnv() {
		if !strings.Contains(env, root) && env != "UV_MANAGED_PYTHON=1" {
			t.Fatalf("environment outside Tokless root: %q", env)
		}
	}
}

func TestHeadroomProxyRuntimePersistsAndRoundTrips(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "")
	if got := HeadroomProxyPort(); got != 8787 {
		t.Fatalf("default port = %d, want 8787", got)
	}
	st := ProxyRuntime{Port: 19787, AnthropicURL: "https://api.provider-a.test", OpenAIURL: "https://api.openai.com", GeminiURL: "https://gemini.provider-a.test", CloudCodeURL: "https://cloudcode.provider-a.test"}
	if err := SaveHeadroomProxyRuntime(st); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadProxyRuntime()
	if !ok {
		t.Fatal("persisted runtime not readable")
	}
	if got != st {
		t.Fatalf("runtime round-trip = %+v, want %+v", got, st)
	}
	if port := HeadroomProxyPort(); port != 19787 {
		t.Fatalf("port after persist = %d, want 19787", port)
	}
	if err := ClearHeadroomProxyRuntime(); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadProxyRuntime(); ok {
		t.Fatal("runtime still present after clear")
	}
	if port := HeadroomProxyPort(); port != 8787 {
		t.Fatalf("port after clear = %d, want default 8787", port)
	}
}

func TestHeadroomProxyRuntimeEnvOverridesPersisted(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	if err := SaveHeadroomProxyRuntime(ProxyRuntime{Port: 19787}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "9123")
	if port := HeadroomProxyPort(); port != 9123 {
		t.Fatalf("env should override persisted state, got %d", port)
	}
}

func TestHeadroomProxyRoutingPreferenceDefaultsOnAndPersists(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	if !ProxyRoutingEnabled() || ProxyRoutingPreferenceSet() {
		t.Fatal("routing preference must default to enabled and unset")
	}
	if err := SetProxyRoutingEnabled(false); err != nil {
		t.Fatal(err)
	}
	if ProxyRoutingEnabled() || !ProxyRoutingPreferenceSet() {
		t.Fatal("disabled routing preference was not persisted")
	}
	if err := SetProxyRoutingEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !ProxyRoutingEnabled() {
		t.Fatal("enabled routing preference was not persisted")
	}
}

func TestHeadroomProxyRoutingPreferenceRejectsMalformedState(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	path := filepath.Join(HeadroomPathsResolved().Root, "proxy.preference")
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"", "true", "enabled\nextra", "disabled\nextra"} {
		if err := WriteFile(path, state); err != nil {
			t.Fatal(err)
		}
		if ProxyRoutingEnabled() {
			t.Fatalf("malformed preference %q enabled routing", state)
		}
	}
}

func TestReadProxyRuntimeRejectsInvalidPorts(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	path := filepath.Join(HeadroomPathsResolved().Root, "proxy.runtime.json")
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{0, -1, 65536} {
		if err := WriteFile(path, `{"port":`+strconv.Itoa(port)+`}`); err != nil {
			t.Fatal(err)
		}
		if _, ok := ReadProxyRuntime(); ok {
			t.Fatalf("invalid port %d accepted", port)
		}
	}
}

func TestSaveProxyRuntimeRejectsInvalidPorts(t *testing.T) {
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })
	for _, port := range []int{0, -1, 65536} {
		if err := SaveHeadroomProxyRuntime(ProxyRuntime{Port: port}); err == nil {
			t.Fatalf("invalid port %d accepted", port)
		}
	}
}
