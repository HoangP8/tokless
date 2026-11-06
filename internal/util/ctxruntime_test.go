package util

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeNode(t *testing.T, path, version string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake node is unix only")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '"+version+"\\r\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestVersionHelpers(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		ver, min string
		want     bool
	}{
		{"v24.12.0", "24.15.0", false},
		{"v24.15.0", "24.15.0", true},
		{"v25.0.1", "24.15.0", true},
		{"v22.4.9", "22.5.0", false},
	} {
		bin := filepath.Join(dir, "node")
		fakeNode(t, bin, c.ver)
		if got := NodeVersionOK(bin, c.min); got != c.want {
			t.Errorf("NodeVersionOK(%s, %s) = %v; want %v", c.ver, c.min, got, c.want)
		}
	}
	if v := NodeFullVersion(filepath.Join(dir, "node")); v != "22.4.9" {
		t.Errorf("NodeFullVersion = %q; want 22.4.9", v)
	}
	if NodeVersionOK(filepath.Join(dir, "missing"), "1.0.0") {
		t.Error("missing binary must not be OK")
	}
	if enginesMin(">=24.15.0") != "24.15.0" || enginesMin("") != "" {
		t.Error("enginesMin wrong")
	}
	if VersionMajor("2.0.2") != 2 || VersionMajor("v1.0.169") != 1 || VersionMajor("x") != 0 {
		t.Error("VersionMajor wrong")
	}
	if !isEngineError("npm warn EBADENGINE Unsupported engine") || isEngineError("ETIMEDOUT") {
		t.Error("isEngineError wrong")
	}
}

func ctxFixture(t *testing.T, pkgJSON, pathNode string) (pkgDir, home string) {
	t.Helper()
	home = t.TempDir()
	orig := homeOverride
	SetHomeOverride(home)
	pkgDir = filepath.Join(t.TempDir(), "context-mode")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	origDir := ctxPkgDir
	ctxPkgDir = func() string { return pkgDir }
	pathDir := filepath.Join(t.TempDir(), "bin")
	if pathNode != "" {
		fakeNode(t, filepath.Join(pathDir, "node"), pathNode)
	}
	t.Setenv("PATH", pathDir)
	origDirs := nodeCandidateDirs
	nodeCandidateDirs = func() []string { // ignore the host's system-wide Node
		var dirs []string
		for _, d := range origDirs() {
			if strings.HasPrefix(d, home) {
				dirs = append(dirs, d)
			}
		}
		return dirs
	}
	t.Cleanup(func() { SetHomeOverride(orig); ctxPkgDir = origDir; nodeCandidateDirs = origDirs })
	return pkgDir, home
}

func TestResolveContextMode(t *testing.T) {
	for _, tc := range []struct {
		name, pkg, pathNode string
		oldDirNode          bool // also place a v24.15.0 Node in the old installer dir
		major2, degraded    bool
		minNode, cli        string
	}{
		{"1.x", `{"version":"1.0.169","bin":{"context-mode":"./build/cli.js"},"engines":{"node":">=22.5.0"}}`, "v22.5.0", false, false, false, "22.5.0", "build/cli.js"},
		{"2.x", `{"version":"2.0.2","bin":{"context-mode":"cli.mjs"},"engines":{"node":">=24.15.0"}}`, "v24.15.0", false, true, false, "24.15.0", "cli.mjs"},
		{"2.x defaults min Node and cli.mjs", `{"version":"2.0.2"}`, "v24.15.0", false, true, false, "24.15.0", "cli.mjs"},
		{"old Node degrades", `{"version":"2.0.2","engines":{"node":">=24.15.0"}}`, "v24.12.0", false, true, true, "24.15.0", "cli.mjs"},
		{"falls back to installer-dir Node", `{"version":"2.0.2","engines":{"node":">=24.15.0"}}`, "v22.0.0", true, true, false, "24.15.0", "cli.mjs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, _ := ctxFixture(t, tc.pkg, tc.pathNode)
			oldNode := filepath.Join(nodeUnixInstallDir(), "bin", "node")
			if tc.oldDirNode {
				fakeNode(t, oldNode, "v24.15.0")
			}
			rt, err := ResolveContextMode()
			if err != nil {
				t.Fatal(err)
			}
			if rt.Major2 != tc.major2 || rt.Degraded != tc.degraded || rt.MinNode != tc.minNode || rt.CLI != filepath.Join(dir, tc.cli) || rt.Node == "" {
				t.Errorf("unexpected runtime: %+v", rt)
			}
			if tc.oldDirNode && rt.Node != oldNode {
				t.Errorf("Node = %q; want %q", rt.Node, oldNode)
			}
		})
	}

	orig := ctxPkgDir
	ctxPkgDir = func() string { return "" }
	defer func() { ctxPkgDir = orig }()
	if _, err := ResolveContextMode(); err == nil {
		t.Error("expected error when not installed")
	}
}

