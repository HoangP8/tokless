package util

import "testing"

const testRoutingToken = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

func TestValidateRoutingStateRequiresExactRouteLaneBinding(t *testing.T) {
	state := RoutingState{
		Version:          RoutingStateVersion,
		Generation:       4,
		State:            RoutingStateCommitted,
		CoordinatorNonce: "coordinator-1",
		CommitNonce:      "commit-1",
		Lanes: []LaneBinding{{
			ID: "lane-1", InstanceNonce: "instance-1", ProviderInstanceID: "provider-1",
			Protocol: "openai-chat", UpstreamID: "upstream-1", Listener: "127.0.0.1:38787", Port: 38787,
			Generation: 4, CoordinatorNonce: "coordinator-1", Ready: true,
		}},
		Routes: []RouteBinding{{
			ID: "route-1", Token: testRoutingToken, ProfileID: "profile-1", SurfaceID: "codex",
			ProviderInstanceID: "provider-1", Protocol: "openai-chat", LaneID: "lane-1", LaneInstanceNonce: "instance-1", CoordinatorNonce: "coordinator-1", UpstreamID: "upstream-1",
			ConfigFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Generation: 4,
		}},
	}
	if err := ValidateRoutingState(state); err != nil {
		t.Fatal(err)
	}
	state.Routes[0].LaneID = "lane-2"
	if err := ValidateRoutingState(state); err == nil {
		t.Fatal("route bound to missing lane was accepted")
	}
}

func TestValidateRoutingStateRejectsCommittedRevokedRoute(t *testing.T) {
	state := RoutingState{
		Version: RoutingStateVersion, Generation: 1, State: RoutingStateCommitted, CoordinatorNonce: "coordinator", CommitNonce: "commit",
		Lanes:  []LaneBinding{{ID: "lane", InstanceNonce: "instance", ProviderInstanceID: "provider", Protocol: "anthropic-messages", UpstreamID: "upstream", Listener: "127.0.0.1:38788", Port: 38788, Generation: 1, CoordinatorNonce: "coordinator", Ready: true}},
		Routes: []RouteBinding{{ID: "route", Token: testRoutingToken, ProfileID: "profile", SurfaceID: "claude", ProviderInstanceID: "provider", Protocol: "anthropic-messages", LaneID: "lane", LaneInstanceNonce: "instance", CoordinatorNonce: "coordinator", UpstreamID: "upstream", ConfigFingerprint: "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", Generation: 1, Revoked: true, RevokedGeneration: 1}},
	}
	if err := ValidateRoutingState(state); err == nil {
		t.Fatal("committed revoked route was accepted")
	}
}

func TestValidRoutingTokenRequiresGeneratedBase64URL(t *testing.T) {
	if !ValidRoutingToken(testRoutingToken) {
		t.Fatal("generated token rejected")
	}
	for _, token := range []string{"token", "", testRoutingToken + "x", "..........................................."} {
		if ValidRoutingToken(token) {
			t.Fatalf("invalid token accepted: %q", token)
		}
	}
}

func TestValidateRoutingStateAcceptsAbsentAndRejectsEmptyCommitted(t *testing.T) {
	if err := ValidateRoutingState(RoutingState{State: RoutingStateAbsent}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRoutingState(RoutingState{Version: RoutingStateVersion, Generation: 1, State: RoutingStateCommitted, CoordinatorNonce: "coordinator", CommitNonce: "commit"}); err == nil {
		t.Fatal("empty committed state accepted")
	}
}

func TestValidLoopbackListenerSupportsIPv4AndIPv6(t *testing.T) {
	if !validLoopbackListener("127.0.0.1:1234", 1234) || !validLoopbackListener("[::1]:1234", 1234) {
		t.Fatal("canonical loopback listener rejected")
	}
	if validLoopbackListener("0.0.0.0:1234", 1234) || validLoopbackListener("127.0.0.1:1235", 1234) {
		t.Fatal("invalid loopback listener accepted")
	}
}

func TestValidateRoutingStateTransitionRejectsCommittedBootstrap(t *testing.T) {
	previous := RoutingState{State: RoutingStateAbsent}
	next := RoutingState{Version: RoutingStateVersion, Generation: 1, State: RoutingStateCommitted, CoordinatorNonce: "coordinator", CommitNonce: "commit"}
	if err := ValidateRoutingStateTransition(previous, next); err == nil {
		t.Fatal("committed state bootstrapped from absent state")
	}
}

func TestValidateRoutingStateTransitionRequiresMonotonicGeneration(t *testing.T) {
	previous := RoutingState{Version: RoutingStateVersion, Generation: 1, State: RoutingStatePreparing, CoordinatorNonce: "coordinator"}
	next := previous
	next.Generation = 1
	if err := ValidateRoutingStateTransition(previous, next); err == nil {
		t.Fatal("unchanged generation accepted")
	}
}

func TestParseRoutingStateRejectsUnknownFields(t *testing.T) {
	if _, err := ParseRoutingState(`{"state":"absent","unexpected":true}`); err == nil {
		t.Fatal("unknown routing state field accepted")
	}
}

func TestParseRoutingStateRejectsDuplicateKeys(t *testing.T) {
	if _, err := ParseRoutingState(`{"state":"absent","state":"absent"}`); err == nil {
		t.Fatal("duplicate routing state key accepted")
	}
}
