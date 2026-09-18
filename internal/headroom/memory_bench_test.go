package headroom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func BenchmarkRouteParts(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _, _ = routeParts("opencode:provider.route-token-abcdefghijklmnopqrstuvwxyz")
	}
}

func BenchmarkPureRouteLookup(b *testing.B) {
	util.SetHomeOverride(b.TempDir())
	if err := SaveBYOKRoutes([]BYOKRoute{{ID: "opencode:test-provider", Token: "token-xyz", Protocol: "openai-chat", Upstream: "http://127.0.0.1:1"}}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lookupBYOKRoute("opencode:test-provider")
	}
}

func BenchmarkBufferPool(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := byokBufferPool.Get()
		byokBufferPool.Put(buf)
	}
}

func BenchmarkBYOKTargetPath(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = byokTargetPath("/api/v1", "/v1/chat/completions")
	}
}

func BenchmarkBYOKGatewayHandlerRouteLookup(b *testing.B) {
	util.SetHomeOverride(b.TempDir())
	if err := SaveBYOKRoutes([]BYOKRoute{{ID: "route", Token: "token", Protocol: "openai-chat", Upstream: "http://127.0.0.1:1"}}); err != nil {
		b.Fatal(err)
	}
	h := byokGatewayHandlerForTest()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://gateway/v1/chat/completions", nil)
		req.Header.Set(byokRouteHeader, "route.token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkBYOKGatewayProxyReuse(b *testing.B) {
	util.SetHomeOverride(b.TempDir())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer upstream.Close()
	if err := SaveBYOKRoutes([]BYOKRoute{{ID: "route", Token: "token", Protocol: "openai-chat", Upstream: upstream.URL}}); err != nil {
		b.Fatal(err)
	}
	h := byokGatewayHandlerForTest()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://gateway/v1/chat/completions", strings.NewReader("{}"))
		req.Header.Set(byokRouteHeader, "route.token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}
