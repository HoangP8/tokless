package util

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	return filepath.Join(resolveHome(), ".cache", "tokless", "versions.json")
}

const cacheTTL = 6 * time.Hour

func loadCache() *cacheShape {
	b, err := os.ReadFile(cachePath())
	if err != nil {
		return nil
	}
	var obj cacheShape
	if json.Unmarshal(b, &obj) != nil {
		return nil
	}
	if time.Since(time.UnixMilli(obj.Ts)) > cacheTTL {
		return nil
	}
	return &obj
}

func saveCache(m map[string]VersionInfo) {
	_ = os.MkdirAll(filepath.Dir(cachePath()), 0o755)
	b, _ := json.MarshalIndent(cacheShape{Ts: time.Now().UnixMilli(), Map: m}, "", "  ")
	_ = os.WriteFile(cachePath(), b, 0o644)
}

func fetchJSON(u string, out any) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(u)
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
	var data struct {
		DistTags struct {
			Latest string `json:"latest"`
		} `json:"dist-tags"`
	}
	if !fetchJSON("https://registry.npmjs.org/"+url.QueryEscape(pkg), &data) {
		return nil
	}
	if data.DistTags.Latest == "" {
		return nil
	}
	return strp(data.DistTags.Latest)
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
func GatherVersions() map[string]VersionInfo {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return map[string]VersionInfo{
			"rtk":          {Installed: strp("0.40.0"), Latest: strp("0.40.0"), Channel: "github"},
			"caveman":      {Installed: nil, Latest: strp("1.0.0"), Channel: "github"},
			"codegraph":    {Installed: nil, Latest: strp("0.9.0"), Channel: "npm"},
			"context-mode": {Installed: nil, Latest: strp("1.0.0"), Channel: "npm"},
			"tokless":      {Installed: strp("0.1.0"), Latest: strp("0.1.0"), Channel: "npm"},
		}
	}
	if c := loadCache(); c != nil {
		return c.Map
	}
	out := map[string]VersionInfo{}
	out["rtk"] = VersionInfo{Installed: rtkInstalledVersion(), Latest: githubLatestRelease("rtk-ai/rtk"), Channel: "github"}
	out["caveman"] = VersionInfo{Installed: nil, Latest: githubLatestRelease("JuliusBrussee/caveman"), Channel: "github"}
	out["codegraph"] = VersionInfo{Installed: npmInstalledVersion("@colbymchenry/codegraph"), Latest: npmLatest("@colbymchenry/codegraph"), Channel: "npm"}
	out["context-mode"] = VersionInfo{Installed: npmInstalledVersion("context-mode"), Latest: npmLatest("context-mode"), Channel: "npm"}
	out["tokless"] = VersionInfo{Installed: npmInstalledVersion("tokless"), Latest: npmLatest("tokless"), Channel: "npm"}
	saveCache(out)
	return out
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
