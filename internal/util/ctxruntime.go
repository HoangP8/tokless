package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const (
	ctxMinNode1x = "22.5.0"  // context-mode 1.x
	ctxMinNode2x = "24.15.0" // context-mode 2.x
)

// CtxRuntime describes how to launch the installed server.
type CtxRuntime struct {
	Node, CLI, Version, MinNode string
	Degraded                    bool
	Major2                      bool
}

// ctxMinNodeDefault is the Node floor when engines.node is absent.
func ctxMinNodeDefault(major2 bool) string {
	if major2 {
		return ctxMinNode2x
	}
	return ctxMinNode1x
}

func VersionMajor(v string) int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '.'); i > 0 {
		v = v[:i]
	}
	n, _ := strconv.Atoi(v)
	return n
}

// enginesMin extracts the first semver of an engines range (">=24.15.0" -> "24.15.0").
func enginesMin(rng string) string {
	if m := reSemver.FindStringSubmatch(rng); m != nil {
		return m[1]
	}
	return ""
}

func NodeFullVersion(bin string) string {
	if bin == "" {
		return ""
	}
	r := Run(bin, []string{"--version"}, RunOptions{Capture: true})
	if r.Code != 0 {
		return ""
	}
	if m := reSemver.FindStringSubmatch(strings.TrimSpace(r.Stdout)); m != nil {
		return m[1]
	}
	return ""
}

func NodeVersionOK(bin, min string) bool {
	v := NodeFullVersion(bin)
	return v != "" && SemverGte(v, min)
}

// PortableRuntimeDir is tokless's private Node dir, separate from the system-Node installer's.
func PortableRuntimeDir() string {
	if IsWin {
		return filepath.Join(filepath.Dir(nodeInstallDir()), "node-runtime")
	}
	return filepath.Join(filepath.Dir(nodeUnixInstallDir()), "node-runtime")
}

func nodeBinIn(dir string) string {
	if IsWin {
		return filepath.Join(dir, "node.exe")
	}
	return filepath.Join(dir, "bin", "node")
}

func portableNodeBin() string { return nodeBinIn(PortableRuntimeDir()) }

func oldNodeInstallDir() string {
	if IsWin {
		return nodeInstallDir()
	}
	return nodeUnixInstallDir()
}

// findNodeAtLeast returns the first Node >= min from PATH, the portable dir, the old installer dir, or version managers.
func findNodeAtLeast(min string) string {
	if p := ResolveNodeBinary(); NodeVersionOK(p, min) {
		return p
	}
	if p := portableNodeBin(); Exists(p) && NodeVersionOK(p, min) {
		return p
	}
	if p := nodeBinIn(oldNodeInstallDir()); Exists(p) && NodeVersionOK(p, min) {
		return p
	}
	exe := "node"
	if IsWin {
		exe = "node.exe"
	}
	for _, d := range nodeCandidateDirs() {
		if p := filepath.Join(d, exe); Exists(p) && NodeVersionOK(p, min) {
			return p
		}
	}
	return ""
}

// EnsureNodeAtLeast returns a Node >= min, downloading a portable one only when install is set.
func EnsureNodeAtLeast(min string, install bool) (string, error) {
	if p := findNodeAtLeast(min); p != "" {
		return p, nil
	}
	if !install {
		return "", errors.New("node >= " + min + " required")
	}
	return EnsurePortableNode(min)
}

// Seams for tests.
var (
	nodeCandidateDirs = nodeBinCandidates
	nodeIndexFetcher  = fetchNodeIndex
	nodeDownload      = downloadToTemp
	nodeShasums       = fetchNodeShasums
	renameFn          = os.Rename
)

func fetchNodeShasums(url string) (string, error) {
	p, err := downloadToTemp(url)
	if err != nil {
		return "", err
	}
	defer os.Remove(p)
	b, err := os.ReadFile(p)
	return string(b), err
}

