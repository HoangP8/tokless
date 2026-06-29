package util

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExecResult mirrors the TS { code, stdout, stderr } shape.
type ExecResult struct {
	Code   int
	Stdout string
	Stderr string
}

// RunOptions controls stdio handling for Run.
type RunOptions struct {
	Capture bool
	Quiet   bool
	Cwd     string
	Env     []string
}

// Run executes a command; Capture pipes stdio, Quiet discards it, else inherit.
func Run(cmd string, args []string, opts RunOptions) ExecResult {
	c := exec.Command(cmd, args...)
	if opts.Cwd != "" {
		c.Dir = opts.Cwd
	}
	if opts.Env != nil {
		c.Env = append(os.Environ(), opts.Env...)
	}
	var outBuf, errBuf bytes.Buffer
	if opts.Capture {
		c.Stdout = &outBuf
		c.Stderr = &errBuf
	} else if opts.Quiet {
		// discard
	} else {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
	}
	err := c.Run()
	res := ExecResult{Stdout: outBuf.String(), Stderr: errBuf.String()}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.Code = ee.ExitCode()
		} else {
			// spawn failure (ENOENT etc.)
			res.Code = 127
			res.Stderr += err.Error()
		}
		return res
	}
	res.Code = 0
	return res
}

// Which finds an executable on PATH, honoring PATHEXT on Windows.
func Which(bin string) string {
	pathEnv := os.Getenv("PATH")
	var exts []string
	sep := ":"
	if IsWin {
		sep = ";"
		pe := os.Getenv("PATHEXT")
		if pe == "" {
			pe = ".EXE;.CMD;.BAT"
		}
		exts = strings.Split(pe, ";")
	} else {
		exts = []string{""}
	}
	for _, dir := range strings.Split(pathEnv, sep) {
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			p := filepath.Join(dir, bin+ext)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return ""
}

func FindBinary(bin string, extraDirs []string) string {
	if p := Which(bin); p != "" {
		return p
	}
	names := []string{bin}
	if IsWin {
		names = []string{bin + ".exe", bin + ".cmd", bin + ".bat", bin}
	}
	for _, dir := range extraDirs {
		if dir == "" {
			continue
		}
		for _, n := range names {
			p := filepath.Join(dir, n)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				PrependProcessPath(dir)
				return p
			}
		}
	}
	return ""
}

// PrependProcessPath puts dir at the front of this process's PATH (idempotent).
func PrependProcessPath(dir string) {
	sep := ":"
	if IsWin {
		sep = ";"
	}
	cur := os.Getenv("PATH")
	for _, d := range strings.Split(cur, sep) {
		if d == dir {
			return
		}
	}
	os.Setenv("PATH", dir+sep+cur)
}

// WhichAny returns the first found bin and its path.
func WhichAny(bins []string) (string, string) {
	for _, b := range bins {
		if p := Which(b); p != "" {
			return b, p
		}
	}
	return "", ""
}

// RtkInstallDirs returns well-known rtk install locations.
func RtkInstallDirs() []string {
	if IsWin {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return []string{filepath.Join(local, "rtk", "bin")}
		}
		return nil
	}
	return []string{filepath.Join(Home(), ".local", "bin")}
}

// BinaryHealthy probes --version for a dot — rejects shims and 0-byte files.
func BinaryHealthy(p string) bool {
	r := Run(p, []string{"--version"}, RunOptions{Capture: true})
	return r.Code == 0 && strings.Contains(r.Stdout, ".")
}

// ResolveRtkBin finds a working rtk binary, surviving PATH drift.
func ResolveRtkBin() string {
	if p := Which("rtk"); p != "" {
		if BinaryHealthy(p) {
			return p
		}
	}
	sep := ":"
	if IsWin {
		sep = ";"
	}
	cur := os.Getenv("PATH")
	prefix := ""
	for _, d := range RtkInstallDirs() {
		if d == "" {
			continue
		}
		prefix += d + sep
	}
	if prefix != "" {
		os.Setenv("PATH", prefix+cur)
	}
	return Which("rtk")
}

