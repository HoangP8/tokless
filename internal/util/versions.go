package util

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VersionInfo holds installed/latest for one tool. Pointers map to TS null.
type VersionInfo struct {
	Installed *string `json:"installed"`
	Latest    *string `json:"latest"`
	Channel   string  `json:"channel"`
}

type cacheShape struct {
	Ts  int64                  `json:"ts"`
	Map map[string]VersionInfo `json:"map"`
}

func cachePath() string {
	home := Home()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "tokless", "versions.json")
}

const cacheTTL = 6 * time.Hour

func loadCache() (*cacheShape, bool) {
	p := cachePath()
	if p == "" {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var obj cacheShape
	if json.Unmarshal(b, &obj) != nil {
		return nil, false
	}
	fresh := time.Since(time.UnixMilli(obj.Ts)) <= cacheTTL
	return &obj, fresh
}

func saveCache(m map[string]VersionInfo) {
	p := cachePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, _ := json.MarshalIndent(cacheShape{Ts: time.Now().UnixMilli(), Map: m}, "", "  ")
	_ = os.WriteFile(p, b, 0o644)
}

func fetchJSON(u string, out any) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "tokless")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(resp.Body).Decode(out) == nil
}

func strp(s string) *string { return &s }

func npmLatest(pkg string) *string {
	// Primary: ask npm itself, so the user's registry/mirror/proxy/auth from
	// .npmrc are honored (a hardcoded npmjs.org GET ignores all of that and
	// fails on mirrored/proxied networks where npm install works fine).
	if v := npmViewLatest(pkg); v != nil {
		return v
	}
	// Fallback (npm not on PATH): direct registry GET against the configured base.
	var data struct {
		DistTags struct {
			Latest string `json:"latest"`
		} `json:"dist-tags"`
	}
	if !fetchJSON(npmRegistryBase()+url.QueryEscape(pkg), &data) {
		return nil
	}
	if data.DistTags.Latest == "" {
		return nil
	}
	return strp(data.DistTags.Latest)
}

// npmViewLatest resolves a package's latest version via the npm CLI (nil if npm
// is absent, times out, or errors). Uses --json to avoid notifier/stderr noise.
func npmViewLatest(pkg string) *string {
	if Which("npm") == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "npm", "info", pkg+"@latest", "version", "--json")
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = nil
	if err := c.Run(); err != nil {
		return nil
	}
	s := strings.TrimSpace(out.String())
	s = strings.Trim(s, "\"") // --json wraps a bare version string in quotes
	if m := reSemver.FindStringSubmatch(s); m != nil {
		return strp(m[1])
	}
	return nil
}

func githubLatestRelease(repo string) *string {
	var data struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	}
	if !fetchJSON("https://api.github.com/repos/"+repo+"/releases/latest", &data) {
		return nil
	}
	tag := data.TagName
	if tag == "" {
		tag = data.Name
	}
	if tag == "" {
		return nil
	}
	return strp(strings.TrimPrefix(tag, "v"))
}

var reSemver = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

func rtkInstalledVersion() *string {
	if Which("rtk") == "" {
		return nil
	}
	r := Run("rtk", []string{"--version"}, RunOptions{Capture: true})
	src := r.Stdout
	if src == "" {
		src = r.Stderr
	}
	if m := reSemver.FindStringSubmatch(src); m != nil {
		return strp(m[1])
	}
	return nil
}

func npmInstalledVersion(pkg string) *string {
	if Which("npm") == "" {
		return nil
	}
	r := Run("npm", []string{"ls", "-g", "--depth=0", "--json", pkg}, RunOptions{Capture: true})
	var j struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if json.Unmarshal([]byte(r.Stdout), &j) != nil {
		return nil
	}
	if d, ok := j.Dependencies[pkg]; ok && d.Version != "" {
		return strp(d.Version)
	}
	return nil
}

// GatherVersions returns version info for all tools, cached for 6h.
func GatherVersions() map[string]VersionInfo { return gatherVersions(false) }

func GatherVersionsForce() map[string]VersionInfo { return gatherVersions(true) }

