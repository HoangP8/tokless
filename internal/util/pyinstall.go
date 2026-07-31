package util

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Python tools install into their own environment, never into the user's
// system packages. Order: uv (brings its own Python) -> pipx -> pip --user.

func uvToolBinDir() string {
	if d := os.Getenv("XDG_BIN_HOME"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".local", "bin")
}

// ResolvePyBin finds a Python tool on PATH, or in uv/pipx folders — an agent
// launched from a GUI often has a shorter PATH than your shell.
func ResolvePyBin(name string) string {
	if p := Which(name); p != "" {
		return p
	}
	for _, dir := range pyBinDirs() {
		for _, exe := range pyExeNames(name) {
			if p := filepath.Join(dir, exe); Exists(p) {
				return p
			}
		}
	}
	return ""
}

// uv and pipx both use ~/.local/bin on Linux and macOS; Windows uses app data.
func pyBinDirs() []string {
	dirs := []string{uvToolBinDir()}
	if IsWin {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs,
				filepath.Join(la, "uv", "tools", "bin"),
				filepath.Join(la, "Programs", "Python", "Scripts"),
			)
		}
		if ad := os.Getenv("APPDATA"); ad != "" {
			dirs = append(dirs, filepath.Join(ad, "Python", "Scripts"))
		}
		return dirs
	}
	return append(dirs, filepath.Join(Home(), "bin"))
}

func pyExeNames(name string) []string {
	if IsWin {
		return []string{name + ".exe", name + ".cmd", name}
	}
	return []string{name}
}

func PythonMinor() int {
	py := WhichPython()
	if py == "" {
		return 0
	}
	r := Run(py, []string{"--version"}, RunOptions{Capture: true, Quiet: true})
	src := r.Stdout
	if src == "" {
		src = r.Stderr
	}
	// "Python 3.12.4" -> 12
	fields := strings.Fields(src)
	if len(fields) < 2 {
		return 0
	}
	parts := strings.Split(fields[len(fields)-1], ".")
	if len(parts) < 2 || parts[0] != "3" {
		return 0
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return n
}

func WhichPython() string {
	if p := Which("python3"); p != "" {
		return p
	}
	return Which("python")
}

func PythonReady(minMinor int) bool {
	if ResolvePyBin("uv") != "" {
		return true
	}
	m := PythonMinor()
	return m > 0 && (minMinor == 0 || m >= minMinor)
}

// EnsureUv installs uv if missing. It's one small binary and brings its own
// Python, which is why we try it first.
func EnsureUv() bool {
	if p := ResolvePyBin("uv"); p != "" {
		PrependProcessPath(filepath.Dir(p))
		return true
	}
	if isPyTest() {
		return false
	}
	dest := filepath.Join(Home(), ".local", "bin")
	_ = os.MkdirAll(dest, 0o755)
	if IsWin {
		ps := "$ErrorActionPreference='Stop'; irm https://astral.sh/uv/install.ps1 | iex"
		if shell := powerShellBin(); shell != "" {
			if Run(shell, []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps}, RunOptions{}).Code == 0 {
				PrependProcessPath(dest)
				return ResolvePyBin("uv") != ""
			}
		}
		return false
	}
	for _, asset := range uvAssetsForThisPlatform() {
		url := "https://github.com/astral-sh/uv/releases/latest/download/" + asset
		if err := DownloadAndExtractTarGz(url, dest); err != nil {
			continue
		}
		// The tarball unpacks into a versioned subdir.
		hoistUvBinaries(dest)
		PrependProcessPath(dest)
		p := ResolvePyBin("uv")
		if p == "" {
			continue
		}
		_ = os.Chmod(p, 0o755)
		// A glibc build downloads happily on musl and then won't run, so make
		// sure it actually executes before calling this a success.
		if BinaryHealthy(p) {
			return true
		}
		_ = os.Remove(p)
	}
	if Which("curl") != "" && Which("sh") != "" {
		if Run("sh", []string{"-c", "curl -LsSf https://astral.sh/uv/install.sh | sh"}, RunOptions{}).Code == 0 {
			PrependProcessPath(dest)
			return ResolvePyBin("uv") != ""
		}
	}
	return false
}

