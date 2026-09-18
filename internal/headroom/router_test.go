package headroom

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

func TestRouterCoordinatorContextPreservesClientCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "http://router/v1/chat/completions", nil).WithContext(ctx)
	coordinatorContext, coordinatorCancel := context.WithCancel(context.Background())
	defer coordinatorCancel()
	merged, mergedCancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(coordinatorContext, mergedCancel)
	defer func() { stop(); mergedCancel() }()
	cancel()
	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("client cancellation did not reach merged request context")
	}
}

func TestRouterRejectsInvalidRequestsWithoutBackendContact(t *testing.T) {
	state := util.RoutingState{
		Version: util.RoutingStateVersion, Generation: 1, State: util.RoutingStateCommitted, CoordinatorNonce: "coordinator", CommitNonce: "commit",
		Lanes:  []util.LaneBinding{{ID: "lane", InstanceNonce: "instance", ProviderInstanceID: "provider", Protocol: "openai-chat", UpstreamID: "upstream", Listener: "127.0.0.1:38787", Port: 38787, Generation: 1, CoordinatorNonce: "coordinator", Ready: true}},
		Routes: []util.RouteBinding{{ID: "route", Token: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", ProfileID: "profile", SurfaceID: "codex", ProviderInstanceID: "provider", Protocol: "openai-chat", LaneID: "lane", LaneInstanceNonce: "instance", CoordinatorNonce: "coordinator", UpstreamID: "upstream", ConfigFingerprint: "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", Generation: 1}},
	}
	snapshot, err := util.NewRoutingSnapshot(state)
	if err != nil {
		t.Fatal(err)
	}
	var contacts atomic.Int32
	router := NewRouter(snapshot, func(util.RouteSelection) bool {
		contacts.Add(1)
		return false
	})
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "http://router/v1/chat/completions", nil),
		httptest.NewRequest(http.MethodPost, "http://router/v1/chat/completions?x=1", nil),
		httptest.NewRequest(http.MethodPost, "http://router/v1/chat%2Fcompletions", nil),
	} {
		request.Header.Set(routerRouteHeader, "unknown."+state.Routes[0].Token)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", recorder.Code)
		}
	}
	if contacts.Load() != 0 {
		t.Fatal("unowned requests reached ownership callback")
	}
}

func TestProtocolForPath(t *testing.T) {
	for _, test := range []struct {
		path     string
		protocol string
		ok       bool
	}{
		{"/v1/chat/completions", "openai-chat", true},
		{"/v1/responses", "openai-responses", true},
		{"/v1/messages", "anthropic-messages", true},
		{"/v1/chat/completions/", "", false},
		{"/other", "", false},
	} {
		protocol, ok := util.RouteProtocolForPath(test.path)
		if protocol != test.protocol || ok != test.ok {
			t.Fatalf("protocolForPath(%q) = %q, %t", test.path, protocol, ok)
		}
	}
}

func TestRouterForwardsOnlySelectedRouteAndStripsCredential(t *testing.T) {
	var gotHeader, gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotHeader = request.Header.Get(routerRouteHeader)
		gotPath = request.URL.Path
		w.WriteHeader(http.StatusCreated)
	}))
	defer backend.Close()
	parsed, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	state := util.RoutingState{
		Version: util.RoutingStateVersion, Generation: 1, State: util.RoutingStateCommitted, CoordinatorNonce: "coordinator", CommitNonce: "commit",
		Lanes:  []util.LaneBinding{{ID: "lane", InstanceNonce: "instance", ProviderInstanceID: "provider", Protocol: "openai-chat", UpstreamID: "upstream", Listener: host + ":" + port, Port: mustRouterPort(t, port), Generation: 1, CoordinatorNonce: "coordinator", Ready: true}},
		Routes: []util.RouteBinding{{ID: "route", Token: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", ProfileID: "profile", SurfaceID: "codex", ProviderInstanceID: "provider", Protocol: "openai-chat", LaneID: "lane", LaneInstanceNonce: "instance", CoordinatorNonce: "coordinator", UpstreamID: "upstream", ConfigFingerprint: "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", Generation: 1}},
	}
	snapshot, err := util.NewRoutingSnapshot(state)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(snapshot, func(selection util.RouteSelection) bool { return selection.Listener == state.Lanes[0].Listener })
	request := httptest.NewRequest(http.MethodPost, "http://router/v1/chat/completions", nil)
	request.Header.Set(routerRouteHeader, "route."+state.Routes[0].Token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || gotPath != "/v1/chat/completions" || gotHeader != "" {
		t.Fatalf("status=%d path=%q route-header=%q", recorder.Code, gotPath, gotHeader)
	}
}

func mustRouterPort(t *testing.T, raw string) int {
	t.Helper()
	port, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatal(err)
	}
	return port
}
