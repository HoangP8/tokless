package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	routingStateFile         = "routing.state.json"
	routingOwnershipPathName = "routing.ownership.json"
	routingLockFile          = "routing.lock"
)

var routingStoreMu sync.Mutex

type RoutingLease struct {
	Version           int    `json:"version"`
	CoordinatorNonce  string `json:"coordinator_nonce"`
	Generation        uint64 `json:"generation"`
	LaneID            string `json:"lane_id"`
	LaneInstanceNonce string `json:"lane_instance_nonce"`
	Listener          string `json:"listener"`
	LeaseNonce        string `json:"lease_nonce"`
	ExpiresAt         string `json:"expires_at"`
}

type routingOwnershipFile struct {
	Version          int            `json:"version"`
	CoordinatorNonce string         `json:"coordinator_nonce"`
	OwnerNonce       string         `json:"owner_nonce"`
	ExpiresAt        string         `json:"expires_at"`
	Leases           []RoutingLease `json:"leases"`
}

func RoutingStatePath() string { return filepath.Join(ToklessDataDir(), routingStateFile) }

func RoutingLeasePath() string { return routingOwnershipPath() }

func routingOwnershipPath() string {
	return filepath.Join(ToklessDataDir(), routingOwnershipPathName)
}

func claimRoutingCoordinator(nonce, owner string, ttl time.Duration) error {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return err
	}
	defer release()
	current, err := readRoutingOwnershipUnlocked()
	if err == nil && coordinatorLive(current, time.Now()) {
		return fmt.Errorf("routing coordinator is already owned")
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read coordinator claim: %w", err)
	}
	return writeRoutingOwnershipUnlocked(routingOwnershipFile{Version: RoutingStateVersion, CoordinatorNonce: nonce, OwnerNonce: owner, ExpiresAt: time.Now().UTC().Add(ttl).Format(time.RFC3339Nano)})
}

func readRoutingOwnershipUnlocked() (routingOwnershipFile, error) {
	raw, err := os.ReadFile(routingOwnershipPath())
	if err != nil {
		return routingOwnershipFile{}, err
	}
	var claim routingOwnershipFile
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := validateJSONKeys(d); err != nil {
		return routingOwnershipFile{}, err
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&claim); err != nil || claim.Version != RoutingStateVersion || (claim.CoordinatorNonce != "" && !ValidRoutingID(claim.CoordinatorNonce)) || (claim.OwnerNonce != "" && !ValidRoutingToken(claim.OwnerNonce)) {
		return routingOwnershipFile{}, fmt.Errorf("invalid routing ownership")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return routingOwnershipFile{}, fmt.Errorf("trailing routing ownership data")
	}
	if _, err := time.Parse(time.RFC3339Nano, claim.ExpiresAt); err != nil {
		return routingOwnershipFile{}, err
	}
	for _, lease := range claim.Leases {
		if err := validateRoutingLease(lease); err != nil {
			return routingOwnershipFile{}, err
		}
		if lease.CoordinatorNonce != claim.CoordinatorNonce {
			return routingOwnershipFile{}, fmt.Errorf("routing lease coordinator mismatch")
		}
	}
	seenLanes := make(map[string]struct{}, len(claim.Leases))
	seenNonces := make(map[string]struct{}, len(claim.Leases))
	for _, lease := range claim.Leases {
		if _, ok := seenLanes[lease.LaneID]; ok {
			return routingOwnershipFile{}, fmt.Errorf("duplicate routing lease lane")
		}
		if _, ok := seenNonces[lease.LeaseNonce]; ok {
			return routingOwnershipFile{}, fmt.Errorf("duplicate routing lease nonce")
		}
		seenLanes[lease.LaneID] = struct{}{}
		seenNonces[lease.LeaseNonce] = struct{}{}
	}
	return claim, nil
}

func writeRoutingOwnershipUnlocked(ownership routingOwnershipFile) error {
	b, err := json.Marshal(ownership)
	if err != nil {
		return err
	}
	return WriteFileAtomic(routingOwnershipPath(), string(b), 0o600)
}

func coordinatorLive(claim routingOwnershipFile, now time.Time) bool {
	expires, err := time.Parse(time.RFC3339Nano, claim.ExpiresAt)
	return err == nil && expires.After(now)
}

func renewRoutingCoordinator(nonce, owner string, ttl time.Duration) error {
	_, err := renewRoutingOwnership(nonce, owner, ttl, nil)
	return err
}