// uvAssetsForThisPlatform lists release assets to try, best first. Linux gets
// both libc flavours because musl (Alpine) can't run a glibc build.
func uvAssetsForThisPlatform() []string {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{"uv-" + arch + "-apple-darwin.tar.gz"}
	case "linux":
		gnu := "uv-" + arch + "-unknown-linux-gnu.tar.gz"
		musl := "uv-" + arch + "-unknown-linux-musl.tar.gz"
		if isMusl() {
			return []string{musl, gnu}
		}
		return []string{gnu, musl}
	}
	return nil
}

// Alpine and friends, where glibc builds won't run.
func isMusl() bool {
	if Exists("/etc/alpine-release") {
		return true
	}
	matches, _ := filepath.Glob("/lib/ld-musl-*")
	return len(matches) > 0
}

func hoistUvBinaries(dest string) {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "uv-") {
			continue
		}
		sub := filepath.Join(dest, e.Name())
		for _, bin := range []string{"uv", "uvx"} {
			src := filepath.Join(sub, bin)
			if !Exists(src) {
				continue
			}
			_ = os.Remove(filepath.Join(dest, bin))
			if os.Rename(src, filepath.Join(dest, bin)) == nil {
				_ = os.Chmod(filepath.Join(dest, bin), 0o755)
			}
		}
		_ = os.RemoveAll(sub)
	}
}

func powerShellBin() string {
	if p := Which("pwsh"); p != "" {
		return p
	}
	return Which("powershell")
}

// PyGlobalInstall installs or upgrades a Python tool. After each try we look
// for bin again, so an installer that claims success but did nothing fails.
// constraints are extra version specs resolved alongside the package, for
// pinning a dependency the package itself left unbounded.
func PyGlobalInstall(pkg, bin string, upgrade bool, constraints ...string) bool {
	if isPyTest() {
		return true
	}
	for _, attempt := range pyAttempts(pkg, upgrade, constraints...) {
		if attempt.prepare != nil && !attempt.prepare() {
			continue
		}
		cmd := ResolvePyBin(attempt.bin)
		if cmd == "" {
			continue
		}
		L.Sub("trying " + attempt.label)
		if Run(cmd, attempt.args, RunOptions{Capture: true}).Code != 0 {
			continue
		}
		if len(attempt.after) > 0 {
			// Pin didn't apply, so this isn't the install we asked for.
			if Run(cmd, attempt.after, RunOptions{Capture: true}).Code != 0 {
				continue
			}
		}
		PrependProcessPath(uvToolBinDir())
		if ResolvePyBin(bin) != "" {
			return true
		}
	}
	return false
}

type pyAttempt struct {
	label   string
	bin     string
	args    []string
	after   []string // optional follow-up run with the same bin
	prepare func() bool
}

// pyAttempts is the install order, kept as data so tests can check it.
func pyAttempts(pkg string, upgrade bool, constraints ...string) []pyAttempt {
	uvArgs := []string{"tool", "install", pkg}
	if upgrade {
		uvArgs = append(uvArgs, "--force")
	}
	for _, c := range constraints {
		uvArgs = append(uvArgs, "--with", c)
	}

	pipxArgs := []string{"install", pkg}
	if upgrade {
		pipxArgs = append(pipxArgs, "--force")
	}
	// pipx has no --with; the pins go in afterwards via inject.
	var pipxInject []string
	if len(constraints) > 0 {
		pipxInject = append([]string{"inject", pypiBaseName(pkg)}, constraints...)
	}

	pipArgs := append([]string{"-m", "pip", "install", "--user", "--upgrade", pkg}, constraints...)
	if len(constraints) > 0 {
		// Unlike uv and pipx, pip --user shares one environment with every other
		// user-installed Python tool, so a pin here reaches beyond this package.
		L.Debug("pip fallback would pin " + strings.Join(constraints, " ") + " in user site-packages")
	}

	return []pyAttempt{
		{label: "uv tool install " + pkg, bin: "uv", args: uvArgs, prepare: EnsureUv},
		{label: "pipx install " + pkg, bin: "pipx", args: pipxArgs, after: pipxInject},
		{label: "pip install --user " + pkg, bin: pythonBinName(), args: pipArgs},
	}
}

