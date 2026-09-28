package util

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type RoutingCoordinator struct {
	nonce    string
	owner    string
	leaseTTL time.Duration
	mu       sync.RWMutex
	leases   map[string]RoutingLease
	snapshot atomic.Value
	active   atomic.Bool
	ctx      context.Context
	cancel   context.CancelFunc
	closeMu  sync.Mutex
	closed   bool
}

func NewRoutingCoordinator(ttl time.Duration) (*RoutingCoordinator, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("coordinator lease TTL must be positive")
	}
	state, err := ReadRoutingState()
	if err != nil {
		return nil, err
	}
	nonce := state.CoordinatorNonce
	if nonce == "" {
		nonce, err = NewRoutingToken()
		if err != nil {
			return nil, err
		}
	}
	owner, err := NewRoutingToken()
	if err != nil {
		return nil, err
	}
	snapshot, err := NewRoutingSnapshot(state)
	if err != nil {
		return nil, err
	}
	coordinator := &RoutingCoordinator{nonce: nonce, owner: owner, leaseTTL: ttl, leases: make(map[string]RoutingLease)}
	if err := claimRoutingCoordinator(nonce, owner, ttl); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	coordinator.snapshot.Store(snapshot)
	coordinator.ctx, coordinator.cancel = ctx, cancel
	coordinator.active.Store(true)
	go coordinator.heartbeat()
	return coordinator, nil
}

func (coordinator *RoutingCoordinator) Nonce() string { return coordinator.nonce }

func (coordinator *RoutingCoordinator) Close() error {
	coordinator.closeMu.Lock()
	defer coordinator.closeMu.Unlock()
	coordinator.mu.Lock()
	coordinator.active.Store(false)
	coordinator.cancel()
	leases := make([]RoutingLease, 0, len(coordinator.leases))
	for _, lease := range coordinator.leases {
		leases = append(leases, lease)
	}
	coordinator.mu.Unlock()
	leaseErr := releaseRoutingLeases(leases, coordinator.nonce, coordinator.owner)
	coordinatorErr := releaseRoutingCoordinator(coordinator.nonce, coordinator.owner)
	coordinator.mu.Lock()
	coordinator.leases = make(map[string]RoutingLease)
	coordinator.closed = true
	coordinator.mu.Unlock()
	return errors.Join(leaseErr, coordinatorErr)
}

func (coordinator *RoutingCoordinator) heartbeat() {
	interval := coordinator.leaseTTL / 3
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			coordinator.closeMu.Lock()
			if !coordinator.active.Load() {
				coordinator.closeMu.Unlock()
				return
			}
			coordinator.mu.RLock()
			leases := make([]RoutingLease, 0, len(coordinator.leases))
			for _, lease := range coordinator.leases {
				leases = append(leases, lease)
			}
			coordinator.mu.RUnlock()
			renewed, err := renewRoutingOwnership(coordinator.nonce, coordinator.owner, coordinator.leaseTTL, leases)
			if err != nil {
				coordinator.closeMu.Unlock()
				if coordinator.ownershipTakenOver() {
					coordinator.fence()
					return
				}
				continue
			}
			coordinator.mu.Lock()
			for _, lease := range renewed {
				coordinator.leases[lease.LaneID] = lease
			}
			coordinator.mu.Unlock()
			coordinator.closeMu.Unlock()
		case <-coordinator.ctx.Done():
			return
		}
	}
}

// ownershipTakenOver reports whether another coordinator or owner now holds
// the on-disk claim, i.e. this coordinator must permanently stop renewing.
func (coordinator *RoutingCoordinator) ownershipTakenOver() bool {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return false
	}
	defer release()
	claim, err := readRoutingOwnershipUnlocked()
	if err != nil {
		return false
	}
	if claim.CoordinatorNonce != coordinator.nonce {
		return true
	}
	if claim.OwnerNonce != coordinator.owner {
		return true
	}
	return false
}

func (coordinator *RoutingCoordinator) fence() {
	coordinator.active.Store(false)
	coordinator.cancel()
}

func (coordinator *RoutingCoordinator) Publish(state RoutingState) error {
	coordinator.closeMu.Lock()
	defer coordinator.closeMu.Unlock()
	if !coordinator.active.Load() {
		return fmt.Errorf("coordinator is fenced")
	}
	snapshot, err := NewRoutingSnapshot(state)
	if err != nil {
		return err
	}
	if state.CoordinatorNonce != coordinator.nonce {
		return fmt.Errorf("routing state coordinator mismatch")
	}
	if err := publishRoutingState(state, coordinator.nonce, coordinator.owner); err != nil {
		return err
	}
	coordinator.mu.Lock()
	for laneID, lease := range coordinator.leases {
		if lease.Generation != state.Generation {
			delete(coordinator.leases, laneID)
		}
	}
	coordinator.mu.Unlock()
	coordinator.snapshot.Store(snapshot)
	return nil
}

func (coordinator *RoutingCoordinator) Current() (RoutingSnapshot, bool) {
	value := coordinator.snapshot.Load()
	if value == nil {
		return RoutingSnapshot{}, false
	}
	return value.(RoutingSnapshot), true
}

