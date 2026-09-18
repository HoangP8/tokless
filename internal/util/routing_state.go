package util

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

const RoutingStateVersion = 1

const (
	RoutingStateAbsent    = "absent"
	RoutingStatePreparing = "preparing"
	RoutingStateCommitted = "committed"
	RoutingStateRevoking  = "revoking"
	RoutingStateStopped   = "stopped"
	RoutingStateDegraded  = "degraded"
)

// RoutingState is the coordinator's complete routing snapshot. Routes and
// lanes are validated as one graph before any request can use them.
type RoutingState struct {
	Version            int            `json:"version"`
	Generation         uint64         `json:"generation"`
	PreviousGeneration uint64         `json:"previous_generation"`
	State              string         `json:"state"`
	CoordinatorNonce   string         `json:"coordinator_nonce"`
	CommitNonce        string         `json:"commit_nonce,omitempty"`
	Routes             []RouteBinding `json:"routes"`
	Lanes              []LaneBinding  `json:"lanes"`
}

// RouteBinding maps one client provider instance to one coordinator lane.
type RouteBinding struct {
	ID                 string `json:"id"`
	Token              string `json:"token"`
	ProfileID          string `json:"profile_id"`
	SurfaceID          string `json:"surface_id"`
	ProviderInstanceID string `json:"provider_instance_id"`
	Protocol           string `json:"protocol"`
	LaneID             string `json:"lane_id"`
	LaneInstanceNonce  string `json:"lane_instance_nonce"`
	CoordinatorNonce   string `json:"coordinator_nonce"`
	UpstreamID         string `json:"upstream_id"`
	ConfigFingerprint  string `json:"config_fingerprint"`
	Generation         uint64 `json:"generation"`
	Revoked            bool   `json:"revoked"`
	RevokedGeneration  uint64 `json:"revoked_generation,omitempty"`
}

// LaneBinding identifies one coordinator-owned backend listener.
type LaneBinding struct {
	ID                 string `json:"id"`
	InstanceNonce      string `json:"instance_nonce"`
	ProviderInstanceID string `json:"provider_instance_id"`
	Protocol           string `json:"protocol"`
	UpstreamID         string `json:"upstream_id"`
	Listener           string `json:"listener"`
	Port               int    `json:"port"`
	Generation         uint64 `json:"generation"`
	CoordinatorNonce   string `json:"coordinator_nonce"`
	Ready              bool   `json:"ready"`
}

func NewRoutingToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func ValidRoutingID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if !(c == '.' || c == '-' || c == '_' || c == ':' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}

func ValidRoutingToken(value string) bool {
	if len(value) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(b) == 32
}

func ValidConfigFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func ValidateRoutingState(state RoutingState) error {
	switch state.State {
	case RoutingStateAbsent:
		if state.Version != 0 || state.Generation != 0 || state.PreviousGeneration != 0 || state.CoordinatorNonce != "" || state.CommitNonce != "" || len(state.Routes) != 0 || len(state.Lanes) != 0 {
			return fmt.Errorf("absent routing state must be empty")
		}
		return nil
	case RoutingStatePreparing, RoutingStateCommitted, RoutingStateRevoking, RoutingStateStopped, RoutingStateDegraded:
	default:
		return fmt.Errorf("invalid routing state %q", state.State)
	}
	if state.Version != RoutingStateVersion {
		return fmt.Errorf("unsupported routing state version %d", state.Version)
	}
	if state.Generation == 0 || state.PreviousGeneration > state.Generation || !ValidRoutingID(state.CoordinatorNonce) {
		return fmt.Errorf("invalid routing state generation or coordinator")
	}
	if state.State == RoutingStateCommitted && !ValidRoutingID(state.CommitNonce) {
		return fmt.Errorf("committed routing state has invalid commit nonce")
	}
	if state.State != RoutingStateCommitted && state.CommitNonce != "" {
		return fmt.Errorf("non-committed routing state has commit nonce")
	}
	if state.State == RoutingStateCommitted && (len(state.Routes) == 0 || len(state.Lanes) == 0) {
		return fmt.Errorf("committed routing state is empty")
	}

	lanes := make(map[string]LaneBinding, len(state.Lanes))
	ports := make(map[int]bool, len(state.Lanes))
	backends := make(map[string]bool, len(state.Lanes))
	laneProviders := make(map[string]bool, len(state.Lanes))
	for _, lane := range state.Lanes {
		if !ValidRoutingID(lane.ID) || !ValidRoutingID(lane.InstanceNonce) {
			return fmt.Errorf("invalid lane identity")
		}
		if _, exists := lanes[lane.ID]; exists {
			return fmt.Errorf("duplicate lane %q", lane.ID)
		}
		if !ValidRoutingID(lane.ProviderInstanceID) || !ValidRoutingID(lane.UpstreamID) || !ValidBYOKProtocol(lane.Protocol) || lane.CoordinatorNonce != state.CoordinatorNonce {
			return fmt.Errorf("lane %q has invalid backend identity", lane.ID)
		}
		if lane.Generation != state.Generation || lane.Port <= 0 || lane.Port > 65535 || !validLoopbackListener(lane.Listener, lane.Port) {
			return fmt.Errorf("lane %q has invalid generation or listener", lane.ID)
		}
		if ports[lane.Port] {
			return fmt.Errorf("duplicate lane port %d", lane.Port)
		}
		backend := lane.ProviderInstanceID + "\x00" + lane.Protocol + "\x00" + lane.UpstreamID
		if backends[backend] {
			return fmt.Errorf("duplicate lane backend %q", lane.ProviderInstanceID)
		}
		if laneProviders[lane.ProviderInstanceID] {
			return fmt.Errorf("duplicate lane provider instance %q", lane.ProviderInstanceID)
		}
		ports[lane.Port] = true
		backends[backend] = true
		laneProviders[lane.ProviderInstanceID] = true
		lanes[lane.ID] = lane
	}

	routeIDs := make(map[string]bool, len(state.Routes))
	tokens := make(map[string]bool, len(state.Routes))
	providers := make(map[string]bool, len(state.Routes))
	for _, route := range state.Routes {
		if !ValidRoutingID(route.ID) || !ValidRoutingToken(route.Token) {
			return fmt.Errorf("invalid route identity")
		}
		if routeIDs[route.ID] {
			return fmt.Errorf("duplicate route %q", route.ID)
		}
		if tokens[route.Token] {
			return fmt.Errorf("duplicate route token")
		}
		if !ValidRoutingID(route.ProfileID) || !ValidRoutingID(route.SurfaceID) || !ValidRoutingID(route.ProviderInstanceID) || !ValidBYOKProtocol(route.Protocol) || !ValidRoutingID(route.LaneID) || !ValidRoutingID(route.LaneInstanceNonce) || route.CoordinatorNonce != state.CoordinatorNonce || !ValidRoutingID(route.UpstreamID) || !ValidConfigFingerprint(route.ConfigFingerprint) {
			return fmt.Errorf("route %q has invalid identity", route.ID)
		}
		if route.Generation != state.Generation {
			return fmt.Errorf("route %q has stale generation", route.ID)
		}
		if route.Revoked && (route.RevokedGeneration == 0 || route.RevokedGeneration > state.Generation) || !route.Revoked && route.RevokedGeneration != 0 {
			return fmt.Errorf("route %q has invalid revocation state", route.ID)
		}
		lane, exists := lanes[route.LaneID]
		if !exists || lane.InstanceNonce != route.LaneInstanceNonce || lane.CoordinatorNonce != route.CoordinatorNonce || lane.ProviderInstanceID != route.ProviderInstanceID || lane.Protocol != route.Protocol || lane.UpstreamID != route.UpstreamID {
			return fmt.Errorf("route %q does not bind exactly to lane", route.ID)
		}
		if providers[route.ProviderInstanceID] {
			return fmt.Errorf("duplicate provider instance %q", route.ProviderInstanceID)
		}
		if state.State == RoutingStateCommitted && (route.Revoked || !lane.Ready) {
			return fmt.Errorf("committed route %q is revoked or lane is not ready", route.ID)
		}
		if state.State != RoutingStateCommitted && !route.Revoked {
			return fmt.Errorf("non-committed route %q is active", route.ID)
		}
		routeIDs[route.ID] = true
		tokens[route.Token] = true
		providers[route.ProviderInstanceID] = true
	}
	return nil
}