func pythonBinName() string {
	if Which("python3") != "" {
		return "python3"
	}
	return "python"
}

// PyToolPython finds the python that owns an installed tool. On Unix the
// command symlinks into its environment; Windows gets a copied launcher
// instead, so there we have to know where uv and pipx keep the environment.
func PyToolPython(bin, pkg string) string {
	if p := pyVenvSibling(bin); p != "" {
		return p
	}
	for _, root := range pyToolVenvRoots(pypiBaseName(pkg)) {
		for _, rel := range []string{"bin/python3", "bin/python", "Scripts/python.exe"} {
			if p := filepath.Join(root, filepath.FromSlash(rel)); Exists(p) {
				return p
			}
		}
	}
	return ""
}

func pyVenvSibling(bin string) string {
	p := ResolvePyBin(bin)
	if p == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(p); err == nil && real != "" {
		p = real
	}
	dir := filepath.Dir(p)
	for _, name := range []string{"python3", "python", "python.exe"} {
		if c := filepath.Join(dir, name); Exists(c) {
			return c
		}
	}
	return ""
}

func pyToolVenvRoots(base string) []string {
	var roots []string
	if d := os.Getenv("UV_TOOL_DIR"); d != "" {
		roots = append(roots, filepath.Join(d, base))
	}
	if IsWin {
		if ad := os.Getenv("APPDATA"); ad != "" {
			roots = append(roots, filepath.Join(ad, "uv", "tools", base))
		}
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			roots = append(roots,
				filepath.Join(la, "uv", "tools", base),
				filepath.Join(la, "pipx", "venvs", base),
			)
		}
		return roots
	}
	return append(roots,
		filepath.Join(Home(), ".local", "share", "uv", "tools", base),
		filepath.Join(Home(), ".local", "share", "pipx", "venvs", base),
		filepath.Join(Home(), "Library", "Application Support", "pipx", "venvs", base),
	)
}

// PyImportOK catches a package that installed fine but can't start.
// No python found means no answer, so don't fail the install over it.
func PyImportOK(py, module string) bool {
	if py == "" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r := Run(py, []string{"-c", "import " + module}, RunOptions{Capture: true, Quiet: true, Ctx: ctx})
	if r.Code != 0 {
		L.Debug("import " + module + " failed: " + lastLine(r.Stderr))
	}
	return r.Code == 0
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// PyPackageVersion asks the package's own environment what version it is.
// Needed for tools like projectmem whose CLI has no --version flag.
func PyPackageVersion(bin, pkg string) *string {
	py := PyToolPython(bin, pkg)
	if py == "" {
		return nil
	}
	code := "import importlib.metadata as m; print(m.version('" + pypiBaseName(pkg) + "'))"
	r := Run(py, []string{"-c", code}, RunOptions{Capture: true, Quiet: true})
	if v := strings.TrimSpace(r.Stdout); reSemver.MatchString(v) {
		return strp(reSemver.FindStringSubmatch(v)[1])
	}
	return nil
}

func PyGlobalUninstall(pkg, bin string) bool {
	if isPyTest() {
		return true
	}
	if ResolvePyBin(bin) == "" {
		return false
	}
	base := pypiBaseName(pkg)
	attempts := []struct {
		cmd  string
		args []string
	}{
		{"uv", []string{"tool", "uninstall", base}},
		{"pipx", []string{"uninstall", base}},
		{pythonBinName(), []string{"-m", "pip", "uninstall", "-y", base}},
	}
	for _, a := range attempts {
		cmd := ResolvePyBin(a.cmd)
		if cmd == "" {
			continue
		}
		if Run(cmd, a.args, RunOptions{Capture: true}).Code == 0 && ResolvePyBin(bin) == "" {
			return true
		}
	}
	return false
}

func isPyTest() bool { return os.Getenv("TOKLESS_TEST") == "1" }
