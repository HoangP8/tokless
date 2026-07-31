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
	Present   bool    `json:"present"`
}

// VersionSpec is a tool's version identity, copied out of core.ToolManifest so
// util doesn't have to import core.
type VersionSpec struct {
	ID       string
	Channel  string // "npm" | "github" | "pypi" | "skill" | "binary"
	Pkg      string
	Repo     string
	Bin      string
	UseTag   bool
	MaxBytes int
	SkillDoc string        // upstream path for skill channel
	Resolve  func() string // optional binary resolver override
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

// VersionCachePath is the versions.json cache path (for tokless info).
func VersionCachePath() string { return cachePath() }

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

const (
	httpTimeout      = 10 * time.Second
	maxSkillDownload = 1 << 20 // 1 MiB ceiling on a fetched skill doc
)

func fetchJSON(u string, out any) bool {
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "tokless")
	req.Header.Set("Accept", "application/json")
	// Anonymous GitHub allows 60 calls an hour per IP, which a shared office
	// network or CI runner burns through fast.
	if strings.HasPrefix(u, "https://api.github.com/") {
		if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
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
	npmBin := ResolveNpmBinary()
	if npmBin == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, npmBin, "info", pkg+"@latest", "version", "--json")
	c.Env = append(os.Environ(), "NPM_CONFIG_PREFER_OFFLINE=false", "NPM_CONFIG_PREFER_ONLINE=true")
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

// githubHeadSHA is the fallback version for repos with no releases or tags.
func githubHeadSHA(repo string) *string {
	var data []struct {
		Sha string `json:"sha"`
	}
	if !fetchJSON("https://api.github.com/repos/"+repo+"/commits?per_page=1", &data) {
		return nil
	}
	if len(data) == 0 || len(data[0].Sha) < 7 {
		return nil
	}
	return strp(data[0].Sha[:7])
}

// pypiLatest resolves a PyPI package's latest published version.
func pypiLatest(pkg string) *string {
	var data struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if !fetchJSON("https://pypi.org/pypi/"+url.PathEscape(pypiBaseName(pkg))+"/json", &data) {
		return nil
	}
	if data.Info.Version == "" {
		return nil
	}
	return strp(data.Info.Version)
}

// pypiBaseName strips an extras suffix: `headroom-ai[all]` -> `headroom-ai`.
func pypiBaseName(pkg string) string {
	if i := strings.IndexByte(pkg, '['); i > 0 {
		return pkg[:i]
	}
	return pkg
}

var reSemver = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

// binVersion reads a version out of `<tool> --version`.
func binVersion(s VersionSpec) *string {
	p := ""
	if s.Resolve != nil {
		p = s.Resolve()
	}
	if p == "" && s.Bin != "" {
		p = Which(s.Bin)
	}
	if p == "" {
		return nil
	}
	r := Run(p, []string{"--version"}, RunOptions{Capture: true})
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
	npmBin := ResolveNpmBinary()
	if npmBin != "" {
		r := Run(npmBin, []string{"ls", "-g", "--depth=0", "--json", pkg}, RunOptions{Capture: true})
		var j struct {
			Dependencies map[string]struct {
				Version string `json:"version"`
			} `json:"dependencies"`
		}
		if json.Unmarshal([]byte(r.Stdout), &j) == nil {
			if d, ok := j.Dependencies[pkg]; ok && d.Version != "" {
				return strp(d.Version)
			}
		}
		if v := npmPrefixInstalledVersion(npmPrefix(), pkg); v != nil {
			return v
		}
		if v := npmPrefixInstalledVersion(userLocalNpmPrefix(), pkg); v != nil {
			return v
		}
	}
	return bunInstalledVersion(pkg)
}

func NpmInstalledVersionExported(pkg string) *string { return npmInstalledVersion(pkg) }

// bunInstalledVersion resolves a bun-linked bin (e.g. ~/.bun/bin/<pkg>) to its
// package.json and reads the version. Also checks ~/.bun/install/global.
func bunInstalledVersion(pkg string) *string {
	h := Home()

	// 1. Resolve symlink: ~/.bun/bin/<pkg> -> ../../node_modules/<pkg>/...
	binLink := filepath.Join(h, ".bun", "bin", pkg)
	if real, err := filepath.EvalSymlinks(binLink); err == nil && real != "" {
		dir := filepath.Dir(real)
		if v := readPkgVersion(filepath.Join(dir, "package.json")); v != nil {
			return v
		}
	}
	// 2. Bun global install: ~/.bun/install/global/node_modules/<pkg>/package.json
	if v := readPkgVersion(filepath.Join(h, ".bun", "install", "global", "node_modules", pkg, "package.json")); v != nil {
		return v
	}
	return nil
}

func readPkgVersion(pj string) *string {
	b, err := os.ReadFile(pj)
	if err != nil {
		return nil
	}
	var p struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &p) != nil || p.Version == "" {
		return nil
	}
	return strp(p.Version)
}

// GatherVersions returns version info for the given specs, latest cached for 6h.
func GatherVersions(specs []VersionSpec) map[string]VersionInfo {
	return gatherVersions(specs, false)
}

func GatherVersionsForce(specs []VersionSpec) map[string]VersionInfo {
	return gatherVersions(specs, true)
}