func validLoopbackListener(listener string, port int) bool {
	host, service, err := net.SplitHostPort(listener)
	return err == nil && (host == "127.0.0.1" || host == "::1") && service == strconv.Itoa(port) && listener == net.JoinHostPort(host, service) && !strings.Contains(listener, "/")
}

// ValidateRoutingStateTransition prevents a new coordinator from adopting a
// committed state or moving generations backward.
func ValidateRoutingStateTransition(previous, next RoutingState) error {
	if err := ValidateRoutingState(previous); err != nil {
		return fmt.Errorf("invalid previous routing state: %w", err)
	}
	if err := ValidateRoutingState(next); err != nil {
		return err
	}
	if previous.State == RoutingStateAbsent {
		if next.PreviousGeneration != 0 || next.State != RoutingStatePreparing {
			return fmt.Errorf("initial routing state must enter preparing")
		}
		return nil
	}
	if previous.CoordinatorNonce != next.CoordinatorNonce {
		return fmt.Errorf("routing coordinator changed")
	}
	if next.PreviousGeneration != previous.Generation || next.Generation <= previous.Generation {
		return fmt.Errorf("routing generation is not monotonic")
	}
	allowed := map[string]map[string]bool{
		RoutingStatePreparing: {RoutingStateCommitted: true, RoutingStateDegraded: true, RoutingStateStopped: true},
		RoutingStateCommitted: {RoutingStatePreparing: true, RoutingStateRevoking: true, RoutingStateDegraded: true, RoutingStateStopped: true},
		RoutingStateRevoking:  {RoutingStateStopped: true, RoutingStatePreparing: true, RoutingStateDegraded: true},
		RoutingStateDegraded:  {RoutingStatePreparing: true, RoutingStateStopped: true},
		RoutingStateStopped:   {RoutingStatePreparing: true},
	}
	if !allowed[previous.State][next.State] {
		return fmt.Errorf("invalid routing state transition %q to %q", previous.State, next.State)
	}
	return nil
}

func ParseRoutingState(raw string) (RoutingState, error) {
	keyDecoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	if err := validateJSONKeys(keyDecoder); err != nil {
		return RoutingState{}, err
	}
	var state RoutingState
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return RoutingState{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return RoutingState{}, fmt.Errorf("trailing routing state data")
	} else if err != io.EOF {
		return RoutingState{}, err
	}
	if err := ValidateRoutingState(state); err != nil {
		return RoutingState{}, err
	}
	return state, nil
}

func validateJSONKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch delimiter := token.(type) {
	case json.Delim:
		switch delimiter {
		case '{':
			keys := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return fmt.Errorf("duplicate or invalid JSON object key")
				}
				keys[name] = true
				if err := validateJSONKeys(decoder); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := validateJSONKeys(decoder); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		}
	}
	return nil
}

func EncodeRoutingState(state RoutingState) (string, error) {
	if err := ValidateRoutingState(state); err != nil {
		return "", err
	}
	b, err := json.Marshal(state)
	return string(b), err
}
