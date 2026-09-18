package headroom

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

func TestBYOKGatewayRoutesExplicitProtocolAndRejectsUnknown(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	upstreamA := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstreamB.Close()
	pool := x509.NewCertPool()
	pool.AddCert(upstreamA.Certificate())
	pool.AddCert(upstreamB.Certificate())
	byokTransportFactory = func() http.RoundTripper {
		return &http.Transport{DialContext: (&http.Transport{}).DialContext, TLSClientConfig: &tls.Config{RootCAs: pool}}
	}
	defer func() {
		byokTransportFactory = func() http.RoundTripper { return &http.Transport{DialContext: safeDialContext, Proxy: nil} }
	}()
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "openai-a", Token: "token-a", Protocol: "openai-chat", Upstream: upstreamA.URL},
		{ID: "anthropic-b", Token: "token-b", Protocol: "anthropic-messages", Upstream: upstreamB.URL},
	}); err != nil {
		t.Fatal(err)
	}
	h := byokGatewayHandlerForTest()

	request := httptest.NewRequest(http.MethodPost, "http://gateway/v1/chat/completions", nil)
	request.Header.Set(byokRouteHeader, "openai-a.token-a")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("openai route status = %d, want %d", recorder.Code, http.StatusCreated)
	}

	request = httptest.NewRequest(http.MethodPost, "http://gateway/v1/messages", nil)
	request.Header.Set(byokRouteHeader, "anthropic-b.token-b")
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("anthropic route status = %d, want %d", recorder.Code, http.StatusAccepted)
	}

	request = httptest.NewRequest(http.MethodPost, "http://gateway/v1/chat/completions", nil)
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("missing route status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
}

func TestBYOKGatewayRejectsProtocolMismatch(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	if err := SaveBYOKRoutes([]BYOKRoute{{ID: "openai", Token: "token", Protocol: "openai-chat", Upstream: "https://api.example.com"}}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://gateway/v1/messages", nil)
	request.Header.Set(byokRouteHeader, "openai.token")
	recorder := httptest.NewRecorder()
	byokGatewayHandlerForTest().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("mismatch status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
}

func TestBYOKGatewayRejectsDuplicateRouteHeaders(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	if err := SaveBYOKRoutes([]BYOKRoute{{ID: "openai", Token: "token", Protocol: "openai-chat", Upstream: "https://api.example.com"}}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://gateway/v1/chat/completions", nil)
	request.Header.Add(byokRouteHeader, "openai.token")
	request.Header.Add(byokRouteHeader, "openai.token")
	recorder := httptest.NewRecorder()
	byokGatewayHandlerForTest().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("duplicate route status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
}

func TestBYOKGatewayEndToEndWithV1Upstream(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	var capturedPath string
	var capturedAuth string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
	byokTransportFactory = func() http.RoundTripper {
		return &http.Transport{DialContext: (&http.Transport{}).DialContext, TLSClientConfig: &tls.Config{RootCAs: pool}}
	}
	defer func() {
		byokTransportFactory = func() http.RoundTripper { return &http.Transport{DialContext: safeDialContext, Proxy: nil} }
	}()
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "custom-provider", Token: "token-custom", Protocol: "openai-chat", Upstream: upstream.URL + "/api"},
	}); err != nil {
		t.Fatal(err)
	}
	h := byokGatewayHandlerForTest()
	request := httptest.NewRequest(http.MethodPost, "http://gateway/v1/chat/completions", nil)
	request.Header.Set(byokRouteHeader, "custom-provider.token-custom")
	request.Header.Set("Authorization", "Bearer test_auth_key_123")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("gateway status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if capturedPath != "/api/v1/chat/completions" {
		t.Fatalf("upstream path = %q, want /api/v1/chat/completions", capturedPath)
	}
	if capturedAuth != "Bearer test_auth_key_123" {
		t.Fatalf("upstream auth = %q, want Bearer test_auth_key_123", capturedAuth)
	}
}

func TestByokTargetPath(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		incoming string
		want     string
	}{
		{"empty target", "", "/v1/chat/completions", "/v1/chat/completions"},
		{"slash target", "/", "/v1/chat/completions", "/v1/chat/completions"},
		{"target with /v1, incoming with /v1", "/api/v1", "/v1/chat/completions", "/api/v1/chat/completions"},
		{"target without /v1, incoming with /v1", "/api", "/v1/chat/completions", "/v1/chat/completions"},
		{"target matches incoming prefix", "/v1", "/v1/chat/completions", "/chat/completions"},
		{"target does not match incoming", "/other", "/v1/chat/completions", "/v1/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := byokTargetPath(tt.target, tt.incoming)
			if got != tt.want {
				t.Errorf("byokTargetPath(%q, %q) = %q, want %q", tt.target, tt.incoming, got, tt.want)
			}
		})
	}
}

