package util

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultRegistryURL = "https://registry.npmjs.org/"

// npmRegistryBase returns the user's configured npm registry (honoring mirrors
// in .npmrc), falling back to the public registry when npm can't be queried.
func npmRegistryBase() string {
	if Which("npm") != "" {
		r := Run("npm", []string{"config", "get", "registry"}, RunOptions{Capture: true})
		v := strings.TrimSpace(r.Stdout)
		if r.Code == 0 && strings.HasPrefix(v, "http") {
			if !strings.HasSuffix(v, "/") {
				v += "/"
			}
			return v
		}
	}
	return defaultRegistryURL
}

type registryDoc struct {
	DistTags map[string]string `json:"dist-tags"`
	Versions map[string]struct {
		Version string `json:"version"`
		Dist    *struct {
			Tarball string `json:"tarball"`
		} `json:"dist"`
	} `json:"versions"`
}

// resolveFromRegistry resolves the version and tarball URL for a package spec.
func resolveFromRegistry(pkg, spec string) (string, string, bool) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, npmRegistryBase()+url.QueryEscape(pkg), nil)
	if err != nil {
		return "", "", false
	}
	req.Header.Set("User-Agent", "tokless")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", false
	}
	var doc registryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", "", false
	}
	version, ok := doc.DistTags[spec]
	if !ok {
		if _, exists := doc.Versions[spec]; exists {
			version = spec
		} else {
			return "", "", false
		}
	}
	vInfo, exists := doc.Versions[version]
	if !exists || vInfo.Dist == nil || vInfo.Dist.Tarball == "" {
		return "", "", false
	}
	return version, vInfo.Dist.Tarball, true
}

var npmResolve = resolveFromRegistry
var npmRun = func(args []string) ExecResult {
	return Run("npm", args, RunOptions{Capture: true})
}
var npmReadInstalled = func(pkg string) *string {
	return npmInstalledVersion(pkg)
}

// buildNpmAttempts orders install attempts strongest-first.
func buildNpmAttempts(pkg, resolvedVersion, tarball, cacheDir string) [][]string {
	token := pkg + "@latest"
	if resolvedVersion != "" {
		token = pkg + "@" + resolvedVersion
	}
	online := []string{"--prefer-online"}
	if cacheDir != "" {
		online = append(online, "--cache", cacheDir)
	}
	attempts := [][]string{
		append([]string{"install", "-g", token}, online...),
	}
	if tarball != "" {
		attempts = append(attempts, append([]string{"install", "-g", tarball}, online...))
	}
	attempts = append(attempts, []string{"install", "-g", token})
	// Last resort for broken/empty local registry configs: force the public
	// registry. Tried last so a configured mirror/proxy always wins first.
	attempts = append(attempts, append([]string{"install", "-g", token, "--registry", defaultRegistryURL}, online...))
	return attempts
}

func installSucceeded(target string, actual *string) (string, bool) {
	if actual == nil {
		return "", false
	}
	if target == "" || *actual == target {
		return *actual, true
	}
	return "", false
}

// NpmGlobalInstall installs an npm package globally.
func NpmGlobalInstall(pkg, spec string) (string, bool) {
	if spec == "" {
		spec = "latest"
	}

	resolvedVersion, tarball, ok := npmResolve(pkg, spec)
	target := resolvedVersion
	if !ok {
		if spec != "latest" {
			target = spec
		}
	}

	cacheDir := freshCacheDir()
	if cacheDir != "" {
		defer cleanupDir(cacheDir)
	}

	for _, args := range buildNpmAttempts(pkg, resolvedVersion, tarball, cacheDir) {
		r := npmRun(args)
		if r.Code != 0 {
			continue
		}
		actual := npmReadInstalled(pkg)
		if v, good := installSucceeded(target, actual); good {
			ensureNpmGlobalBinOnPath()
			return v, true
		}
	}
	return "", false
}

var npmPrefix = func() string {
	r := Run("npm", []string{"config", "get", "prefix"}, RunOptions{Capture: true})
	if r.Code != 0 {
		return ""
	}
	return strings.TrimSpace(r.Stdout)
}

func ensureNpmGlobalBinOnPath() {
	prefix := npmPrefix()
	if prefix == "" {
		return
	}
	PrependProcessPath(npmGlobalBinDir(prefix, IsWin))
}

// npmGlobalBinDir maps an npm prefix to its global bin dir.
func npmGlobalBinDir(prefix string, win bool) string {
	if win {
		return prefix
	}
	return filepath.Join(prefix, "bin")
}

func freshCacheDir() string {
	dir, err := os.MkdirTemp("", "tokless-npm-*")
	if err != nil {
		return ""
	}
	return dir
}

func cleanupDir(dir string) {
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}
