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
	resp, err := client.Get(registryURL + url.QueryEscape(pkg))
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

// NpmGlobalInstall installs an npm package globally.
func NpmGlobalInstall(pkg, spec string) (string, bool) {
	if spec == "" {
		spec = "latest"
	}
	var target string
	var tarball string
	resolvedVersion, tb, ok := resolveFromRegistry(pkg, spec)
	if ok {
		target = resolvedVersion
		tarball = tb
	} else {
		target = spec
	}

	atSpec := pkg + "@" + spec
	cacheDir := freshCacheDir()

	attempts := [][]string{
		{"install", "-g", atSpec},
		{"install", "-g", atSpec, "--prefer-online"},
		{"install", "-g", atSpec, "--registry", registryURL, "--cache", cacheDir, "--prefer-online"},
	}
	if ok && tarball != "" {
		attempts = append(attempts, []string{"install", "-g", tarball})
	}

	for _, args := range attempts {
		r := Run("npm", args, RunOptions{Capture: true})
		// Clean up the cache directory if this attempt used it.
		var hasCache bool
		for _, arg := range args {
			if arg == cacheDir {
				hasCache = true
				break
			}
		}
		if hasCache {
			cleanupDir(cacheDir)
		}
		if r.Code == 0 {
			return target, true
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