func TestBYOKGatewayCoordinatorAuthorizationRejectsUncommittedRoute(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
	byokTransportFactory = func() http.RoundTripper {
		return &http.Transport{DialContext: (&http.Transport{}).DialContext, TLSClientConfig: &tls.Config{RootCAs: pool}}
	}
	defer func() {
		byokTransportFactory = func() http.RoundTripper { return &http.Transport{DialContext: safeDialContext, Proxy: nil} }
	}()
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:custom", Token: "test-token", Protocol: "openai-chat", Upstream: upstream.URL},
	}); err != nil {
		t.Fatal(err)
	}

	coordinator, err := util.NewRoutingCoordinator(30 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()

	h := BYOKGatewayHandlerWithCoordinator(coordinator)
	request := httptest.NewRequest(http.MethodPost, "http://gateway/v1/chat/completions", nil)
	request.Header.Set(byokRouteHeader, "opencode:custom.test-token")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("gateway status = %d, want %d (bad gateway)", recorder.Code, http.StatusBadGateway)
	}
}

func TestEndToEnd_HeadroomToBYOKGatewayToUpstream(t *testing.T) {
	headroomBin := ResolveHeadroomBin()
	if headroomBin == "" {
		t.Skip("headroom binary not found")
	}

	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	upstreamReceived := make(chan bool, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(byokRouteHeader) != "" {
			t.Errorf("upstream should NOT receive %s header", byokRouteHeader)
		}
		upstreamReceived <- true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer upstream.Close()

	fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided","code":"invalid_api_key"}}`))
	}))
	defer fallbackServer.Close()

	routeToken := "mock-token-xyz"
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:mock", Token: routeToken, Protocol: "openai-chat", Upstream: upstream.URL},
	}); err != nil {
		t.Fatal(err)
	}

	gatewayListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer gatewayListener.Close()
	gatewayPort := gatewayListener.Addr().(*net.TCPAddr).Port
	gatewayServer := &http.Server{Handler: byokGatewayHandlerForTest()}
	go func() { _ = gatewayServer.Serve(gatewayListener) }()
	defer func() { _ = gatewayServer.Close() }()

	headroomListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	headroomPort := headroomListener.Addr().(*net.TCPAddr).Port
	_ = headroomListener.Close()

	allowedURLs := "http://127.0.0.1:" + strconv.Itoa(gatewayPort) + ",http://localhost:" + strconv.Itoa(gatewayPort)
	cmd := exec.Command(headroomBin, "proxy",
		"--port", strconv.Itoa(headroomPort),
		"--no-optimize",
		"--openai-api-url", fallbackServer.URL,
	)
	cmd.Env = append(cmd.Environ(),
		"HEADROOM_ALLOWED_BASE_URLS="+allowedURLs,
		"OPENAI_TARGET_API_URL="+fallbackServer.URL,
		"HOME="+home,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start headroom proxy: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	// Wait for Headroom to become ready (up to 15s)
	headroomURL := "http://127.0.0.1:" + strconv.Itoa(headroomPort)
	ready := false
	for i := 0; i < 150; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(headroomURL + "/livez")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
	}
	if !ready {
		t.Fatalf("headroom proxy failed to become ready; stderr:\n%s", stderr.String())
	}

	gatewayURL := "http://127.0.0.1:" + strconv.Itoa(gatewayPort)
	reqBody := `{"model":"test","messages":[{"role":"user","content":"ping"}]}`
	req, err := http.NewRequest(http.MethodPost, headroomURL+"/v1/chat/completions", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-headroom-base-url", gatewayURL)
	req.Header.Set("X-Tokless-Route", "opencode:mock."+routeToken)
	req.Header.Set("Authorization", "Bearer mock-api-key")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request to headroom failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "pong") {
		t.Fatalf("expected response from mock upstream, got: %s", string(body))
	}

	select {
	case <-upstreamReceived:
	case <-time.After(time.Second):
		t.Fatal("mock upstream did not receive request")
	}

	reqBody2 := `{"model":"test","messages":[{"role":"user","content":"different-prompt-for-reproduction"}]}`
	reqWithoutBaseURL, err := http.NewRequest(http.MethodPost, headroomURL+"/v1/chat/completions", strings.NewReader(reqBody2))
	if err != nil {
		t.Fatal(err)
	}
	reqWithoutBaseURL.Header.Set("Content-Type", "application/json")
	reqWithoutBaseURL.Header.Set("X-Tokless-Route", "opencode:mock."+routeToken)
	reqWithoutBaseURL.Header.Set("Authorization", "Bearer mock-api-key")

	resp2, err := client.Do(reqWithoutBaseURL)
	if err != nil {
		t.Fatalf("request to headroom failed: %v", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)

	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 from fallback, got %d; body: %s", resp2.StatusCode, string(body2))
	}
	if !strings.Contains(string(body2), "invalid_api_key") {
		t.Fatalf("expected invalid_api_key from fallback, got: %s", string(body2))
	}

	unallowedPort := 19999
	reqBody3 := `{"model":"test","messages":[{"role":"user","content":"unallowed-loopback-url"}]}`
	reqUnallowed, err := http.NewRequest(http.MethodPost, headroomURL+"/v1/chat/completions", strings.NewReader(reqBody3))
	if err != nil {
		t.Fatal(err)
	}
	reqUnallowed.Header.Set("Content-Type", "application/json")
	reqUnallowed.Header.Set("x-headroom-base-url", "http://127.0.0.1:"+strconv.Itoa(unallowedPort))
	reqUnallowed.Header.Set("X-Tokless-Route", "opencode:mock."+routeToken)
	reqUnallowed.Header.Set("Authorization", "Bearer mock-api-key")

	resp3, err := client.Do(reqUnallowed)
	if err != nil {
		t.Fatalf("request to headroom failed: %v", err)
	}
	defer resp3.Body.Close()
	body3, _ := io.ReadAll(resp3.Body)

	if resp3.StatusCode == http.StatusOK || strings.Contains(string(body3), "pong") {
		t.Fatalf("ssrf guard check: expected unallowed loopback to be rejected, got 200 with pong: %s", string(body3))
	}
}