// testVersionFixture keeps versions fixed under TOKLESS_TEST=1.
var testVersionFixture = map[string]VersionInfo{
	"rtk":          {Installed: strp("0.43.0"), Latest: strp("0.43.0"), Channel: "github"},
	"codegraph":    {Installed: nil, Latest: strp("1.1.6"), Channel: "npm"},
	"context-mode": {Installed: nil, Latest: strp("1.0.169"), Channel: "npm"},
	"principles":   {Installed: strp("2c60614"), Latest: strp("2c60614"), Channel: "skill"},
	"caveman":      {Installed: strp("1.9.1"), Latest: strp("1.9.1"), Channel: "skill"},
	"ponytail":     {Installed: strp("4.8.4"), Latest: strp("4.8.4"), Channel: "skill"},
	"headroom":     {Installed: nil, Latest: strp("0.33.0"), Channel: "pypi"},
	"projectmem":   {Installed: nil, Latest: strp("0.2.0"), Channel: "pypi"},
}

func gatherVersions(specs []VersionSpec, force bool) map[string]VersionInfo {
	if os.Getenv("TOKLESS_TEST") == "1" {
		out := map[string]VersionInfo{}
		for _, s := range specs {
			if v, ok := testVersionFixture[s.ID]; ok {
				out[s.ID] = v
			}
		}
		return out
	}
	// Latest (slow, network) is cached; installed (fast, local) is always live.
	latest := cachedLatest(specs, force)
	out := make(map[string]VersionInfo, len(specs))
	for _, s := range specs {
		installed := InstalledVersion(s)
		out[s.ID] = VersionInfo{
			Installed: installed,
			Latest:    latest[s.ID],
			Channel:   s.Channel,
			Present:   installed != nil,
		}
	}
	return out
}

// LatestVersionFor returns one tool's latest available version (cached).
func LatestVersionFor(specs []VersionSpec, id string) *string {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return testVersionFixture[id].Latest
	}
	return cachedLatest(specs, false)[id]
}

// InstalledVersion reads one tool's live installed version (nil if absent).
func InstalledVersion(s VersionSpec) *string {
	switch s.Channel {
	case "npm":
		return npmInstalledVersion(s.Pkg)
	case "skill":
		return SkillInstalledVersion(s.ID)
	case "pypi":
		// Not every Python CLI has --version; fall back to package metadata.
		if v := binVersion(s); v != nil {
			return v
		}
		return PyPackageVersion(s.Bin, s.Pkg)
	case "github", "binary":
		return binVersion(s)
	}
	return nil
}

// InstalledPath returns a tool's on-disk path, or "" if missing.
func InstalledPath(s VersionSpec) string {
	switch s.Channel {
	case "npm":
		return npmPkgDir(s.Pkg)
	case "skill":
		return SkillDir(s.ID)
	case "pypi", "github", "binary":
		if s.Resolve != nil {
			if p := s.Resolve(); p != "" {
				return p
			}
		}
		if s.Bin != "" {
			return Which(s.Bin)
		}
	}
	return ""
}

// npmPkgDir finds a global npm/bun package directory.
func npmPkgDir(pkg string) string {
	var dirs []string
	for _, prefix := range []string{npmPrefix(), userLocalNpmPrefix()} {
		if prefix == "" {
			continue
		}
		if IsWin {
			dirs = append(dirs, filepath.Join(prefix, "node_modules", pkg))
		} else {
			dirs = append(dirs, filepath.Join(prefix, "lib", "node_modules", pkg))
		}
	}
	dirs = append(dirs, filepath.Join(Home(), ".bun", "install", "global", "node_modules", pkg))
	for _, d := range dirs {
		if Exists(filepath.Join(d, "package.json")) {
			return d
		}
	}
	return ""
}

var latestFetcher = fetchLatestFor

// fetchLatestFor resolves one tool's latest upstream version (nil on failure).
func fetchLatestFor(s VersionSpec) *string {
	switch s.Channel {
	case "npm":
		return npmLatest(s.Pkg)
	case "github":
		return githubLatestRelease(s.Repo)
	case "pypi":
		return pypiLatest(s.Pkg)
	case "skill":
		return SkillLatest(s)
	}
	return nil
}

// cachedLatest returns the latest-version lookups, cached to disk (6h TTL).
func cachedLatest(specs []VersionSpec, force bool) map[string]*string {
	if os.Getenv("TOKLESS_TEST") == "1" {
		m := map[string]*string{}
		for _, s := range specs {
			m[s.ID] = testVersionFixture[s.ID].Latest
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
	var todo []VersionSpec
	for _, s := range specs {
		if result[s.ID] != nil && fresh && !force {
			continue
		}
		todo = append(todo, s)
	}
	fetched := false
	if len(todo) > 0 {
		var wg sync.WaitGroup
		var mu sync.Mutex
		got := make(map[string]*string, len(todo))
		for _, s := range todo {
			wg.Add(1)
			go func(s VersionSpec) {
				defer wg.Done()
				if v := latestFetcher(s); v != nil {
					mu.Lock()
					got[s.ID] = v
					mu.Unlock()
				}
			}(s)
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

var reSemverFull = regexp.MustCompile(`^v?\d+\.\d+`)

// IsSemver reports whether a version string is orderable as a semver.
// Commit SHAs (the only version signal for repos without releases) are not.
func IsSemver(v string) bool { return reSemverFull.MatchString(v) }

// VersionOutdated compares semver when both sides parse as semver, and falls
// back to plain inequality otherwise — SemverCompare reads every SHA as 0.0.0
// and would call each one equal to the next.
func VersionOutdated(installed, latest *string) bool {
	if installed == nil || latest == nil {
		return false
	}
	if IsSemver(*installed) && IsSemver(*latest) {
		return SemverCompare(installed, latest) < 0
	}
	return *installed != *latest
}

func CountOutdated(m map[string]VersionInfo) int {
	n := 0
	for _, v := range m {
		if VersionOutdated(v.Installed, v.Latest) {
			n++
		}
	}
	return n
}

func BustVersionCache() {
	_ = os.Remove(cachePath())
}