func gatherVersions(force bool) map[string]VersionInfo {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return map[string]VersionInfo{
			"rtk":          {Installed: strp("0.40.0"), Latest: strp("0.40.0"), Channel: "github"},
			"caveman":      {Installed: nil, Latest: strp("1.0.0"), Channel: "github"},
			"codegraph":    {Installed: nil, Latest: strp("0.9.0"), Channel: "npm"},
			"context-mode": {Installed: nil, Latest: strp("1.0.0"), Channel: "npm"},
			"tokless":      {Installed: strp("0.1.0"), Latest: strp("0.1.0"), Channel: "npm"},
		}
	}
	// Latest (slow, network) is cached; installed (fast, local) is always live.
	latest := cachedLatest(force)
	out := map[string]VersionInfo{}
	out["rtk"] = VersionInfo{Installed: rtkInstalledVersion(), Latest: latest["rtk"], Channel: "github"}
	out["caveman"] = VersionInfo{Installed: cavemanInstalledVersion(), Latest: latest["caveman"], Channel: "github"}
	out["codegraph"] = VersionInfo{Installed: npmInstalledVersion("@colbymchenry/codegraph"), Latest: latest["codegraph"], Channel: "npm"}
	out["context-mode"] = VersionInfo{Installed: npmInstalledVersion("context-mode"), Latest: latest["context-mode"], Channel: "npm"}
	out["tokless"] = VersionInfo{Installed: npmInstalledVersion("tokless"), Latest: latest["tokless"], Channel: "npm"}
	return out
}

// LatestVersionFor returns one tool's latest available version (cached).
func LatestVersionFor(id string) *string {
	return cachedLatest(false)[id]
}

// InstalledVersionFor reads one tool's live installed version (nil if absent).
func InstalledVersionFor(id string) *string {
	switch id {
	case "rtk":
		return rtkInstalledVersion()
	case "codegraph":
		return npmInstalledVersion("@colbymchenry/codegraph")
	case "context-mode":
		return npmInstalledVersion("context-mode")
	case "tokless":
		return npmInstalledVersion("tokless")
	case "caveman":
		return cavemanInstalledVersion()
	}
	return nil
}

const cavemanVersionMarker = ".tokless-version"

// cavemanVersionDirs lists per-agent caveman install dirs, priority order.
func cavemanVersionDirs() []string {
	home := Home()

	claude := filepath.Join(home, ".claude")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		claude = d
	}

	codex := filepath.Join(home, ".codex")
	if d := os.Getenv("CODEX_HOME"); d != "" {
		codex = d
	}

	gemini := filepath.Join(home, ".gemini")

	return []string{
		filepath.Join(OpenCodePathsResolved().Dir, "plugins", "caveman"),
		filepath.Join(claude, "plugins", "marketplaces", "caveman"),
		filepath.Join(claude, "plugins", "caveman"),
		filepath.Join(codex, "skills", "caveman"),
		filepath.Join(home, ".agents", "skills", "caveman"),
		filepath.Join(gemini, "antigravity", "skills", "caveman"),
		filepath.Join(gemini, "config", "skills", "caveman"),
	}
}

// cavemanInstalled reports whether a caveman install exists in dir.
func cavemanInstalled(dir string) bool {
	return Exists(filepath.Join(dir, "plugin.js")) ||
		Exists(filepath.Join(dir, "SKILL.md")) ||
		Exists(filepath.Join(dir, "package.json"))
}

func readCavemanMarker(dir string) string {
	if raw, ok := ReadFileSafe(filepath.Join(dir, cavemanVersionMarker)); ok {
		return strings.TrimSpace(raw)
	}
	return ""
}

func readCavemanPkgVersion(dir string) string {
	if raw, ok := ReadFileSafe(filepath.Join(dir, "package.json")); ok {
		var pkg struct {
			Version string `json:"version"`
		}
		if json.Unmarshal([]byte(raw), &pkg) == nil && pkg.Version != "" && pkg.Version != "0.1.0" {
			return pkg.Version
		}
	}
	return ""
}

