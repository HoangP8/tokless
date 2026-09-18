package util

import (
	"testing"
	"time"
)

func committedRoutingState() RoutingState {
	return RoutingState{
		Version: RoutingStateVersion, Generation: 7, State: RoutingStateCommitted,
		CoordinatorNonce: "coordinator", CommitNonce: "commit",
		Lanes:  []LaneBinding{{ID: "lane", InstanceNonce: "instance", ProviderInstanceID: "provider", Protocol: "openai-chat", UpstreamID: "upstream", Listener: "127.0.0.1:38787", Port: 38787, Generation: 7, CoordinatorNonce: "coordinator", Ready: true}},
		Routes: []RouteBinding{{ID: "route", Token: testRoutingToken, ProfileID: "profile", SurfaceID: "codex", ProviderInstanceID: "provider", Protocol: "openai-chat", LaneID: "lane", LaneInstanceNonce: "instance", CoordinatorNonce: "coordinator", UpstreamID: "upstream", ConfigFingerprint: "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", Generation: 7}},
	}
}

func TestResolveRouteUsesOnlyBoundReadyLane(t *testing.T) {
	snapshot, err := NewRoutingSnapshot(committedRoutingState())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := snapshot.ResolveRequest(RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "POST", Path: "/v1/chat/completions"}, func(selection RouteSelection) bool {
		return selection.Listener == "127.0.0.1:38787"
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Listener != "127.0.0.1:38787" || selection.LaneID != "lane" || selection.LaneInstanceNonce != "instance" || selection.CoordinatorNonce != "coordinator" || selection.UpstreamID != "upstream" || selection.Generation != 7 {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestResolveRouteRejectsInvalidWithoutFallback(t *testing.T) {
	state := committedRoutingState()
	snapshot, err := NewRoutingSnapshot(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{"", "route", "route.bad", "other." + testRoutingToken} {
		if _, err := snapshot.ResolveRequest(RouteRequest{Credential: credential, Protocol: "openai-chat", Method: "POST", Path: "/v1/chat/completions"}, func(RouteSelection) bool { return true }); err == nil {
			t.Fatalf("credential accepted: %q", credential)
		}
	}
	if _, err := snapshot.ResolveRequest(RouteRequest{Credential: "route." + testRoutingToken, Protocol: "anthropic-messages", Method: "POST", Path: "/v1/messages"}, func(RouteSelection) bool { return true }); err == nil {
		t.Fatal("protocol mismatch accepted")
	}
}

func TestResolveRouteRejectsUnreadyOrWrongLane(t *testing.T) {
	state := committedRoutingState()
	state.Lanes[0].Ready = false
	if _, err := NewRoutingSnapshot(state); err == nil {
		t.Fatal("unready committed lane published")
	}
	state = committedRoutingState()
	state.Routes[0].LaneInstanceNonce = "other-instance"
	if _, err := NewRoutingSnapshot(state); err == nil {
		t.Fatal("wrong lane instance published")
	}
}

func TestRoutingSnapshotValidatesMethodAndPath(t *testing.T) {
	snapshot, err := NewRoutingSnapshot(committedRoutingState())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.ResolveRequest(RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "GET", Path: "/v1/chat/completions"}, func(RouteSelection) bool { return true }); err == nil {
		t.Fatal("GET route accepted")
	}
	for _, path := range []string{"/v1/responses", "/v1/chat/completions/", "/v1/chat%2Fcompletions"} {
		if _, err := snapshot.ResolveRequest(RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "POST", Path: path}, func(RouteSelection) bool { return true }); err == nil {
			t.Fatalf("wrong protocol path accepted: %q", path)
		}
	}
	selection, err := snapshot.ResolveRequest(RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "POST", Path: "/v1/chat/completions"}, func(RouteSelection) bool { return true })
	if err != nil || selection.Listener != "127.0.0.1:38787" {
		t.Fatalf("valid request = %+v, err = %v", selection, err)
	}
}

func TestRoutingSnapshotProtocolPathMatrix(t *testing.T) {
	for _, test := range []struct {
		protocol string
		path     string
	}{
		{"openai-chat", "/v1/chat/completions"},
		{"openai-chat", "/chat/completions"},
		{"openai-responses", "/v1/responses"},
		{"openai-responses", "/responses"},
		{"anthropic-messages", "/v1/messages"},
		{"anthropic-messages", "/messages"},
	} {
		state := committedRoutingState()
		state.Routes[0].Protocol = test.protocol
		state.Lanes[0].Protocol = test.protocol
		if test.protocol == "openai-responses" {
			state.Routes[0].UpstreamID = "upstream-responses"
			state.Lanes[0].UpstreamID = "upstream-responses"
		}
		if test.protocol == "anthropic-messages" {
			state.Routes[0].UpstreamID = "upstream-anthropic"
			state.Lanes[0].UpstreamID = "upstream-anthropic"
		}
		snapshot, err := NewRoutingSnapshot(state)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := snapshot.ResolveRequest(RouteRequest{Credential: "route." + testRoutingToken, Protocol: test.protocol, Method: "POST", Path: test.path}, func(RouteSelection) bool { return true }); err != nil {
			t.Fatalf("%s %s rejected: %v", test.protocol, test.path, err)
		}
	}
}

func TestRoutingSnapshotRejectsNonCanonicalPathAndMissingOwnership(t *testing.T) {
	snapshot, err := NewRoutingSnapshot(committedRoutingState())
	if err != nil {
		t.Fatal(err)
	}
	base := RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "POST", Path: "/v1/chat/completions"}
	for _, request := range []RouteRequest{
		{Credential: base.Credential, Protocol: base.Protocol, Method: base.Method, Path: base.Path, RawPath: base.Path},
		{Credential: base.Credential, Protocol: base.Protocol, Method: base.Method, Path: base.Path, RawQuery: "x=1"},
		{Credential: base.Credential, Protocol: base.Protocol, Method: base.Method, Path: "/v1/chat%2Fcompletions"},
	} {
		if _, err := snapshot.ResolveRequest(request, func(RouteSelection) bool { return true }); err == nil {
			t.Fatalf("non-canonical request accepted: %+v", request)
		}
	}
	if _, err := snapshot.ResolveRequest(base, nil); err == nil {
		t.Fatal("missing ownership accepted")
	}
	if _, err := snapshot.ResolveRequest(base, func(RouteSelection) bool { return false }); err == nil {
		t.Fatal("unowned listener accepted")
	}
}

