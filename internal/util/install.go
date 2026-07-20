package util

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallRecord is the install.json marker (method + path for staleness checks).
type InstallRecord struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Version string `json:"version"`
	At      string `json:"at"`
}

// ToklessDataDir is per-user tokless state (~/.local/share/tokless or %LOCALAPPDATA%\tokless).
// Honors SetHomeOverride so tests never write outside the sandbox (esp. Windows LOCALAPPDATA).
func ToklessDataDir() string {
	if homeOverride != "" {
		if IsWin {
			return filepath.Join(homeOverride, "AppData", "Local", "tokless")
		}
		return filepath.Join(homeOverride, ".local", "share", "tokless")
	}
	if IsWin {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(Home(), "AppData", "Local")
		}
		return filepath.Join(base, "tokless")
	}
	return filepath.Join(Home(), ".local", "share", "tokless")
}

// InstallMarkerPath is install.json under ToklessDataDir.
func InstallMarkerPath() string { return filepath.Join(ToklessDataDir(), "install.json") }

// WriteInstallMarker writes how this binary was installed.
func WriteInstallMarker(method, path, version string) error {
	if err := EnsureDir(ToklessDataDir()); err != nil {
		return err
	}
	b, err := json.Marshal(InstallRecord{
		Method:  method,
		Path:    path,
		Version: version,
		At:      time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return WriteFile(InstallMarkerPath(), string(b))
}

// RefreshInstallMarker updates version/path after self-update; keeps prior method if any.
func RefreshInstallMarker(version string) {
	method := "self-update"
	if raw, ok := ReadFileSafe(InstallMarkerPath()); ok {
		var m InstallRecord
		if json.Unmarshal([]byte(raw), &m) == nil && m.Method != "" {
			method = m.Method
		}
	}
	_ = WriteInstallMarker(method, ToklessAbs(), version)
}

// InstallInfo returns install provenance. exact means marker path matches this binary.
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

// samePath compares paths with Clean; on Windows also lowercases and normalizes slashes.
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

// inferInstallMethod guesses install channel from binary location.
func inferInstallMethod(exe string) string {
	if exe == "" || !strings.ContainsRune(exe, filepath.Separator) {
		return "unknown"
	}
	slash := filepath.ToSlash(exe)
	if IsWin {
		slash = strings.ToLower(slash)
	}
	dir := filepath.Dir(exe)

	switch {
	case strings.Contains(strings.ToLower(slash), "/cellar/"), strings.HasPrefix(slash, "/opt/homebrew/"),
		strings.HasPrefix(slash, "/home/linuxbrew/"), strings.HasPrefix(slash, "/usr/local/homebrew/"):
		return "homebrew"
	case strings.Contains(slash, "/node_modules/.bin/"):
		return "npm"
	case isGoBinDir(dir):
		return "go install"
	case isInstallScriptDir(dir):
		return "install script"
	case strings.Contains(slash, "/dist/release/"), strings.Contains(slash, "/go-build"):
		return "source build"
	}
	return "unknown"
}

// isInstallScriptDir matches curl/irm install destinations (unix + Windows).
func isInstallScriptDir(dir string) bool {
	if samePath(dir, filepath.Join(Home(), ".local", "bin")) {
		return true
	}
	if !IsWin {
		return false
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(Home(), "AppData", "Local")
	}
	return samePath(dir, filepath.Join(base, "Programs", "tokless"))
}

// isGoBinDir is true when dir is GOBIN or $GOPATH/bin (or ~/go/bin).
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
