package util

import (
	"os"
	"sync"
	"testing"
	"time"
)

func TestRoutingCoordinatorRecoversCommittedSnapshot(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := coordinator.Current()
	if !ok {
		t.Fatal("missing recovered snapshot")
	}
	selection, err := snapshot.ResolveRequest(RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "POST", Path: "/v1/chat/completions"}, func(RouteSelection) bool { return true })
	if err != nil || selection.Generation != state.Generation {
		t.Fatalf("selection = %+v, err = %v", selection, err)
	}
}

func TestRoutingCoordinatorOwnsMultipleLanesAndExactLease(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	secondToken := "AgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fICE"
	state.Lanes = append(state.Lanes, LaneBinding{ID: "lane-2", InstanceNonce: "instance-2", ProviderInstanceID: "provider-2", Protocol: "openai-responses", UpstreamID: "upstream-2", Listener: "127.0.0.1:38789", Port: 38789, Generation: state.Generation, CoordinatorNonce: state.CoordinatorNonce, Ready: true})
	state.Routes = append(state.Routes, RouteBinding{ID: "route-2", Token: secondToken, ProfileID: "profile-2", SurfaceID: "opencode", ProviderInstanceID: "provider-2", Protocol: "openai-responses", LaneID: "lane-2", LaneInstanceNonce: "instance-2", CoordinatorNonce: state.CoordinatorNonce, UpstreamID: "upstream-2", ConfigFingerprint: "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabce", Generation: state.Generation})
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := coordinator.Acquire(state, state.Lanes[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.Acquire(state, state.Lanes[1])
	if err != nil {
		t.Fatal(err)
	}
	if first.LaneID == second.LaneID {
		t.Fatal("multiple lanes shared lease identity")
	}
	forged := first
	forged.ExpiresAt = time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	if _, err := coordinator.Renew(forged); err == nil {
		t.Fatal("forged coordinator lease renewed")
	}
	if err := coordinator.Release(first); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Release(second); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingCoordinatorCloseAllowsImmediateRestart(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	first, err := NewRoutingCoordinator(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Acquire(state, state.Lanes[0]); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewRoutingCoordinator(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.Acquire(state, state.Lanes[0]); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingCoordinatorHeartbeatRenewsEmptyLeaseSet(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(30 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	time.Sleep(50 * time.Millisecond)
	if !routingCoordinatorOwned(coordinator.nonce, coordinator.owner) {
		t.Fatal("empty lease set stopped coordinator heartbeat")
	}
}

func TestRoutingCoordinatorCloseRetriesTransientCleanup(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RoutingLeasePath(), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err == nil {
		t.Fatal("Close ignored transient ownership failure")
	}
	ownership := routingOwnershipFile{Version: RoutingStateVersion, CoordinatorNonce: coordinator.nonce, OwnerNonce: coordinator.owner, ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)}
	if err := writeRoutingOwnershipUnlocked(ownership); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingCoordinatorFencesAfterOwnershipTakeover(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(30 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	lease, err := coordinator.Acquire(state, state.Lanes[0])
	if err != nil {
		t.Fatal(err)
	}
	routingStoreMu.Lock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		routingStoreMu.Unlock()
		t.Fatal(err)
	}
	claim, err := readRoutingOwnershipUnlocked()
	if err != nil {
		release()
		routingStoreMu.Unlock()
		t.Fatal(err)
	}
	claim.OwnerNonce, err = NewRoutingToken()
	if err != nil {
		release()
		routingStoreMu.Unlock()
		t.Fatal(err)
	}
	claim.ExpiresAt = time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	claim.Leases[0].ExpiresAt = claim.ExpiresAt
	err = writeRoutingOwnershipUnlocked(claim)
	release()
	routingStoreMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for coordinator.active.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if coordinator.active.Load() {
		t.Fatal("coordinator did not fence after ownership takeover")
	}
	select {
	case <-coordinator.ctx.Done():
	default:
		t.Fatal("fencing did not cancel dispatch context")
	}
	_ = lease
}

func TestRoutingCoordinatorConcurrentAuthorizeAndClose(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Acquire(state, state.Lanes[0]); err != nil {
		t.Fatal(err)
	}
	selection, err := resolveRoute(state, "route."+testRoutingToken, "openai-chat")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = coordinator.Authorize(selection)
		}()
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if _, err := coordinator.Authorize(selection); err == nil {
		t.Fatal("authorization succeeded after Close")
	}
}

func TestRoutingCoordinatorCloseIsSafeDuringHeartbeat(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(50 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Acquire(state, state.Lanes[0]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	if routingCoordinatorOwned(coordinator.nonce, coordinator.owner) {
		t.Fatal("coordinator claim survived repeated Close")
	}
}

func TestRoutingCoordinatorRejectsLeaseAfterPublishedGenerationChanges(t *testing.T) {
	SetHomeOverride(t.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	lease, err := coordinator.Acquire(state, state.Lanes[0])
	if err != nil {
		t.Fatal(err)
	}
	next := state
	next.Generation++
	next.PreviousGeneration = state.Generation
	next.State = RoutingStatePreparing
	next.CommitNonce = ""
	next.Lanes = append([]LaneBinding(nil), state.Lanes...)
	next.Lanes[0].Generation = next.Generation
	next.Lanes[0].Ready = false
	next.Routes = append([]RouteBinding(nil), state.Routes...)
	next.Routes[0].Generation = next.Generation
	next.Routes[0].Revoked = true
	next.Routes[0].RevokedGeneration = next.Generation
	if err := coordinator.Publish(next); err != nil {
		t.Fatal(err)
	}
	selection := RouteSelection{LaneID: lease.LaneID, LaneInstanceNonce: lease.LaneInstanceNonce, CoordinatorNonce: lease.CoordinatorNonce, Listener: lease.Listener, Generation: lease.Generation, LeaseNonce: lease.LeaseNonce}
	if _, err := coordinator.Authorize(selection); err == nil {
		t.Fatal("stale lease authorized after generation publication")
	}
}

func publishCommittedForCoordinatorTest(state RoutingState) error {
	preparing := RoutingState{Version: RoutingStateVersion, Generation: state.Generation - 1, State: RoutingStatePreparing, CoordinatorNonce: state.CoordinatorNonce}
	if err := PublishRoutingState(preparing); err != nil {
		return err
	}
	return PublishRoutingState(state)
}