func renewRoutingOwnership(nonce, owner string, ttl time.Duration, expected []RoutingLease) ([]RoutingLease, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("coordinator lease TTL must be positive")
	}
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return nil, err
	}
	defer release()
	claim, err := readRoutingOwnershipUnlocked()
	if err != nil || claim.CoordinatorNonce != nonce || claim.OwnerNonce != owner {
		return nil, fmt.Errorf("coordinator claim is not owned")
	}
	if expected != nil {
		byNonce := make(map[string]RoutingLease, len(claim.Leases))
		for _, lease := range claim.Leases {
			byNonce[lease.LeaseNonce] = lease
		}
		for _, lease := range expected {
			persisted, ok := byNonce[lease.LeaseNonce]
			if !ok || !sameRoutingLease(persisted, lease) {
				return nil, fmt.Errorf("routing lease is not owned")
			}
		}
	}
	claim.ExpiresAt = time.Now().UTC().Add(ttl).Format(time.RFC3339Nano)
	expiresAt := claim.ExpiresAt
	for i := range claim.Leases {
		claim.Leases[i].ExpiresAt = expiresAt
	}
	if err := writeRoutingOwnershipUnlocked(claim); err != nil {
		return nil, err
	}
	return claim.Leases, nil
}

func releaseRoutingCoordinator(nonce, owner string) error {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return err
	}
	defer release()
	claim, err := readRoutingOwnershipUnlocked()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if claim.CoordinatorNonce != nonce || claim.OwnerNonce != owner {
		return fmt.Errorf("coordinator claim is owned by another process")
	}
	if err := os.Remove(routingOwnershipPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func routingCoordinatorOwned(nonce, owner string) bool {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return false
	}
	defer release()
	claim, err := readRoutingOwnershipUnlocked()
	return err == nil && claim.CoordinatorNonce == nonce && claim.OwnerNonce == owner && coordinatorLive(claim, time.Now())
}

func coordinatorClaimMatchesUnlocked(nonce, owner string) bool {
	claim, err := readRoutingOwnershipUnlocked()
	return err == nil && claim.CoordinatorNonce == nonce && claim.OwnerNonce == owner && coordinatorLive(claim, time.Now())
}

func authorizeRoutingDispatchForLane(selection RouteSelection, coordinatorNonce, ownerNonce string) (RoutingLease, error) {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return RoutingLease{}, err
	}
	defer release()
	ownership, err := readRoutingOwnershipUnlocked()
	if err != nil || ownership.CoordinatorNonce != coordinatorNonce || ownership.OwnerNonce != ownerNonce || !coordinatorLive(ownership, time.Now()) {
		return RoutingLease{}, fmt.Errorf("coordinator claim is not owned")
	}
	state, err := readRoutingStateUnlocked()
	if err != nil || state.State != RoutingStateCommitted {
		return RoutingLease{}, fmt.Errorf("routing state is not committed")
	}
	for _, lease := range ownership.Leases {
		if lease.LaneID != selection.LaneID {
			continue
		}
		selection.LeaseNonce = lease.LeaseNonce
		if leaseLive(lease, time.Now()) && leaseMatchesState(lease, state) && sameLeaseSelection(lease, selection) {
			return lease, nil
		}
		return RoutingLease{}, fmt.Errorf("routing lease is unavailable")
	}
	return RoutingLease{}, fmt.Errorf("routing lease is unavailable")
}

func ReadRoutingState() (RoutingState, error) {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	raw, err := os.ReadFile(RoutingStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return RoutingState{State: RoutingStateAbsent}, nil
		}
		return RoutingState{}, err
	}
	return ParseRoutingState(string(raw))
}

// PublishRoutingState atomically publishes a validated next generation. It
// refuses stale state and never overwrites a state owned by another nonce.
func PublishRoutingState(next RoutingState) error {
	return publishRoutingState(next, "", "")
}

func publishRoutingState(next RoutingState, coordinatorNonce, ownerNonce string) error {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return err
	}
	defer release()
	if coordinatorNonce != "" && !coordinatorClaimMatchesUnlocked(coordinatorNonce, ownerNonce) {
		return fmt.Errorf("coordinator claim is not owned")
	}
	previous, err := readRoutingStateUnlocked()
	if err != nil {
		return err
	}
	if err := ValidateRoutingStateTransition(previous, next); err != nil {
		return err
	}
	content, err := EncodeRoutingState(next)
	if err != nil {
		return err
	}
	return WriteFileAtomic(RoutingStatePath(), content, 0o600)
}

