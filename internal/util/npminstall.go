package util

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"time"
)

const registryURL = "https://registry.npmjs.org/"

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
	req, err := http.NewRequest(http.MethodGet, registryURL+url.QueryEscape(pkg), nil)
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
		append([]string{"install", "-g", token, "--registry", registryURL}, online...),
	}
	if tarball != "" {
		attempts = append(attempts, append([]string{"install", "-g", tarball}, online...))
	}
	attempts = append(attempts, []string{"install", "-g", token})
	return attempts
}

func installSucceeded(target string, actual *string) (string, bool) {
	if actual == nil {
		return target, true
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
			return v, true
		}
	}
	return "", false
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
