package headroom

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// freeProxyPortPair picks a free TCP port and reserves the next one too so
// route = headroom+1 does not collide with a live user daemon on 8787/8788.
func freeProxyPortPair(t *testing.T) int {
	t.Helper()
	for i := 0; i < 32; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if port >= 65535 {
			_ = ln.Close()
			continue
		}
		ln2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+1))
		if err != nil {
			_ = ln.Close()
			continue
		}
		_ = ln2.Close()
		_ = ln.Close()
		return port
	}
	t.Fatal("no free proxy port pair")
	return 0
}

func TestJoinUpstream(t *testing.T) {
	cases := []struct {
		base, path, want string
	}{
		{"https://api.xkiro.com/v1", "/v1/chat/completions", "https://api.xkiro.com/v1/chat/completions"},
		{"https://api.ai-box.vn/v1", "/v1/messages", "https://api.ai-box.vn/v1/messages"},
		{"https://api.qwencoder.cloud/api/v1", "/v1/chat/completions", "https://api.qwencoder.cloud/api/v1/chat/completions"},
		{"https://example.com", "/v1/chat/completions", "https://example.com/v1/chat/completions"},
	}
	for _, c := range cases {
		got, err := joinUpstream(c.base, c.path, "")
		if err != nil || got != c.want {
			t.Fatalf("join(%q,%q)=%q err=%v want %q", c.base, c.path, got, err, c.want)
		}
	}
}

func TestKeyFingerprintStable(t *testing.T) {
	a := KeyFingerprint("sk-test")
	b := KeyFingerprint("sk-test")
	if a == "" || a != b || a == "sk-test" {
		t.Fatalf("fp = %q", a)
	}
}

func TestRouteProxyForwardsByKey(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	// Avoid live user 8788: StartRouteProxy reuses any tokless-route on the port.
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", fmt.Sprint(freeProxyPortPair(t)))
	routeProxyInline = true
	t.Cleanup(func() {
		_ = StopRouteProxy()
		routeProxyInline = false
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "bad path "+r.URL.Path, 400)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.Contains(auth, "route-key-1") {
			http.Error(w, "bad auth", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	if err := SaveRouteMap([]RouteEntry{{
		KeyFP:   KeyFingerprint("route-key-1"),
		BaseURL: upstream.URL + "/v1",
		ID:      "test",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := StartRouteProxy(); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("POST", RouteProxyURL()+"/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer route-key-1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestProxyUpstreamURLsUsesRouteWhenMapped(t *testing.T) {
	isolateProxyOps(t)
	proxyTestBin(t)
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "https://api.ai-box.vn")
	t.Setenv("TOKLESS_HEADROOM_OPENAI_URL", "https://api.xkiro.com/v1")
	_ = SaveRouteMap([]RouteEntry{{KeyFP: KeyFingerprint("k"), BaseURL: "https://host/v1"}})
	a, o := ProxyUpstreamURLs()
	if a != RouteProxyURL() || o != RouteProxyURL() {
		t.Fatalf("with routes: %q %q want both %q", a, o, RouteProxyURL())
	}
	ra, ro := realUpstreamURLs()
	if ra != "https://api.ai-box.vn" || ro != "https://api.xkiro.com/v1" {
		t.Fatalf("real = %q %q", ra, ro)
	}
	_ = ClearRouteMap()
	a, o = ProxyUpstreamURLs()
	if a != "https://api.ai-box.vn" || o != "https://api.xkiro.com/v1" {
		t.Fatalf("without routes: %q %q", a, o)
	}
}
