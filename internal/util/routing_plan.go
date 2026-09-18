package util

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RoutingSnapshot is an immutable-by-convention copy suitable for concurrent
// request handling. Build one at publication time; never mutate its state.
type RoutingSnapshot struct {
	state RoutingState
}

func (snapshot RoutingSnapshot) Generation() uint64 { return snapshot.state.Generation }

type RouteRequest struct {
	Credential string
	Protocol   string
	Method     string
	Path       string
	RawPath    string
	RawQuery   string
}

func NewRoutingSnapshot(state RoutingState) (RoutingSnapshot, error) {
	if err := ValidateRoutingState(state); err != nil {
		return RoutingSnapshot{}, err
	}
	state.Routes = append([]RouteBinding(nil), state.Routes...)
	state.Lanes = append([]LaneBinding(nil), state.Lanes...)
	return RoutingSnapshot{state: state}, nil
}

// ResolveRequest validates the complete HTTP routing boundary. ownership must
// prove the selected lane still belongs to this coordinator generation.
func (snapshot RoutingSnapshot) ResolveRequest(request RouteRequest, ownership func(RouteSelection) bool) (RouteSelection, error) {
	if request.Method != http.MethodPost {
		return RouteSelection{}, fmt.Errorf("route method not allowed")
	}
	if request.RawQuery != "" || request.RawPath != "" || strings.Contains(request.Path, "%") {
		return RouteSelection{}, fmt.Errorf("non-canonical route path")
	}
	if !routePathForProtocol(request.Protocol, request.Path) {
		return RouteSelection{}, fmt.Errorf("route protocol or path mismatch")
	}
	selection, err := resolveRoute(snapshot.state, request.Credential, request.Protocol)
	if err != nil {
		return RouteSelection{}, err
	}
	if ownership == nil || !ownership(selection) {
		return RouteSelection{}, fmt.Errorf("route listener ownership unavailable")
	}
	return selection, nil
}

// ResolveRequestWithLease adds the persisted ownership fence required for
// production dispatch. lease must match the committed snapshot and lane.
func (snapshot RoutingSnapshot) ResolveRequestWithLease(request RouteRequest, lease RoutingLease, ownership func(RouteSelection) bool) (RouteSelection, error) {
	if err := validateRoutingLease(lease); err != nil || !leaseLive(lease, time.Now()) {
		return RouteSelection{}, fmt.Errorf("routing lease is unavailable")
	}
	selection, err := snapshot.ResolveRequest(request, func(selection RouteSelection) bool {
		if !leaseMatchesState(lease, snapshot.state) || lease.LaneID != selection.LaneID || lease.Listener != selection.Listener {
			return false
		}
		selection.LeaseNonce = lease.LeaseNonce
		return ownership != nil && ownership(selection)
	})
	if err != nil {
		return RouteSelection{}, err
	}
	selection.LeaseNonce = lease.LeaseNonce
	return selection, nil
}

// RouteSelection is the only destination data a router may dispatch. Listener
// comes from the validated lane binding, never from client input.
type RouteSelection struct {
	RouteID           string
	LaneID            string
	LaneInstanceNonce string
	CoordinatorNonce  string
	UpstreamID        string
	Listener          string
	Protocol          string
	Generation        uint64
	LeaseNonce        string
}

// ResolveRoute selects exactly one committed route from its explicit
// credential and protocol. It has no default or fallback destination.
func resolveRoute(state RoutingState, credential, protocol string) (RouteSelection, error) {
	if err := ValidateRoutingState(state); err != nil {
		return RouteSelection{}, fmt.Errorf("invalid routing state: %w", err)
	}
	if state.State != RoutingStateCommitted {
		return RouteSelection{}, fmt.Errorf("routing state is not committed")
	}
	if !ValidBYOKProtocol(protocol) {
		return RouteSelection{}, fmt.Errorf("invalid request protocol")
	}
	idx := strings.LastIndex(credential, ".")
	if idx <= 0 || idx >= len(credential)-1 {
		return RouteSelection{}, fmt.Errorf("malformed route credential")
	}
	id, token := credential[:idx], credential[idx+1:]
	if !ValidRoutingID(id) || !ValidRoutingToken(token) || strings.Contains(token, ".") {
		return RouteSelection{}, fmt.Errorf("malformed route credential")
	}
	for _, route := range state.Routes {
		if route.ID != id || subtle.ConstantTimeCompare([]byte(route.Token), []byte(token)) != 1 {
			continue
		}
		if route.Revoked || route.Generation != state.Generation || route.Protocol != protocol {
			return RouteSelection{}, fmt.Errorf("route credential is stale or protocol mismatched")
		}
		for _, lane := range state.Lanes {
			if lane.ID == route.LaneID && lane.InstanceNonce == route.LaneInstanceNonce && lane.CoordinatorNonce == route.CoordinatorNonce && lane.ProviderInstanceID == route.ProviderInstanceID && lane.Protocol == route.Protocol && lane.UpstreamID == route.UpstreamID && lane.Generation == state.Generation && lane.Ready {
				return RouteSelection{
					RouteID: route.ID, LaneID: lane.ID, LaneInstanceNonce: lane.InstanceNonce,
					CoordinatorNonce: lane.CoordinatorNonce, UpstreamID: lane.UpstreamID,
					Listener: lane.Listener, Protocol: lane.Protocol, Generation: state.Generation,
				}, nil
			}
		}
		return RouteSelection{}, fmt.Errorf("route lane is unavailable")
	}
	return RouteSelection{}, fmt.Errorf("unknown route credential")
}

func routePathForProtocol(protocol, path string) bool {
	got, ok := RouteProtocolForPath(path)
	return ok && got == protocol
}

func RouteProtocolForPath(path string) (string, bool) {
	switch path {
	case "/v1/chat/completions", "/chat/completions":
		return "openai-chat", true
	case "/v1/responses", "/responses":
		return "openai-responses", true
	case "/v1/messages", "/messages":
		return "anthropic-messages", true
	default:
		return "", false
	}
}

func RouteProtocolPath(protocol, path string) bool {
	return routePathForProtocol(protocol, path)
}