// nodeBinCandidates returns well-known node bin dirs across version managers.
func nodeBinCandidates() []string {
	h := Home()
	var dirs []string
	nvmNode := filepath.Join(h, ".nvm", "versions", "node")
	if entries, err := os.ReadDir(nvmNode); err == nil {
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].IsDir() {
				dirs = append(dirs, filepath.Join(nvmNode, entries[i].Name(), "bin"))
			}
		}
	}
	fnmDir := filepath.Join(h, ".fnm", "node-versions")
	if entries, err := os.ReadDir(fnmDir); err == nil {
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].IsDir() {
				dirs = append(dirs, filepath.Join(fnmDir, entries[i].Name(), "installation", "bin"))
			}
		}
	}
	if d := os.Getenv("FNM_MULTISHELL_PATH"); d != "" {
		dirs = append(dirs, d)
	}
	dirs = append(dirs, filepath.Join(h, ".volta", "bin"))
	dirs = append(dirs, filepath.Join(h, ".asdf", "shims"))
	nDir := filepath.Join(h, ".n", "versions", "node")
	if entries, err := os.ReadDir(nDir); err == nil {
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].IsDir() {
				dirs = append(dirs, filepath.Join(nDir, entries[i].Name(), "bin"))
			}
		}
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	return dirs
}

// ResolveNodeBinary finds a working node binary, surviving PATH drift.
func ResolveNodeBinary() string {
	if p := Which("node"); p != "" {
		if BinaryHealthy(p) {
			return p
		}
	}
	for _, d := range nodeBinCandidates() {
		if d == "" || !Exists(d) {
			continue
		}
		p := filepath.Join(d, "node")
		if IsWin {
			p += ".exe"
		}
		if Exists(p) {
			PrependProcessPath(d)
			return p
		}
	}
	return ""
}

// ResolveNpmBinary finds npm, surviving PATH drift.
func ResolveNpmBinary() string {
	if p := Which("npm"); p != "" {
		return p
	}
	nodePath := ResolveNodeBinary()
	if nodePath == "" {
		return ""
	}
	dir := filepath.Dir(nodePath)
	p := filepath.Join(dir, "npm")
	if IsWin {
		p += ".cmd"
	}
	if Exists(p) {
		PrependProcessPath(dir)
		return p
	}
	return ""
}

// codegraphBinCandidates returns well-known codegraph install dirs.
func codegraphBinCandidates() []string {
	h := Home()
	return []string{
		filepath.Join(h, ".local", "bin"),
		filepath.Join(h, ".bun", "bin"),
	}
}

// ResolveCodegraphBin finds codegraph, surviving PATH drift.
func ResolveCodegraphBin() string {
	if p := Which("codegraph"); p != "" {
		if BinaryHealthy(p) {
			return p
		}
	}
	for _, d := range codegraphBinCandidates() {
		if d == "" || !Exists(d) {
			continue
		}
		p := filepath.Join(d, "codegraph")
		if IsWin {
			p += ".cmd"
		}
		if Exists(p) {
			PrependProcessPath(d)
			return p
		}
	}
	// Fall back to nvm node bin dir (npm global install lands there)
	nodePath := ResolveNodeBinary()
	if nodePath != "" {
		dir := filepath.Dir(nodePath)
		p := filepath.Join(dir, "codegraph")
		if IsWin {
			p += ".cmd"
		}
		if Exists(p) {
			PrependProcessPath(dir)
			return p
		}
	}
	return ""
}

// ResolveBunBinary finds bun, surviving PATH drift.
func ResolveBunBinary() string {
	if p := Which("bun"); p != "" {
		return p
	}
	p := filepath.Join(Home(), ".bun", "bin", "bun")
	if Exists(p) {
		PrependProcessPath(filepath.Dir(p))
		return p
	}
	return ""
}
