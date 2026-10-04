package util

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const uvInstallDocs = "https://docs.astral.sh/uv/getting-started/installation/"

var ensureUVTimeout = 3 * time.Minute

// uvInstallCmd runs the official uv standalone installer.
var uvInstallCmd = func(ctx context.Context, name string, args []string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Env = os.Environ() // inherit proxies for corporate networks
	out, err := c.CombinedOutput()
	if err != nil {
		if len(out) == 0 {
			return err
		}
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetUVInstallCmdForTest replaces the installer hook for hermetic tests and
// returns a restore function.
func SetUVInstallCmdForTest(fn func(ctx context.Context, name string, args []string) error) func() {
	prev := uvInstallCmd
	uvInstallCmd = fn
	return func() { uvInstallCmd = prev }
}

// uvInstallArgs returns the official installer command for goos (pure, for tests).
func uvInstallArgs(goos string) (string, []string) {
	if goos == "windows" {
		return "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-c",
			"irm https://astral.sh/uv/install.ps1 | iex"}
	}
	return "sh", []string{"-c", "curl -LsSf https://astral.sh/uv/install.sh | sh"}
}

// uvInstallArgsWget is the curl-free fallback for minimal unix images.
func uvInstallArgsWget() (string, []string) {
	return "sh", []string{"-c", "wget -qO- https://astral.sh/uv/install.sh | sh"}
}

// uvCandidatePaths returns well-known standalone uv install locations for goos.
func uvCandidatePaths(goos string) []string {
	home := Home()
	if goos == "windows" {
		paths := []string{filepath.Join(home, ".local", "bin", "uv.exe")}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			paths = append(paths, filepath.Join(local, "uv", "uv.exe"))
		}
		return paths
	}
	return []string{
		filepath.Join(home, ".local", "bin", "uv"),
		filepath.Join(home, ".cargo", "bin", "uv"),
	}
}

// resolveUV returns a usable uv path from PATH or well-known install dirs.
func resolveUV() string {
	if p := Which("uv"); p != "" {
		return p
	}
	for _, p := range uvCandidatePaths(runtime.GOOS) {
		if stableExecutable(p) {
			return p
		}
	}
	return ""
}

// EnsureUV returns a usable uv binary, bootstrapping the official standalone
// installer once when uv is absent.
func EnsureUV(ctx context.Context) (string, error) {
	if p := Which("uv"); p != "" {
		return p, nil
	}
	if os.Getenv("TOKLESS_TEST") == "1" {
		return "", fmt.Errorf("uv not found on PATH; install uv: %s", uvInstallDocs)
	}
	ctx, cancel := context.WithTimeout(ctx, ensureUVTimeout)
	defer cancel()
	name, args := uvInstallArgs(runtime.GOOS)
	if runtime.GOOS == "windows" && Which("powershell") == "" && Which("pwsh") != "" {
		name = "pwsh"
	}
	if err := uvInstallCmd(ctx, name, args); err != nil {
		if runtime.GOOS != "windows" && Which("curl") == "" && Which("wget") != "" {
			name, args = uvInstallArgsWget()
			err = uvInstallCmd(ctx, name, args)
		}
		if err != nil {
			return "", fmt.Errorf("uv install failed: %w; install uv manually: %s", err, uvInstallDocs)
		}
	}
	if p := resolveUV(); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("uv install finished but uv not found on PATH; install uv manually: %s", uvInstallDocs)
}
