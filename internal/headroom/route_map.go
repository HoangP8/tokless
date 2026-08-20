package headroom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/HoangP8/tokless/internal/util"
)

// RouteEntry maps a BYOK credential fingerprint to the real upstream base URL.
type RouteEntry struct {
	KeyFP   string `json:"key_fp"`
	BaseURL string `json:"base_url"`
	ID      string `json:"id,omitempty"`
}

type routeMapFile struct {
	Routes []RouteEntry `json:"routes"`
}

var (
	routeMu   sync.RWMutex
	routeByFP map[string]RouteEntry
)

func routeMapPath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "proxy.routes.json")
}

// SaveRouteMap replaces the on-disk + in-memory key→upstream table (mode 0600).
func SaveRouteMap(entries []RouteEntry) error {
	clean := make([]RouteEntry, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		e.KeyFP = strings.TrimSpace(e.KeyFP)
		e.BaseURL = strings.TrimRight(strings.TrimSpace(e.BaseURL), "/")
		if e.KeyFP == "" || e.BaseURL == "" || seen[e.KeyFP] {
			continue
		}
		seen[e.KeyFP] = true
		clean = append(clean, e)
	}
	b, err := json.Marshal(routeMapFile{Routes: clean})
	if err != nil {
		return err
	}
	path := routeMapPath()
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	routeMu.Lock()
	routeByFP = make(map[string]RouteEntry, len(clean))
	for _, e := range clean {
		routeByFP[e.KeyFP] = e
	}
	routeMu.Unlock()
	return nil
}

// LoadRouteMap loads routes from disk into memory. Missing file = empty table.
func LoadRouteMap() []RouteEntry {
	raw, ok := util.ReadFileSafe(routeMapPath())
	if !ok {
		routeMu.Lock()
		routeByFP = map[string]RouteEntry{}
		routeMu.Unlock()
		return nil
	}
	var f routeMapFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil
	}
	routeMu.Lock()
	routeByFP = make(map[string]RouteEntry, len(f.Routes))
	out := make([]RouteEntry, 0, len(f.Routes))
	for _, e := range f.Routes {
		if e.KeyFP == "" || e.BaseURL == "" {
			continue
		}
		routeByFP[e.KeyFP] = e
		out = append(out, e)
	}
	routeMu.Unlock()
	return out
}

// ClearRouteMap drops the on-disk route table and memory.
func ClearRouteMap() error {
	routeMu.Lock()
	routeByFP = map[string]RouteEntry{}
	routeMu.Unlock()
	if err := os.Remove(routeMapPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// KeyFingerprint is a stable non-reversible id for a credential.
func KeyFingerprint(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// LookupRoute returns the upstream base URL for a raw API key, if mapped.
// Always reloads disk so a detached route-proxy worker sees parent SaveRouteMap.
func LookupRoute(rawKey string) (baseURL string, ok bool) {
	fp := KeyFingerprint(rawKey)
	if fp == "" {
		return "", false
	}
	_ = LoadRouteMap()
	routeMu.RLock()
	e, ok := routeByFP[fp]
	routeMu.RUnlock()
	if !ok {
		return "", false
	}
	return e.BaseURL, true
}

// RouteCount is the number of live BYOK routes (for status / skip empty router).
func RouteCount() int {
	routeMu.RLock()
	n := len(routeByFP)
	routeMu.RUnlock()
	if n > 0 {
		return n
	}
	return len(LoadRouteMap())
}