// StampCavemanVersion records version into every present caveman dir.
func StampCavemanVersion(version string) {
	if version == "" {
		return
	}
	for _, dir := range cavemanVersionDirs() {
		if cavemanInstalled(dir) {
			_ = os.WriteFile(filepath.Join(dir, cavemanVersionMarker), []byte(version+"\n"), 0o644)
		}
	}
}

func cavemanInstalledVersion() *string {
	presentDir := ""
	for _, dir := range cavemanVersionDirs() {
		if v := readCavemanMarker(dir); v != "" {
			return strp(v)
		}
		if v := readCavemanPkgVersion(dir); v != "" {
			return strp(v)
		}
		if presentDir == "" && cavemanInstalled(dir) {
			presentDir = dir
		}
	}

	if presentDir != "" {
		if latest := cachedLatest(false)["caveman"]; latest != nil {
			_ = os.WriteFile(filepath.Join(presentDir, cavemanVersionMarker), []byte(*latest+"\n"), 0o644)
			return latest
		}
	}
	return nil
}

var toolIDs = []string{"rtk", "caveman", "codegraph", "context-mode", "tokless"}

var latestFetcher = fetchLatestFor

// fetchLatestFor resolves one tool's latest upstream version (nil on failure).
func fetchLatestFor(id string) *string {
	switch id {
	case "rtk":
		return githubLatestRelease("rtk-ai/rtk")
	case "caveman":
		return githubLatestRelease("JuliusBrussee/caveman")
	case "codegraph":
		return npmLatest("@colbymchenry/codegraph")
	case "context-mode":
		return npmLatest("context-mode")
	case "tokless":
		return npmLatest("tokless")
	}
	return nil
}

// cachedLatest returns the latest-version lookups, cached to disk (6h TTL).
func cachedLatest(force bool) map[string]*string {
	if os.Getenv("TOKLESS_TEST") == "1" {
		m := map[string]*string{}
		for k, v := range GatherVersions() {
			m[k] = v.Latest
		}
		return m
	}

	cache, fresh := loadCache()
	result := map[string]*string{}
	if cache != nil {
		for k, v := range cache.Map {
			if v.Latest != nil {
				result[k] = v.Latest
			}
		}
	}

	// Fetch needed ids in parallel; npm CLI spawn is heavy, so pay it once in
	// wall-clock time rather than once per tool.
	var todo []string
	for _, id := range toolIDs {
		if result[id] != nil && fresh && !force {
			continue
		}
		todo = append(todo, id)
	}
	fetched := false
	if len(todo) > 0 {
		var wg sync.WaitGroup
		var mu sync.Mutex
		got := make(map[string]*string, len(todo))
		for _, id := range todo {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				if v := latestFetcher(id); v != nil {
					mu.Lock()
					got[id] = v
					mu.Unlock()
				}
			}(id)
		}
		wg.Wait()
		for id, v := range got {
			result[id] = v
			fetched = true
		}
	}

	// Persist on any successful fetch, or when forced.
	if fetched || force {
		store := map[string]VersionInfo{}
		for k, v := range result {
			if v != nil {
				store[k] = VersionInfo{Latest: v}
			}
		}
		saveCache(store)
	}
	return result
}

func parseSemverParts(s string) []int {
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

// SemverCompare returns -1/0/1 comparing two version strings.
func SemverCompare(a, b *string) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	pa, pb := parseSemverParts(*a), parseSemverParts(*b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		da, db := 0, 0
		if i < len(pa) {
			da = pa[i]
		}
		if i < len(pb) {
			db = pb[i]
		}
		if da != db {
			if da > db {
				return 1
			}
			return -1
		}
	}
	return 0
}

func SemverGte(a, b string) bool { return SemverCompare(&a, &b) >= 0 }

func CountOutdated(m map[string]VersionInfo) int {
	n := 0
	for _, v := range m {
		if v.Installed != nil && v.Latest != nil && SemverCompare(v.Installed, v.Latest) < 0 {
			n++
		}
	}
	return n
}

func BustVersionCache() {
	_ = os.Remove(cachePath())
}