func TestLoadBYOKRoutesFailsOnCorruptedRegistry(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	// Pre-populate with valid routes
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "valid-route", Token: "valid-token", Protocol: "openai-chat", Upstream: "https://api.valid.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if !BYOKRoutesConfigured() {
		t.Fatalf("expected BYOK routes to be configured")
	}

	routesFile := filepath.Join(home, ".local", "share", "tokless", "headroom", "byok.routes.json")
	if err := os.WriteFile(routesFile, []byte("{invalid-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := LoadBYOKRoutes()
	if err == nil || !strings.Contains(err.Error(), "invalid BYOK route registry") {
		t.Fatalf("LoadBYOKRoutes on corrupt file want error, got: %v", err)
	}
	// Verify that in-memory routes are purged and fail closed
	if BYOKRoutesConfigured() {
		t.Fatalf("expected BYOK routes to be cleared on registry corruption")
	}
	if _, ok := lookupBYOKRoute("valid-route"); ok {
		t.Fatalf("expected lookup of previously loaded route to fail after corruption")
	}
}

func TestStartBYOKGatewayFailsClosedOnCorruptedRegistry(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	routesFile := filepath.Join(home, ".local", "share", "tokless", "headroom", "byok.routes.json")
	if err := os.MkdirAll(filepath.Dir(routesFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routesFile, []byte("{invalid-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := StartBYOKGateway()
	if err == nil || !strings.Contains(err.Error(), "invalid BYOK route registry") {
		t.Fatalf("StartBYOKGateway on corrupt registry want error, got: %v", err)
	}
}

func TestBYOKGatewayCoordinatorAuthorizationFailsClosed(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	upstreamHit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:test", Token: "token-123", Protocol: "openai-chat", Upstream: upstream.URL},
	}); err != nil {
		t.Fatal(err)
	}

	coordinator, err := util.NewRoutingCoordinator(30 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Close/fence the coordinator so authorization fails
	_ = coordinator.Close()

	handler := byokGatewayHandlerWithCoordinatorForTest(coordinator)
	req := httptest.NewRequest(http.MethodPost, "http://gateway/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set(byokRouteHeader, "opencode:test.token-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d (bad gateway)", rec.Code, http.StatusBadGateway)
	}
	if upstreamHit {
		t.Fatalf("fenced coordinator request reached upstream server — failed to fail closed")
	}
}

func TestBYOKGatewayVirtualPathPrefixRouting(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	upstreamHit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream received path %q, want /v1/chat/completions", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer upstream.Close()

	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:custom-prov", Token: "tok-abc", Protocol: "openai-chat", Upstream: upstream.URL},
	}); err != nil {
		t.Fatal(err)
	}

	handler := byokGatewayHandlerForTest()

	// 1. /byok/custom-prov/v1/chat/completions without headers succeeds
	upstreamHit = false
	req := httptest.NewRequest(http.MethodPost, "http://gateway/byok/custom-prov/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !upstreamHit {
		t.Fatal("upstream not reached via /byok/ prefix")
	}

	// 2. /tokless/byok/custom-prov/v1/chat/completions without headers succeeds
	upstreamHit = false
	req = httptest.NewRequest(http.MethodPost, "http://gateway/tokless/byok/custom-prov/v1/chat/completions", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !upstreamHit {
		t.Fatal("upstream not reached via /tokless/byok/ prefix")
	}

	// 3. /byok/custom-prov/v1/chat/completions with matching X-Tokless-Route succeeds
	upstreamHit = false
	req = httptest.NewRequest(http.MethodPost, "http://gateway/byok/custom-prov/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set(byokRouteHeader, "opencode:custom-prov.tok-abc")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	// 4. /byok/custom-prov/v1/chat/completions with MISMATCHING route token fails closed
	req = httptest.NewRequest(http.MethodPost, "http://gateway/byok/custom-prov/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set(byokRouteHeader, "opencode:custom-prov.wrong-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 on mismatched header token", rec.Code)
	}

	// 5. Traversal attempt fails closed
	req = httptest.NewRequest(http.MethodPost, "http://gateway/byok/../etc/passwd", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 on path traversal", rec.Code)
	}

	// 6. Unknown provider fails closed
	req = httptest.NewRequest(http.MethodPost, "http://gateway/byok/unregistered-prov/v1/chat/completions", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 on unknown provider", rec.Code)
	}

	// 7. Duplicate X-Tokless-Route headers rejected
	req = httptest.NewRequest(http.MethodPost, "http://gateway/byok/custom-prov/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Add(byokRouteHeader, "opencode:custom-prov.tok-abc")
	req.Header.Add(byokRouteHeader, "opencode:custom-prov.tok-abc")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 on duplicate route headers", rec.Code)
	}
}

func TestBYOKGatewayFailsClosedOnDeletedRegistry(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:deleteme", Token: "tok-123", Protocol: "openai-chat", Upstream: "https://api.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if !BYOKRoutesConfigured() {
		t.Fatal("expected routes configured")
	}

	// Delete the routes file
	routesFile := filepath.Join(home, ".local", "share", "tokless", "headroom", "byok.routes.json")
	if err := os.Remove(routesFile); err != nil {
		t.Fatal(err)
	}

	// Reload to detect deletion
	if err := LoadBYOKRoutes(); err != nil {
		t.Fatalf("LoadBYOKRoutes on missing file returned error: %v", err)
	}
	if BYOKRoutesConfigured() {
		t.Fatal("expected routes unconfigured after file deletion")
	}
	if _, ok := lookupBYOKRoute("opencode:deleteme"); ok {
		t.Fatal("expected route lookup to fail closed after file deletion")
	}
}

func TestBYOKGatewayConcurrentReloadAndLookup(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:prov-0", Token: "tok-0", Protocol: "openai-chat", Upstream: "https://api.example.com"},
	}); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Reader goroutines
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = lookupBYOKRoute("opencode:prov-0")
					_ = BYOKRoutesConfigured()
				}
			}
		}()
	}

	// Writer / Reloader goroutines
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				_ = LoadBYOKRoutes()
			}
		}(i)
	}

	// Wait for writers to complete
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Ensure route table remains consistent
	if !BYOKRoutesConfigured() {
		t.Fatal("expected route table consistent after concurrent reloads")
	}
}

