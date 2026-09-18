package util

import (
	"os"
	"testing"
	"time"
)

func TestRoutingStorePublishesOnlyMonotonicValidatedState(t *testing.T) {
	SetHomeOverride(t.TempDir())
	if state, err := ReadRoutingState(); err != nil || state.State != RoutingStateAbsent {
		t.Fatalf("initial state = %+v, err = %v", state, err)
	}
	preparing := RoutingState{Version: RoutingStateVersion, Generation: 1, State: RoutingStatePreparing, CoordinatorNonce: "coordinator"}
	if err := PublishRoutingState(preparing); err != nil {
		t.Fatal(err)
	}
	if err := PublishRoutingState(preparing); err == nil {
		t.Fatal("stale state publication accepted")
	}
	if _, err := ParseRoutingState(mustReadRoutingFile(t, RoutingStatePath())); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingLeaseRequiresExactCommittedLaneAndOwnership(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.Generation = 2
	state.PreviousGeneration = 1
	state.Lanes[0].Generation = 2
	state.Routes[0].Generation = 2
	if err := PublishRoutingState(RoutingState{Version: RoutingStateVersion, Generation: 1, State: RoutingStatePreparing, CoordinatorNonce: state.CoordinatorNonce}); err != nil {
		t.Fatal(err)
	}
	if err := PublishRoutingState(state); err != nil {
		t.Fatal(err)
	}
	lease, err := CreateRoutingLease(state, state.Lanes[0], time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateRoutingLease(state, state.Lanes[0], time.Minute); err == nil {
		t.Fatal("duplicate live lease accepted")
	}
	for _, forged := range []LaneBinding{
		func() LaneBinding {
			lane := state.Lanes[0]
			lane.Listener = "127.0.0.1:18788"
			lane.Port = 18788
			return lane
		}(),
		func() LaneBinding { lane := state.Lanes[0]; lane.InstanceNonce = "forged"; return lane }(),
	} {
		if _, err := CreateRoutingLease(state, forged, time.Minute); err == nil {
			t.Fatal("forged lane lease accepted")
		}
	}
	if _, err := RenewRoutingLease(RoutingLease{LeaseNonce: "wrong"}, time.Minute); err == nil {
		t.Fatal("foreign lease renewal accepted")
	}
	if err := ReleaseRoutingLease(RoutingLease{LeaseNonce: "wrong"}); err == nil {
		t.Fatal("foreign lease release accepted")
	}
	if err := ReleaseRoutingLease(lease); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRoutingLease(); !os.IsNotExist(err) {
		t.Fatalf("lease after release = %v", err)
	}
}

func TestRoutingLeaseRejectsMalformedPersistedLease(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.Generation = 2
	state.PreviousGeneration = 1
	state.Lanes[0].Generation = 2
	state.Routes[0].Generation = 2
	if err := PublishRoutingState(RoutingState{Version: RoutingStateVersion, Generation: 1, State: RoutingStatePreparing, CoordinatorNonce: state.CoordinatorNonce}); err != nil {
		t.Fatal(err)
	}
	if err := PublishRoutingState(state); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(RoutingLeasePath(), `{"version":1,"version":1}`, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateRoutingLease(state, state.Lanes[0], time.Minute); err == nil {
		t.Fatal("malformed persisted lease was overwritten")
	}
}

func TestRoutingLeaseCannotRenewAfterGenerationChanges(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.Generation = 2
	state.PreviousGeneration = 1
	state.Lanes[0].Generation = 2
	state.Routes[0].Generation = 2
	if err := PublishRoutingState(RoutingState{Version: RoutingStateVersion, Generation: 1, State: RoutingStatePreparing, CoordinatorNonce: state.CoordinatorNonce}); err != nil {
		t.Fatal(err)
	}
	if err := PublishRoutingState(state); err != nil {
		t.Fatal(err)
	}
	lease, err := CreateRoutingLease(state, state.Lanes[0], time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	next := state
	next.Generation = 3
	next.PreviousGeneration = 2
	next.State = RoutingStatePreparing
	next.CommitNonce = ""
	next.Lanes = append([]LaneBinding(nil), state.Lanes...)
	next.Lanes[0].Generation = 3
	next.Lanes[0].Ready = false
	next.Routes = append([]RouteBinding(nil), state.Routes...)
	next.Routes[0].Generation = 3
	next.Routes[0].Revoked = true
	next.Routes[0].RevokedGeneration = 3
	if err := PublishRoutingState(next); err != nil {
		t.Fatal(err)
	}
	if _, err := RenewRoutingLease(lease, time.Minute); err == nil {
		t.Fatal("old-generation lease renewed")
	}
}

func TestRoutingLeaseRejectsExpiredLease(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.Generation = 2
	state.PreviousGeneration = 1
	state.Lanes[0].Generation = 2
	state.Routes[0].Generation = 2
	if err := PublishRoutingState(RoutingState{Version: RoutingStateVersion, Generation: 1, State: RoutingStatePreparing, CoordinatorNonce: state.CoordinatorNonce}); err != nil {
		t.Fatal(err)
	}
	if err := PublishRoutingState(state); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateRoutingLease(state, state.Lanes[0], -time.Second); err == nil {
		t.Fatal("non-positive lease TTL accepted")
	}
}

func mustReadRoutingFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
