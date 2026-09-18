package util

import (
	"testing"
	"time"
)

func BenchmarkRoutingSnapshotResolve(b *testing.B) {
	state := committedRoutingState()
	snapshot, err := NewRoutingSnapshot(state)
	if err != nil {
		b.Fatal(err)
	}
	request := RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "POST", Path: "/v1/chat/completions"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = snapshot.ResolveRequest(request, func(RouteSelection) bool { return true })
	}
}

func BenchmarkRoutingCoordinatorResolveAndAuthorize(b *testing.B) {
	SetHomeOverride(b.TempDir())
	state := committedRoutingState()
	state.PreviousGeneration = state.Generation - 1
	if err := publishCommittedForCoordinatorTest(state); err != nil {
		b.Fatal(err)
	}
	coordinator, err := NewRoutingCoordinator(time.Minute)
	if err != nil {
		b.Fatal(err)
	}
	defer coordinator.Close()
	if _, err := coordinator.Acquire(state, state.Lanes[0]); err != nil {
		b.Fatal(err)
	}
	request := RouteRequest{Credential: "route." + testRoutingToken, Protocol: "openai-chat", Method: "POST", Path: "/v1/chat/completions"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = coordinator.ResolveAndAuthorize(request)
	}
}
