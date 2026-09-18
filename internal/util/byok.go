package util

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var byokRegistryLock sync.Mutex

func withBYOKRegistryLock(fn func() error) error {
	if err := EnsureDir(HeadroomPathsResolved().Root); err != nil {
		return err
	}
	path := filepath.Join(HeadroomPathsResolved().Root, "byok.routes.lock")
	token := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, err = file.WriteString(token); err == nil {
				err = file.Close()
			} else {
				_ = file.Close()
			}
			if err != nil {
				_ = os.Remove(path)
				return err
			}
			defer func() {
				if raw, readErr := os.ReadFile(path); readErr == nil && string(raw) == token {
					_ = os.Remove(path)
				}
			}()
			return fn()
		}
		if !os.IsExist(err) {
			return err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(path)
			continue
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// BYOKRoute contains routing metadata only. Provider credentials stay in the
// agent's own configuration and never enter this registry.
type BYOKRoute struct {
	ID       string `json:"id"`
	Token    string `json:"token"`
	Protocol string `json:"protocol"`
	Upstream string `json:"upstream"`
}

type byokRouteFile struct {
	Routes []BYOKRoute `json:"routes"`
}

func byokRoutesPath() string { return filepath.Join(HeadroomPathsResolved().Root, "byok.routes.json") }

func BYOKRouteHeader(route BYOKRoute) string { return route.ID + "." + route.Token }

func BYOKRouteProtocol(api string) string {
	switch api {
	case "openai-completions":
		return "openai-chat"
	case "openai-responses":
		return "openai-responses"
	case "anthropic-messages":
		return "anthropic-messages"
	default:
		return ""
	}
}

func ValidBYOKProtocol(protocol string) bool {
	switch protocol {
	case "openai-chat", "openai-responses", "anthropic-messages":
		return true
	default:
		return false
	}
}

// ValidBYOKID validates a route ID allowing only alphanumeric, underscore, colon, and hyphen.
func ValidBYOKID(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if !(c == '.' || c == '-' || c == '_' || c == ':' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}

func ValidBYOKToken(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if !(c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}

func ValidBYOKUpstream(upstream string) bool {
	upstream = strings.TrimRight(strings.TrimSpace(upstream), "/")
	u, err := url.Parse(upstream)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	isLoopback := IsLoopbackHost(u.Hostname())
	if u.Scheme == "https" {
		if isLoopback {
			return true
		}
		port := u.Port()
		return port == "" || port == "443" || port == "8443"
	}
	return u.Scheme == "http" && isLoopback
}

func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func NewBYOKRoute(id, protocol, upstream string) (BYOKRoute, error) {
	upstream = strings.TrimRight(strings.TrimSpace(upstream), "/")
	if !ValidBYOKUpstream(upstream) {
		return BYOKRoute{}, fmt.Errorf("invalid BYOK upstream")
	}
	if !ValidBYOKID(id) || !ValidBYOKProtocol(protocol) {
		return BYOKRoute{}, fmt.Errorf("invalid BYOK route identity")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return BYOKRoute{}, err
	}
	return BYOKRoute{ID: id, Token: base64.RawURLEncoding.EncodeToString(b), Protocol: protocol, Upstream: upstream}, nil
}

func SaveBYOKRoutes(routes []BYOKRoute) error {
	byokRegistryLock.Lock()
	defer byokRegistryLock.Unlock()
	return withBYOKRegistryLock(func() error {
		clean := make([]BYOKRoute, 0, len(routes))
		seen := make(map[string]bool, len(routes))
		for _, route := range routes {
			route.ID = strings.TrimSpace(route.ID)
			route.Token = strings.TrimSpace(route.Token)
			route.Protocol = strings.TrimSpace(route.Protocol)
			route.Upstream = strings.TrimRight(strings.TrimSpace(route.Upstream), "/")
			if !ValidBYOKID(route.ID) || !ValidBYOKToken(route.Token) || !ValidBYOKProtocol(route.Protocol) {
				return fmt.Errorf("invalid BYOK route %q", route.ID)
			}
			if _, err := NewBYOKRoute(route.ID, route.Protocol, route.Upstream); err != nil {
				return err
			}
			if seen[route.ID] {
				return fmt.Errorf("route %q: duplicate ID", route.ID)
			}
			seen[route.ID] = true
			clean = append(clean, route)
		}
		content, err := EncodeBYOKRoutes(clean)
		if err != nil {
			return err
		}
		return WriteFileAtomic(byokRoutesPath(), content, 0o600)
	})
}

func ReadBYOKRoutes() ([]BYOKRoute, error) {
	byokRegistryLock.Lock()
	defer byokRegistryLock.Unlock()
	var routes []BYOKRoute
	err := withBYOKRegistryLock(func() error {
		raw, err := os.ReadFile(byokRoutesPath())
		if err != nil {
			return err
		}
		routes, err = ParseBYOKRoutes(string(raw))
		return err
	})
	return routes, err
}

func ParseBYOKRoutes(raw string) ([]BYOKRoute, error) {
	var f byokRouteFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, err
	}
	return f.Routes, nil
}

func EncodeBYOKRoutes(routes []BYOKRoute) (string, error) {
	b, err := json.Marshal(byokRouteFile{Routes: routes})
	return string(b), err
}

// UpsertBYOKRoute records routing metadata without touching agent credentials.
// Returns the new route and the previous route (if any) for rollback.
func UpsertBYOKRoute(id, protocol, upstream string) (BYOKRoute, BYOKRoute, error) {
	if _, err := NewBYOKRoute(id, protocol, upstream); err != nil {
		return BYOKRoute{}, BYOKRoute{}, err
	}
	byokRegistryLock.Lock()
	defer byokRegistryLock.Unlock()
	var result, previous BYOKRoute
	err := withBYOKRegistryLock(func() error {
		routes := []BYOKRoute{}
		if raw, err := os.ReadFile(byokRoutesPath()); err == nil {
			var parseErr error
			routes, parseErr = ParseBYOKRoutes(string(raw))
			if parseErr != nil {
				return fmt.Errorf("invalid BYOK route registry: %w", parseErr)
			}
		}
		for i := range routes {
			if routes[i].ID == id {
				previous = routes[i]
				routes[i].Protocol = protocol
				routes[i].Upstream = strings.TrimRight(strings.TrimSpace(upstream), "/")
				result = routes[i]
				content, err := EncodeBYOKRoutes(routes)
				if err != nil {
					return err
				}
				return WriteFileAtomic(byokRoutesPath(), content, 0o600)
			}
		}
		route, err := NewBYOKRoute(id, protocol, upstream)
		if err != nil {
			return err
		}
		routes = append(routes, route)
		result = route
		content, err := EncodeBYOKRoutes(routes)
		if err != nil {
			return err
		}
		return WriteFileAtomic(byokRoutesPath(), content, 0o600)
	})
	return result, previous, err
}

func BYOKGatewayPort() int {
	if raw := strings.TrimSpace(os.Getenv("TOKLESS_BYOK_PROXY_PORT")); raw != "" {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	return 18787
}

func BYOKGatewayEndpoint() string { return "http://127.0.0.1:" + strconv.Itoa(BYOKGatewayPort()) }

func ReadBYOKRoute(id string) (BYOKRoute, bool) {
	byokRegistryLock.Lock()
	defer byokRegistryLock.Unlock()
	var result BYOKRoute
	err := withBYOKRegistryLock(func() error {
		raw, err := os.ReadFile(byokRoutesPath())
		if err != nil {
			return err
		}
		routes, err := ParseBYOKRoutes(string(raw))
		if err != nil {
			return err
		}
		for _, route := range routes {
			if route.ID == id {
				result = route
				break
			}
		}
		return nil
	})
	return result, err == nil && result.ID != ""
}

// DeleteBYOKRoute removes a route from the registry by ID.
func DeleteBYOKRoute(id string) error {
	byokRegistryLock.Lock()
	defer byokRegistryLock.Unlock()
	return withBYOKRegistryLock(func() error {
		raw, err := os.ReadFile(byokRoutesPath())
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		routes, err := ParseBYOKRoutes(string(raw))
		if err != nil {
			return err
		}
		var remaining []BYOKRoute
		for _, route := range routes {
			if route.ID != id {
				remaining = append(remaining, route)
			}
		}
		encoded, err := EncodeBYOKRoutes(remaining)
		if err != nil {
			return err
		}
		return WriteFileAtomic(byokRoutesPath(), encoded, 0o600)
	})
}