func TestBYOKGatewaySameSizeAtomicReplacementReloadsHash(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	// Route 1 with 8-byte token
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:prov-a", Token: "11112222", Protocol: "openai-chat", Upstream: "https://api.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := LoadBYOKRoutes(); err != nil {
		t.Fatal(err)
	}
	r1, ok := lookupBYOKRoute("opencode:prov-a")
	if !ok || r1.Token != "11112222" {
		t.Fatalf("expected token 11112222, got %v", r1.Token)
	}

	// Replace atomically with same byte length, different token
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:prov-a", Token: "33334444", Protocol: "openai-chat", Upstream: "https://api.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := LoadBYOKRoutes(); err != nil {
		t.Fatal(err)
	}
	r2, ok := lookupBYOKRoute("opencode:prov-a")
	if !ok || r2.Token != "33334444" {
		t.Fatalf("expected token 33334444 after replacement, got %v", r2.Token)
	}
}

func TestBYOKGatewayLivezStatus(t *testing.T) {
	h := byokGatewayHandlerForTest()
	req := httptest.NewRequest(http.MethodGet, "http://gateway/livez", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	// Trigger error
	badErr := "simulated corrupt registry"
	byokReloadErr.Store(&badErr)
	t.Cleanup(func() { byokReloadErr.Store(nil) })

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "degraded") {
		t.Fatalf("expected degraded status in response body, got %s", rec2.Body.String())
	}
}