func CreateRoutingLease(state RoutingState, lane LaneBinding, ttl time.Duration) (RoutingLease, error) {
	return createRoutingLease(state, lane, ttl, "", "")
}

func createRoutingLease(state RoutingState, lane LaneBinding, ttl time.Duration, coordinatorNonce, ownerNonce string) (RoutingLease, error) {
	if err := ValidateRoutingState(state); err != nil {
		return RoutingLease{}, fmt.Errorf("invalid routing state: %w", err)
	}
	if state.State != RoutingStateCommitted || state.Generation == 0 {
		return RoutingLease{}, fmt.Errorf("routing lease requires committed state")
	}
	if lane.Generation != state.Generation || lane.CoordinatorNonce != state.CoordinatorNonce || !lane.Ready {
		return RoutingLease{}, fmt.Errorf("lane does not belong to committed routing state")
	}
	if ttl <= 0 {
		return RoutingLease{}, fmt.Errorf("routing lease TTL must be positive")
	}
	nonce, err := newRoutingNonce()
	if err != nil {
		return RoutingLease{}, err
	}
	lease := RoutingLease{
		Version: RoutingStateVersion, CoordinatorNonce: state.CoordinatorNonce,
		Generation: state.Generation, LaneID: lane.ID, LaneInstanceNonce: lane.InstanceNonce,
		Listener: lane.Listener, LeaseNonce: nonce, ExpiresAt: time.Now().UTC().Add(ttl).Format(time.RFC3339Nano),
	}
	if err := validateRoutingLease(lease); err != nil {
		return RoutingLease{}, err
	}
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return RoutingLease{}, err
	}
	defer release()
	if coordinatorNonce != "" && !coordinatorClaimMatchesUnlocked(coordinatorNonce, ownerNonce) {
		return RoutingLease{}, fmt.Errorf("coordinator claim is not owned")
	}
	currentState, err := readRoutingStateUnlocked()
	if err != nil {
		return RoutingLease{}, fmt.Errorf("read routing state: %w", err)
	}
	if !sameRoutingState(currentState, state) {
		return RoutingLease{}, fmt.Errorf("routing state is not currently published")
	}
	currentLane, ok := routingLane(currentState, lane.ID)
	if !ok || !sameLaneIdentity(currentLane, lane) {
		return RoutingLease{}, fmt.Errorf("lane is not currently published")
	}
	ownership, err := readRoutingOwnershipUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return RoutingLease{}, fmt.Errorf("read routing ownership: %w", err)
	}
	if os.IsNotExist(err) {
		ownership = routingOwnershipFile{Version: RoutingStateVersion, CoordinatorNonce: state.CoordinatorNonce}
	}
	if coordinatorNonce != "" && (ownership.CoordinatorNonce != coordinatorNonce || ownership.OwnerNonce != ownerNonce || !coordinatorLive(ownership, time.Now())) {
		return RoutingLease{}, fmt.Errorf("coordinator claim is not owned")
	}
	leases := ownership.Leases
	for _, existing := range leases {
		if existing.LaneID == lease.LaneID && existing.Generation == lease.Generation && existing.CoordinatorNonce == lease.CoordinatorNonce && leaseLive(existing, time.Now()) {
			return RoutingLease{}, fmt.Errorf("routing lease already owned")
		}
	}
	kept := leases[:0]
	for _, existing := range leases {
		if leaseLive(existing, time.Now()) && existing.Generation == currentState.Generation && existing.CoordinatorNonce == currentState.CoordinatorNonce {
			kept = append(kept, existing)
		}
	}
	leases = append(kept, lease)
	ownership.Version = RoutingStateVersion
	ownership.CoordinatorNonce = state.CoordinatorNonce
	if ownership.ExpiresAt == "" {
		ownership.ExpiresAt = lease.ExpiresAt
	}
	ownership.Leases = leases
	if err := writeRoutingOwnershipUnlocked(ownership); err != nil {
		return RoutingLease{}, err
	}
	return lease, nil
}

func ReadRoutingLease(laneID ...string) (RoutingLease, error) {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return RoutingLease{}, err
	}
	defer release()
	leases, err := readRoutingLeasesUnlocked()
	if err != nil {
		return RoutingLease{}, err
	}
	if len(laneID) > 1 {
		return RoutingLease{}, fmt.Errorf("multiple lane IDs supplied")
	}
	for _, lease := range leases {
		if len(laneID) == 0 || lease.LaneID == laneID[0] {
			return lease, nil
		}
	}
	return RoutingLease{}, os.ErrNotExist
}