func TestRoutingSnapshotRequiresLiveMatchingLease(t *testing.T) {
	state := committedRoutingState()
	snapshot, err := NewRoutingSnapshot(state)
	if err != nil {
		t.Fatal(err)
	}
	lease := RoutingLease{
		Version: RoutingStateVersion, CoordinatorNonce: state.CoordinatorNonce, Generation: state.Generation,
		LaneID: state.Lanes[0].ID, LaneInstanceNonce: state.Lanes[0].InstanceNonce, Listener: state.Lanes[0].Listener,
		LeaseNonce: testRoutingToken, ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano),
	}
	request := RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "POST", Path: "/v1/chat/completions"}
	selection, err := snapshot.ResolveRequestWithLease(request, lease, func(selection RouteSelection) bool {
		return selection.LeaseNonce == lease.LeaseNonce
	})
	if err != nil || selection.LeaseNonce != lease.LeaseNonce {
		t.Fatalf("selection = %+v, err = %v", selection, err)
	}
	lease.Generation++
	if _, err := snapshot.ResolveRequestWithLease(request, lease, func(RouteSelection) bool { return true }); err == nil {
		t.Fatal("stale lease accepted")
	}
	lease = RoutingLease{
		Version: RoutingStateVersion, CoordinatorNonce: state.CoordinatorNonce, Generation: state.Generation,
		LaneID: state.Lanes[0].ID, LaneInstanceNonce: state.Lanes[0].InstanceNonce, Listener: state.Lanes[0].Listener,
		LeaseNonce: testRoutingToken, ExpiresAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}
	if _, err := snapshot.ResolveRequestWithLease(request, lease, func(RouteSelection) bool { return true }); err == nil {
		t.Fatal("expired lease accepted")
	}
}