// pickNodeRelease picks the newest release >= min shipping token, preferring LTS.
func pickNodeRelease(entries []nodeDistEntry, token, min string) (string, bool) {
	fallback := ""
	for _, e := range entries {
		v := strings.TrimPrefix(e.Version, "v")
		if !SemverGte(v, min) || !hasString(e.Files, token) {
			continue
		}
		if lts := strings.TrimSpace(string(e.LTS)); lts != "" && lts != "false" && lts != "null" {
			return e.Version, true
		}
		if fallback == "" {
			fallback = e.Version
		}
	}
	return fallback, fallback != ""
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// EnsurePortableNode installs Node >= min into PortableRuntimeDir only.
func EnsurePortableNode(min string) (string, error) {
	if p := portableNodeBin(); Exists(p) && NodeVersionOK(p, min) {
		return p, nil
	}
	var token, archive, dest string
	arch := nodeUnixArch()
	if IsWin {
		arch = nodeWinArch()
	}
	if arch == "" {
		return "", errors.New("unsupported architecture for Node install: " + runtime.GOARCH)
	}
	if IsWin {
		token = "win-" + arch + "-zip"
	} else {
		token, _ = nodeUnixDist(runtime.GOOS, arch)
	}
	dest = PortableRuntimeDir()
	entries, ok := nodeIndexFetcher()
	if !ok {
		return "", errors.New("couldn't fetch the Node.js release index")
	}
	v, ok := pickNodeRelease(entries, token, min)
	if !ok {
		return "", errors.New("no Node.js release >= " + min + " for " + token)
	}
	base := "https://nodejs.org/dist/" + v + "/"
	if IsWin {
		archive = "node-" + v + "-win-" + arch + ".zip"
	} else {
		_, fileBase, _ := nodeUnixArtifact(runtime.GOOS, arch, v)
		archive = fileBase + ".tar.gz"
	}
	L.Info("Downloading Node.js " + v + " from nodejs.org…")
	tmp, err := nodeDownload(base + archive)
	if err != nil {
		return "", errors.New("Node download failed: " + err.Error())
	}
	defer os.Remove(tmp)
	sums, err := nodeShasums(base + "SHASUMS256.txt")
	if err != nil {
		return "", errors.New("Node checksum fetch failed: " + err.Error())
	}
	if err := verifySha256(tmp, archive, sums); err != nil {
		return "", err
	}
	stage, old := dest+".new", dest+".old"
	_ = os.RemoveAll(stage)
	_ = os.RemoveAll(old)
	if IsWin {
		err = extractZipStripRoot(tmp, stage)
	} else {
		err = extractTarGzStripRoot(tmp, stage)
	}
	if err != nil {
		_ = os.RemoveAll(stage)
		return "", errors.New("Node extract failed: " + err.Error())
	}
	if !NodeVersionOK(nodeBinIn(stage), min) {
		_ = os.RemoveAll(stage)
		return "", errors.New("downloaded Node.js is not usable or older than " + min)
	}
	hadOld := Exists(dest)
	if hadOld {
		if err := renameFn(dest, old); err != nil {
			_ = os.RemoveAll(stage)
			return "", err
		}
	}
	if err := renameFn(stage, dest); err != nil {
		if hadOld {
			_ = renameFn(old, dest)
		}
		_ = os.RemoveAll(stage)
		return "", err
	}
	_ = os.RemoveAll(old)
	return portableNodeBin(), nil
}

func verifySha256(file, name, sums string) error {
	want := ""
	for _, l := range strings.Split(sums, "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[1] == name {
			want = f[0]
		}
	}
	if want == "" {
		return errors.New("no checksum listed for " + name)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return errors.New("checksum mismatch for " + name)
	}
	return nil
}

// ctxPkgDir locates the installed package: via the PATH binary, else npm/bun globals (test seam).
var ctxPkgDir = func() string {
	if d := ctxPkgDirFromPath(); d != "" {
		return d
	}
	return npmPkgDir("context-mode")
}