func RenewRoutingLease(lease RoutingLease, ttl time.Duration) (RoutingLease, error) {
	return renewRoutingLease(lease, ttl, "", "")
}

func renewRoutingLease(lease RoutingLease, ttl time.Duration, coordinatorNonce, ownerNonce string) (RoutingLease, error) {
	if ttl <= 0 {
		return RoutingLease{}, fmt.Errorf("routing lease TTL must be positive")
	}
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return RoutingLease{}, err
	}
	defer release()
	if coordinatorNonce != "" && !coordinatorClaimMatchesUnlocked(coordinatorNonce, ownerNonce) {
		return RoutingLease{}, fmt.Errorf("coordinator claim is not owned")
	}
	state, stateErr := readRoutingStateUnlocked()
	ownership, err := readRoutingOwnershipUnlocked()
	if err != nil {
		return RoutingLease{}, fmt.Errorf("routing lease is not owned")
	}
	leases := ownership.Leases
	var current RoutingLease
	for _, candidate := range leases {
		if candidate.LeaseNonce == lease.LeaseNonce {
			current = candidate
			break
		}
	}
	if err != nil || stateErr != nil || current.LeaseNonce == "" || !leaseLive(current, time.Now()) || !leaseMatchesState(current, state) {
		return RoutingLease{}, fmt.Errorf("routing lease is not owned")
	}
	current.ExpiresAt = time.Now().UTC().Add(ttl).Format(time.RFC3339Nano)
	for i := range leases {
		if leases[i].LeaseNonce == current.LeaseNonce {
			leases[i] = current
		}
	}
	if coordinatorNonce != "" && (ownership.CoordinatorNonce != coordinatorNonce || ownership.OwnerNonce != ownerNonce) {
		return RoutingLease{}, fmt.Errorf("coordinator claim is not owned")
	}
	ownership.Leases = leases
	if err := writeRoutingOwnershipUnlocked(ownership); err != nil {
		return RoutingLease{}, err
	}
	return current, nil
}

func renewRoutingLeases(leases []RoutingLease, ttl time.Duration, coordinatorNonce, ownerNonce string) ([]RoutingLease, error) {
	return renewRoutingOwnership(coordinatorNonce, ownerNonce, ttl, leases)
}

func releaseRoutingLeases(leases []RoutingLease, coordinatorNonce, ownerNonce string) error {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return err
	}
	defer release()
	ownership, err := readRoutingOwnershipUnlocked()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if ownership.CoordinatorNonce != coordinatorNonce || ownership.OwnerNonce != ownerNonce {
		return fmt.Errorf("coordinator claim is not owned")
	}
	for _, lease := range leases {
		for i, persisted := range ownership.Leases {
			if sameRoutingLease(persisted, lease) {
				ownership.Leases = append(ownership.Leases[:i], ownership.Leases[i+1:]...)
				break
			}
		}
	}
	return writeRoutingOwnershipUnlocked(ownership)
}

func ReleaseRoutingLease(lease RoutingLease) error {
	return releaseRoutingLease(lease, "", "")
}

func releaseRoutingLease(lease RoutingLease, coordinatorNonce, ownerNonce string) error {
	routingStoreMu.Lock()
	defer routingStoreMu.Unlock()
	release, err := acquireRoutingStoreLock()
	if err != nil {
		return err
	}
	defer release()
	if coordinatorNonce != "" && !coordinatorClaimMatchesUnlocked(coordinatorNonce, ownerNonce) {
		return fmt.Errorf("coordinator claim is not owned")
	}
	leases, err := readRoutingLeasesUnlocked()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	index := -1
	for i := range leases {
		if leases[i].LeaseNonce == lease.LeaseNonce {
			index = i
			break
		}
	}
	if index < 0 || !sameRoutingLease(leases[index], lease) {
		return fmt.Errorf("routing lease is owned by another coordinator")
	}
	leases = append(leases[:index], leases[index+1:]...)
	ownership, err := readRoutingOwnershipUnlocked()
	if err != nil {
		return err
	}
	ownership.Leases = leases
	if err := writeRoutingOwnershipUnlocked(ownership); err != nil {
		return err
	}
	return nil
}