func TestResolveContextModePrefersPathBinaryPackage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	pkg := filepath.Join(t.TempDir(), "lib", "node_modules", "context-mode")
	os.MkdirAll(pkg, 0o755)
	os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"context-mode","version":"2.0.2","bin":{"context-mode":"cli.mjs"}}`), 0o644)
	os.WriteFile(filepath.Join(pkg, "cli.mjs"), nil, 0o644)
	binDir := filepath.Join(t.TempDir(), "bin")
	os.MkdirAll(binDir, 0o755)
	if err := os.Symlink(filepath.Join(pkg, "cli.mjs"), filepath.Join(binDir, "context-mode")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	real, _ := filepath.EvalSymlinks(pkg)
	if got := ctxPkgDirFromPath(); got != real {
		t.Errorf("ctxPkgDirFromPath = %q; want %q", got, real)
	}
	orig := npmPrefix
	npmPrefix = func() string { return "" }
	defer func() { npmPrefix = orig }()
	if got := ctxPkgDir(); got != real {
		t.Errorf("ctxPkgDir = %q; want %q", got, real)
	}
}

func TestFindNodeAtLeast(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(home string) string
	}{
		{"old installer dir", func(string) string { return filepath.Join(nodeUnixInstallDir(), "bin", "node") }},
		{"version manager", func(home string) string {
			return filepath.Join(home, ".nvm", "versions", "node", "v24.15.0", "bin", "node")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, home := ctxFixture(t, `{"version":"2.0.2"}`, "v22.0.0")
			want := tc.path(home)
			fakeNode(t, want, "v24.15.0")
			if got := findNodeAtLeast("24.15.0"); got != want {
				t.Errorf("got %q; want %q", got, want)
			}
		})
	}
}

func TestEnsureNodeAtLeast(t *testing.T) {
	ctxFixture(t, `{"version":"2.0.2"}`, "v22.0.0")
	if bin, err := EnsureNodeAtLeast("22.0.0", false); err != nil || bin == "" {
		t.Errorf("bin=%q err=%v", bin, err)
	}
	called := false
	oi := nodeIndexFetcher
	nodeIndexFetcher = func() ([]nodeDistEntry, bool) { called = true; return nil, false }
	defer func() { nodeIndexFetcher = oi }()
	if _, err := EnsureNodeAtLeast("24.15.0", false); err == nil || err.Error() != "node >= 24.15.0 required" || called {
		t.Errorf("install=false must not download: err=%v called=%v", err, called)
	}
	if _, err := EnsureNodeAtLeast("24.15.0", true); err == nil || !called {
		t.Errorf("install=true must attempt the portable install: err=%v called=%v", err, called)
	}
}

func TestContextModeLatestStub(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	v, m, err := ContextModeLatest()
	if v != "1.0.169" || m != "22.5.0" || err != nil {
		t.Errorf("got %q %q %v", v, m, err)
	}
}

func TestPickNodeRelease(t *testing.T) {
	lts, no := json.RawMessage(`"Krypton"`), json.RawMessage(`false`)
	es := []nodeDistEntry{
		{Version: "v25.1.0", Files: []string{"linux-x64"}, LTS: no},
		{Version: "v24.15.1", Files: []string{"linux-x64"}, LTS: lts},
		{Version: "v24.12.0", Files: []string{"linux-x64"}, LTS: lts},
	}
	if v, ok := pickNodeRelease(es, "linux-x64", "24.15.0"); !ok || v != "v24.15.1" {
		t.Errorf("got %q %v", v, ok)
	}
	if v, ok := pickNodeRelease(es[:1], "linux-x64", "24.15.0"); !ok || v != "v25.1.0" {
		t.Errorf("non-LTS fallback: got %q %v", v, ok)
	}
	if _, ok := pickNodeRelease(es, "linux-x64", "26.0.0"); ok {
		t.Error("nothing >= 26 should match")
	}
}

func portableFixture(t *testing.T) (home string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tarball path is unix only")
	}
	arch := nodeUnixArch()
	if arch == "" {
		t.Skip("unsupported arch")
	}
	home = t.TempDir()
	orig := homeOverride
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride(orig) })
	t.Setenv("PATH", t.TempDir())

	archive := filepath.Join(t.TempDir(), "node.tgz")
	f, _ := os.Create(archive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\nprintf 'v24.15.1\\n'\n"
	tw.WriteHeader(&tar.Header{Name: "node-v24.15.1/bin/node", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write([]byte(body))
	tw.Close()
	gz.Close()
	f.Close()
	raw, _ := os.ReadFile(archive)
	sum := sha256.Sum256(raw)
	_, fileBase, _ := nodeUnixArtifact(runtime.GOOS, arch, "v24.15.1")
	token, _ := nodeUnixDist(runtime.GOOS, arch)

	oi, od, os_, orn := nodeIndexFetcher, nodeDownload, nodeShasums, renameFn
	t.Cleanup(func() { nodeIndexFetcher, nodeDownload, nodeShasums, renameFn = oi, od, os_, orn })
	nodeIndexFetcher = func() ([]nodeDistEntry, bool) {
		return []nodeDistEntry{{Version: "v24.15.1", Files: []string{token}, LTS: json.RawMessage(`"x"`)}}, true
	}
	nodeDownload = func(string) (string, error) {
		c := filepath.Join(t.TempDir(), "dl")
		return c, os.WriteFile(c, raw, 0o644)
	}
	nodeShasums = func(string) (string, error) {
		return hex.EncodeToString(sum[:]) + "  " + fileBase + ".tar.gz\n", nil
	}
	return home
}

// Install touches only the private dir.
func TestEnsurePortableNodeIsolated(t *testing.T) {
	home := portableFixture(t)
	oldNode := filepath.Join(nodeUnixInstallDir(), "bin", "node")
	fakeNode(t, oldNode, "v20.0.0")

	bin, err := EnsurePortableNode("24.15.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "share", "tokless", "node-runtime", "bin", "node"); bin != want {
		t.Errorf("bin = %q; want %q", bin, want)
	}
	if NodeFullVersion(oldNode) != "20.0.0" {
		t.Error("old installer dir must survive")
	}
	dest := PortableRuntimeDir()
	if Exists(dest+".new") || Exists(dest+".old") {
		t.Error("staging dirs must be cleaned up")
	}
	for _, p := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npmrc")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s must not be created", p)
		}
	}
}

func TestEnsurePortableNodeFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject func(dest string)
		check  func(t *testing.T, dest string)
	}{
		{"checksum mismatch installs nothing", func(string) {
			nodeShasums = func(string) (string, error) { return "00  x.tar.gz\n", nil }
		}, func(t *testing.T, dest string) {
			if Exists(portableNodeBin()) {
				t.Error("nothing must be installed")
			}
		}},
		{"failed swap restores the previous runtime", func(dest string) {
			fakeNode(t, portableNodeBin(), "v24.0.0") // too old: forces reinstall
			real := renameFn
			renameFn = func(from, to string) error {
				if from == dest+".new" {
					return os.ErrPermission
				}
				return real(from, to)
			}
		}, func(t *testing.T, dest string) {
			if NodeFullVersion(portableNodeBin()) != "24.0.0" {
				t.Error("previous runtime must be restored")
			}
			if Exists(dest+".new") || Exists(dest+".old") {
				t.Error("staging dirs must be cleaned up")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			portableFixture(t)
			dest := PortableRuntimeDir()
			tc.inject(dest)
			if _, err := EnsurePortableNode("24.15.0"); err == nil {
				t.Fatal("expected an error")
			}
			tc.check(t, dest)
		})
	}
}