func (coordinator *RoutingCoordinator) Authorize(selection RouteSelection) (context.Context, error) {
	if !coordinator.active.Load() {
		return nil, fmt.Errorf("coordinator is fenced")
	}
	snapshot, ok := coordinator.Current()
	if !ok || selection.Generation != snapshot.Generation() {
		return nil, fmt.Errorf("routing lease is stale")
	}
	coordinator.mu.RLock()
	lease, owned := coordinator.leases[selection.LaneID]
	coordinator.mu.RUnlock()
	if !owned || lease.CoordinatorNonce != coordinator.nonce || lease.Generation != selection.Generation ||
		lease.Listener != selection.Listener || lease.LaneInstanceNonce != selection.LaneInstanceNonce ||
		lease.LeaseNonce != selection.LeaseNonce || !leaseLive(lease, time.Now()) || !leaseMatchesState(lease, snapshot.state) {
		return nil, fmt.Errorf("routing lease is unavailable")
	}
	if !coordinator.active.Load() {
		return nil, fmt.Errorf("coordinator is fenced")
	}
	return coordinator.ctx, nil
}

func (coordinator *RoutingCoordinator) AuthorizeRequest(request RouteRequest) (context.Context, error) {
	_, ctx, err := coordinator.ResolveAndAuthorize(request)
	return ctx, err
}

func (coordinator *RoutingCoordinator) ResolveAndAuthorize(request RouteRequest) (RouteSelection, context.Context, error) {
	snapshot, ok := coordinator.Current()
	if !ok {
		return RouteSelection{}, nil, fmt.Errorf("routing snapshot unavailable")
	}
	selection, err := snapshot.ResolveRequest(request, func(selection RouteSelection) bool {
		coordinator.mu.RLock()
		lease, owned := coordinator.leases[selection.LaneID]
		coordinator.mu.RUnlock()
		return owned && lease.CoordinatorNonce == coordinator.nonce && lease.Generation == selection.Generation && lease.Listener == selection.Listener && leaseLive(lease, time.Now())
	})
	if err != nil {
		return RouteSelection{}, nil, err
	}
	coordinator.mu.RLock()
	lease := coordinator.leases[selection.LaneID]
	coordinator.mu.RUnlock()
	selection.LeaseNonce = lease.LeaseNonce
	ctx, err := coordinator.Authorize(selection)
	return selection, ctx, err
}

func (coordinator *RoutingCoordinator) Acquire(state RoutingState, lane LaneBinding) (RoutingLease, error) {
	coordinator.closeMu.Lock()
	defer coordinator.closeMu.Unlock()
	if !coordinator.active.Load() {
		return RoutingLease{}, fmt.Errorf("coordinator is fenced")
	}
	if state.CoordinatorNonce != coordinator.nonce {
		return RoutingLease{}, fmt.Errorf("routing state coordinator mismatch")
	}
	lease, err := createRoutingLease(state, lane, coordinator.leaseTTL, coordinator.nonce, coordinator.owner)
	if err != nil {
		return RoutingLease{}, err
	}
	coordinator.mu.Lock()
	if current, exists := coordinator.leases[lease.LaneID]; !exists || leaseExpiresAfter(lease, current) {
		coordinator.leases[lease.LaneID] = lease
	}
	coordinator.mu.Unlock()
	return lease, nil
}

func (coordinator *RoutingCoordinator) Renew(lease RoutingLease) (RoutingLease, error) {
	coordinator.closeMu.Lock()
	defer coordinator.closeMu.Unlock()
	if !coordinator.active.Load() {
		return RoutingLease{}, fmt.Errorf("coordinator is fenced")
	}
	coordinator.mu.RLock()
	owned, ok := coordinator.leases[lease.LaneID]
	if !ok || !sameRoutingLease(owned, lease) || lease.CoordinatorNonce != coordinator.nonce {
		coordinator.mu.RUnlock()
		return RoutingLease{}, fmt.Errorf("coordinator does not own lease")
	}
	coordinator.mu.RUnlock()
	renewed, err := renewRoutingOwnership(coordinator.nonce, coordinator.owner, coordinator.leaseTTL, []RoutingLease{lease})
	if err != nil {
		return RoutingLease{}, err
	}
	for _, current := range renewed {
		if current.LaneID == lease.LaneID {
			coordinator.mu.Lock()
			if existing, exists := coordinator.leases[lease.LaneID]; !exists || leaseExpiresAfter(current, existing) {
				coordinator.leases[lease.LaneID] = current
			}
			coordinator.mu.Unlock()
			return current, nil
		}
	}
	return RoutingLease{}, fmt.Errorf("routing lease is unavailable")
}

func leaseExpiresAfter(a, b RoutingLease) bool {
	aExpiry, aErr := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	bExpiry, bErr := time.Parse(time.RFC3339Nano, b.ExpiresAt)
	return aErr == nil && bErr == nil && aExpiry.After(bExpiry)
}

func (coordinator *RoutingCoordinator) Release(lease RoutingLease) error {
	coordinator.closeMu.Lock()
	defer coordinator.closeMu.Unlock()
	if !coordinator.active.Load() {
		return fmt.Errorf("coordinator is fenced")
	}
	coordinator.mu.RLock()
	owned, ok := coordinator.leases[lease.LaneID]
	if !ok || !sameRoutingLease(owned, lease) || lease.CoordinatorNonce != coordinator.nonce {
		coordinator.mu.RUnlock()
		return fmt.Errorf("coordinator does not own lease")
	}
	coordinator.mu.RUnlock()
	err := releaseRoutingLease(lease, coordinator.nonce, coordinator.owner)
	if err == nil {
		coordinator.mu.Lock()
		delete(coordinator.leases, lease.LaneID)
		coordinator.mu.Unlock()
	}
	return err
}
