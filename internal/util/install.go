package util

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallRecord is the marker an installer writes so tokless can report,
// exactly, which channel put it on disk. Path is recorded so a stale marker
// (installed via curl, later rebuilt from source) can be detected instead of
// reported as truth.
type InstallRecord struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Version string `json:"version"`
	At      string `json:"at"`
}

// ToklessDataDir is the per-user directory tokless keeps its own state in.
func ToklessDataDir() string {
	if IsWin {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(Home(), "AppData", "Local")
		}
		return filepath.Join(base, "tokless")
	}
	return filepath.Join(Home(), ".local", "share", "tokless")
}

// InstallMarkerPath is where installers write the install record.
func InstallMarkerPath() string { return filepath.Join(ToklessDataDir(), "install.json") }

// WriteInstallMarker records how this binary was installed.
func WriteInstallMarker(method, path string) error {
	if err := EnsureDir(ToklessDataDir()); err != nil {
		return err
	}
	b, err := json.Marshal(InstallRecord{
		Method:  method,
		Path:    path,
		Version: ToklessVersion(),
		At:      time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return WriteFile(InstallMarkerPath(), string(b))
}

// InstallInfo reports how the running binary was installed. exact is true only
// when a marker written by the installer matches this binary; otherwise the
// method is inferred from the binary's location and callers must present it as
// a guess.
func InstallInfo() (rec InstallRecord, exact bool) {
	exe := ToklessAbs()

	if raw, ok := ReadFileSafe(InstallMarkerPath()); ok {
		var m InstallRecord
		if json.Unmarshal([]byte(raw), &m) == nil && m.Method != "" {
			if samePath(m.Path, exe) {
				return m, true
			}
		}
	}

	return InstallRecord{Method: inferInstallMethod(exe), Path: exe}, false
}

// samePath compares two binary paths tolerantly (case/separator on Windows).
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	norm := func(p string) string {
		p = filepath.Clean(p)
		if IsWin {
			return strings.ToLower(filepath.ToSlash(p))
		}
		return p
	}
	return norm(a) == norm(b)
}

// inferInstallMethod guesses the channel from where the binary lives.
func inferInstallMethod(exe string) string {
	if exe == "" || !strings.ContainsRune(exe, filepath.Separator) {
		return "unknown"
	}
	slash := filepath.ToSlash(exe)
	if IsWin {
		slash = strings.ToLower(slash)
	}
	dir := filepath.ToSlash(filepath.Dir(exe))

	switch {
	// Homebrew's dir is literally "Cellar"; match case-insensitively.
	case strings.Contains(strings.ToLower(slash), "/cellar/"), strings.HasPrefix(slash, "/opt/homebrew/"),
		strings.HasPrefix(slash, "/home/linuxbrew/"), strings.HasPrefix(slash, "/usr/local/homebrew/"):
		return "homebrew"
	case strings.Contains(slash, "/node_modules/.bin/"):
		return "npm"
	case isGoBinDir(dir):
		return "go install"
	case samePath(dir, filepath.Join(Home(), ".local", "bin")):
		return "install script"
	case strings.Contains(slash, "/dist/release/"), strings.Contains(slash, "/go-build"):
		return "source build"
	}
	return "unknown"
}

// isGoBinDir reports whether dir is where `go install` drops binaries.
func isGoBinDir(dir string) bool {
	if b := os.Getenv("GOBIN"); b != "" && samePath(dir, b) {
		return true
	}
	roots := strings.Split(os.Getenv("GOPATH"), string(os.PathListSeparator))
	roots = append(roots, filepath.Join(Home(), "go"))
	for _, r := range roots {
		if r == "" {
			continue
		}
		if samePath(dir, filepath.Join(r, "bin")) {
			return true
		}
	}
	return false
}