func TestBYOKWatcherLiveRevocation(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	// Initial valid route
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:live-test", Token: "init-tok", Protocol: "openai-chat", Upstream: "https://api.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := LoadBYOKRoutes(); err != nil {
		t.Fatal(err)
	}

	stopWatcher := make(chan struct{})
	doneWatcher := make(chan struct{})
	go func() {
		runBYOKWatcher(byokRoutesPath(), stopWatcher, 10*time.Millisecond)
		close(doneWatcher)
	}()

	// Verify route exists
	r, ok := lookupBYOKRoute("opencode:live-test")
	if !ok || r.Token != "init-tok" {
		t.Fatalf("initial route lookup failed: %v, %v", ok, r)
	}

	// 1. Same-size atomic replacement (new token, same length: 8 chars)
	if err := SaveBYOKRoutes([]BYOKRoute{
		{ID: "opencode:live-test", Token: "next-tok", Protocol: "openai-chat", Upstream: "https://api.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	// Wait for watcher to detect hash change (<=100ms)
	var updated bool
	for i := 0; i < 10; i++ {
		time.Sleep(10 * time.Millisecond)
		if cur, exists := lookupBYOKRoute("opencode:live-test"); exists && cur.Token == "next-tok" {
			updated = true
			break
		}
	}
	if !updated {
		t.Fatal("watcher failed to detect same-size atomic token replacement within 100ms")
	}

	// 2. Corrupt file -> fails closed immediately and marks reload error (<=100ms)
	if err := os.WriteFile(byokRoutesPath(), []byte("not-json{{{"), 0o600); err != nil {
		t.Fatal(err)
	}
	var failedClosed bool
	for i := 0; i < 10; i++ {
		time.Sleep(10 * time.Millisecond)
		if _, exists := lookupBYOKRoute("opencode:live-test"); !exists {
			failedClosed = true
			break
		}
	}
	if !failedClosed {
		t.Fatal("watcher failed to fail closed on corrupt file within 100ms")
	}
	if byokReloadErr.Load() == nil {
		t.Fatal("expected byokReloadErr to be set after corruption")
	}

	// 3. Delete file -> fails closed and preserves degradation error
	_ = os.Remove(byokRoutesPath())
	var deletionFailedClosed bool
	for i := 0; i < 10; i++ {
		time.Sleep(10 * time.Millisecond)
		if _, exists := lookupBYOKRoute("opencode:live-test"); !exists {
			deletionFailedClosed = true
			break
		}
	}
	if !deletionFailedClosed {
		t.Fatal("expected route lookup to fail closed after deletion within 100ms")
	}
	if byokReloadErr.Load() == nil || !strings.Contains(*byokReloadErr.Load(), "not found") {
		t.Fatalf("expected degradation error mentioning not found, got %v", byokReloadErr.Load())
	}
	close(stopWatcher)
	<-doneWatcher

	// 4. Watcher started when registry is already missing clears any existing routes immediately
	activeBYOKRoutes.Store(&byokRouteTable{
		items: map[string]BYOKRoute{"stale": {ID: "stale"}},
	})
	stopWatcher2 := make(chan struct{})
	doneWatcher2 := make(chan struct{})
	go func() {
		runBYOKWatcher(byokRoutesPath(), stopWatcher2, 10*time.Millisecond)
		close(doneWatcher2)
	}()
	time.Sleep(20 * time.Millisecond)
	if _, exists := lookupBYOKRoute("stale"); exists {
		t.Fatal("watcher started on missing registry must immediately clear pre-existing stale routes")
	}
	if byokReloadErr.Load() == nil || !strings.Contains(*byokReloadErr.Load(), "not found") {
		t.Fatalf("expected degradation error on startup when missing, got %v", byokReloadErr.Load())
	}
	close(stopWatcher2)
	<-doneWatcher2
}