func readRoutingStateUnlocked() (RoutingState, error) {
	raw, err := os.ReadFile(RoutingStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return RoutingState{State: RoutingStateAbsent}, nil
		}
		return RoutingState{}, err
	}
	return ParseRoutingState(string(raw))
}

func readRoutingLeasesUnlocked() ([]RoutingLease, error) {
	ownership, err := readRoutingOwnershipUnlocked()
	if os.IsNotExist(err) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	return ownership.Leases, nil
}

func validateRoutingLease(lease RoutingLease) error {
	if lease.Version != RoutingStateVersion || !ValidRoutingID(lease.CoordinatorNonce) || lease.Generation == 0 || !ValidRoutingID(lease.LaneID) || !ValidRoutingID(lease.LaneInstanceNonce) || !ValidRoutingToken(lease.LeaseNonce) || lease.Listener == "" {
		return fmt.Errorf("invalid routing lease")
	}
	if _, err := time.Parse(time.RFC3339Nano, lease.ExpiresAt); err != nil {
		return fmt.Errorf("invalid routing lease expiry")
	}
	port := listenerPort(lease.Listener)
	if port <= 0 || !validLoopbackListener(lease.Listener, port) {
		return fmt.Errorf("invalid routing lease listener")
	}
	return nil
}

func leaseLive(lease RoutingLease, now time.Time) bool {
	expires, err := time.Parse(time.RFC3339Nano, lease.ExpiresAt)
	return err == nil && expires.After(now)
}

func listenerPort(listener string) int {
	_, rawPort, err := net.SplitHostPort(listener)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return 0
	}
	return port
}

func newRoutingNonce() (string, error) {
	return NewRoutingToken()
}

func routingLane(state RoutingState, id string) (LaneBinding, bool) {
	for _, lane := range state.Lanes {
		if lane.ID == id {
			return lane, true
		}
	}
	return LaneBinding{}, false
}

func sameRoutingStateIdentity(a, b RoutingState) bool {
	return a.State == b.State && a.Generation == b.Generation && a.CoordinatorNonce == b.CoordinatorNonce && a.CommitNonce == b.CommitNonce
}

func sameRoutingState(a, b RoutingState) bool {
	if !sameRoutingStateIdentity(a, b) || a.PreviousGeneration != b.PreviousGeneration {
		return false
	}
	aContent, aErr := EncodeRoutingState(a)
	bContent, bErr := EncodeRoutingState(b)
	return aErr == nil && bErr == nil && aContent == bContent
}

func sameRoutingLease(a, b RoutingLease) bool {
	return a.Version == b.Version && a.CoordinatorNonce == b.CoordinatorNonce && a.Generation == b.Generation && a.LaneID == b.LaneID && a.LaneInstanceNonce == b.LaneInstanceNonce && a.Listener == b.Listener && a.LeaseNonce == b.LeaseNonce && a.ExpiresAt == b.ExpiresAt
}

func sameLaneIdentity(a, b LaneBinding) bool {
	return a.ID == b.ID && a.InstanceNonce == b.InstanceNonce && a.ProviderInstanceID == b.ProviderInstanceID && a.Protocol == b.Protocol && a.UpstreamID == b.UpstreamID && a.Listener == b.Listener && a.Port == b.Port && a.Generation == b.Generation && a.CoordinatorNonce == b.CoordinatorNonce && a.Ready == b.Ready
}

func leaseMatchesState(lease RoutingLease, state RoutingState) bool {
	lane, ok := routingLane(state, lease.LaneID)
	return ok && state.State == RoutingStateCommitted && lease.Generation == state.Generation && lease.CoordinatorNonce == state.CoordinatorNonce && lane.InstanceNonce == lease.LaneInstanceNonce && lane.Listener == lease.Listener && lane.Ready
}

func sameLeaseSelection(lease RoutingLease, selection RouteSelection) bool {
	return lease.CoordinatorNonce == selection.CoordinatorNonce && lease.Generation == selection.Generation && lease.LaneID == selection.LaneID && lease.LaneInstanceNonce == selection.LaneInstanceNonce && lease.Listener == selection.Listener && lease.LeaseNonce == selection.LeaseNonce
}

func acquireRoutingStoreLock() (func(), error) {
	if err := EnsureDir(filepath.Dir(RoutingStatePath())); err != nil {
		return nil, err
	}
	return acquireRoutingFileLock(filepath.Join(ToklessDataDir(), routingLockFile))
}
