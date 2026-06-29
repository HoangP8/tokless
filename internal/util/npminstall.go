package util

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultRegistryURL = "https://registry.npmjs.org/"

// npmRegistryBase returns the user's configured npm registry.
func npmRegistryBase() string {
	npmBin := ResolveNpmBinary()
	if npmBin != "" {
		r := Run(npmBin, []string{"config", "get", "registry"}, RunOptions{Capture: true})
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
	npmBin := ResolveNpmBinary()
	if npmBin == "" {
		return ExecResult{Code: 127, Stderr: "npm not found"}
	}
	return Run(npmBin, args, RunOptions{Capture: true})
}
var npmRunEnv = func(args, env []string) ExecResult {
	npmBin := ResolveNpmBinary()
	if npmBin == "" {
		return ExecResult{Code: 127, Stderr: "npm not found"}
	}
	return Run(npmBin, args, RunOptions{Capture: true, Env: env})
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
// On exhaustion returns ("", false, nil) — never crashes, logs each attempt.
func NpmGlobalInstall(pkg, spec string) (string, bool, error) {
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

	for i, args := range buildNpmAttempts(pkg, resolvedVersion, tarball, cacheDir) {
		r := npmRun(args)
		if r.Code != 0 {
			L.Debug("npm attempt " + strconv.Itoa(i+1) + " failed: " + firstNpmLine(r.Stderr, r.Stdout))
			continue
		}
		actual := npmReadInstalled(pkg)
		if v, good := installSucceeded(target, actual); good {
			ensureNpmGlobalBinOnPath()
			return v, true, nil
		}
		L.Debug("npm attempt " + strconv.Itoa(i+1) + " exit 0 but package not installed")
	}

	// Final fallback: user-local prefix.
	token := pkg + "@" + spec
	if resolvedVersion != "" {
		token = pkg + "@" + resolvedVersion
	}
	if v, ok := npmUserPrefixInstall(pkg, token, cacheDir); ok {
		return v, true, nil
	}
	return "", false, nil
}

func firstNpmLine(stderr, stdout string) string {
	for _, s := range []string{stderr, stdout} {
		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimSpace(l)
			if l != "" {
				return l
			}
		}
	}
	return "no output"
}

func userLocalNpmPrefix() string {
	for _, k := range []string{"npm_config_prefix", "NPM_CONFIG_PREFIX"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return filepath.Join(Home(), ".local")
}

func npmPrefixInstalledVersion(prefix, pkg string) *string {
	pj := filepath.Join(prefix, "lib", "node_modules", pkg, "package.json")
	if IsWin {
		pj = filepath.Join(prefix, "node_modules", pkg, "package.json")
	}
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
	return &p.Version
}

func npmUserPrefixInstall(pkg, token, cacheDir string) (string, bool) {
	prefix := userLocalNpmPrefix()
	if prefix == "" {
		return "", false
	}
	seed := filepath.Join(prefix, "lib")
	if IsWin {
		seed = prefix
	}
	if EnsureDir(seed) != nil {
		return "", false
	}
	args := []string{"install", "-g", token, "--no-audit", "--no-fund"}
	if cacheDir != "" {
		args = append(args, "--cache", cacheDir)
	}
	if r := npmRunEnv(args, []string{"npm_config_prefix=" + prefix}); r.Code != 0 {
		return "", false
	}
	v := npmPrefixInstalledVersion(prefix, pkg)
	if v == nil {
		return "", false
	}
	PrependProcessPath(npmGlobalBinDir(prefix, IsWin))
	return *v, true
}

var npmPrefix = func() string {
	npmBin := ResolveNpmBinary()
	if npmBin == "" {
		return ""
	}
	r := Run(npmBin, []string{"config", "get", "prefix"}, RunOptions{Capture: true})
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

// NodeMajor returns the installed Node.js major version, or 0 if unknown.
func NodeMajor() int {
	nodeBin := ResolveNodeBinary()
	if nodeBin == "" {
		return 0
	}
	r := Run(nodeBin, []string{"--version"}, RunOptions{Capture: true})
	v := strings.TrimPrefix(strings.TrimSpace(r.Stdout), "v")
	if i := strings.IndexByte(v, '.'); i > 0 {
		v = v[:i]
	}
	n, _ := strconv.Atoi(v)
	return n
}

// NodeTooOldHint returns an actionable message when Node is older than min.
func NodeTooOldHint(min int) string {
	if maj := NodeMajor(); maj > 0 && maj < min {
		return "Node.js v" + strconv.Itoa(maj) + " is too old (need v" + strconv.Itoa(min) +
			"+). Upgrade without sudo via nvm (https://github.com/nvm-sh/nvm), " +
			"or: curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt-get install -y nodejs"
	}
	return ""
}