func ctxPkgDirFromPath() string {
	bin := Which("context-mode")
	if bin == "" {
		return ""
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(real)
	for i := 0; i < 5; i++ {
		if raw, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
			var pkg struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(raw, &pkg) == nil && pkg.Name == "context-mode" {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func ResolveContextMode() (CtxRuntime, error) {
	dir := ctxPkgDir()
	if dir == "" {
		return CtxRuntime{}, errors.New("context-mode is not installed")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return CtxRuntime{}, err
	}
	var pkg struct {
		Version string          `json:"version"`
		Bin     json.RawMessage `json:"bin"`
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return CtxRuntime{}, err
	}
	rt := CtxRuntime{Version: pkg.Version, Major2: VersionMajor(pkg.Version) >= 2}
	rt.MinNode = enginesMin(pkg.Engines.Node)
	if rt.MinNode == "" {
		rt.MinNode = ctxMinNodeDefault(rt.Major2)
	}
	rel := ctxBinTarget(pkg.Bin)
	if rel == "" && rt.Major2 {
		rel = "cli.mjs"
	}
	if rel == "" {
		return CtxRuntime{}, errors.New("context-mode package.json has no bin")
	}
	rt.CLI = filepath.Join(dir, rel)
	if rt.Node = findNodeAtLeast(rt.MinNode); rt.Node == "" {
		rt.Node = ResolveNodeBinary()
		rt.Degraded = true
	}
	return rt, nil
}

func ctxBinTarget(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil {
		if p := m["context-mode"]; p != "" {
			return p
		}
		for _, p := range m {
			return p
		}
	}
	return ""
}

// ctxLatest caches the registry lookup per process.
var ctxLatest struct {
	sync.Mutex
	done         bool
	ver, minNode string
	err          error
}

var errCtxLatest = errors.New("couldn't resolve latest context-mode from the npm registry")

func ContextModeLatestVersion() (string, error) {
	v, _, err := ContextModeLatest()
	return v, err
}

func ContextModeLatest() (version, minNode string, err error) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return "1.0.169", "22.5.0", nil
	}
	ctxLatest.Lock()
	defer ctxLatest.Unlock()
	if !ctxLatest.done {
		ctxLatest.done = true
		ctxLatest.ver, ctxLatest.minNode, ctxLatest.err = fetchContextModeLatest()
	}
	return ctxLatest.ver, ctxLatest.minNode, ctxLatest.err
}

// ctxMaxMajor caps what tokless installs or pins; newer majors are not supported yet.
const ctxMaxMajor = 2

// Test seam.
var npmViewFieldsFn = npmViewFields

func fetchContextModeLatest() (version, minNode string, err error) {
	f := npmViewFieldsFn("context-mode@latest", "version", "engines.node")
	if m := reSemver.FindStringSubmatch(f["version"]); m != nil {
		version = m[1]
		minNode = enginesMin(f["engines.node"])
	} else if v := npmLatest("context-mode"); v != nil {
		version = *v
	} else {
		return "", "", errCtxLatest
	}
	if VersionMajor(version) > ctxMaxMajor {
		// "context-mode@2" matches many versions: npm prints a JSON array in no fixed order.
		version = highestSemver(npmViewFieldsFn("context-mode@"+strconv.Itoa(ctxMaxMajor), "version")["version"])
		if version == "" {
			return "", "", errCtxLatest
		}
		minNode = enginesMin(npmViewFieldsFn("context-mode@"+version, "engines.node")["engines.node"])
	}
	if minNode == "" {
		minNode = ctxMinNodeDefault(VersionMajor(version) >= 2)
	}
	return version, minNode, nil
}

// highestSemver reads a JSON string or array of versions and returns the highest valid one.
func highestSemver(raw string) string {
	vs := []string{strings.Trim(strings.TrimSpace(raw), `"`)}
	var arr []string
	if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
		vs = arr
	}
	best := ""
	for _, v := range vs {
		m := reSemver.FindStringSubmatch(v)
		if m != nil && (best == "" || SemverCompare(&m[1], &best) > 0) {
			best = m[1]
		}
	}
	return best
}
