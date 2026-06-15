package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakePkg simulates npm having installed pkg into a custom prefix tree.
func writeFakePkg(t *testing.T, prefix, pkg, version string) {
	t.Helper()
	parent := "lib"
	if IsWin {
		parent = ""
	}
	dir := filepath.Join(prefix, parent, "node_modules", pkg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"`+pkg+`","version":"`+version+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNpmPrefixInstalledVersion(t *testing.T) {
	prefix := t.TempDir()
	if v := npmPrefixInstalledVersion(prefix, "context-mode"); v != nil {
		t.Fatalf("missing package.json must yield nil, got %v", *v)
	}
	writeFakePkg(t, prefix, "context-mode", "9.9.9")
	v := npmPrefixInstalledVersion(prefix, "context-mode")
	if v == nil || *v != "9.9.9" {
		t.Fatalf("want 9.9.9, got %v", v)
	}
}

func TestNpmUserPrefixInstall_Success(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	defer SetHomeOverride("")
	t.Setenv("npm_config_prefix", "")
	t.Setenv("NPM_CONFIG_PREFIX", "")
	t.Setenv("PATH", "/usr/bin")
	prefix := userLocalNpmPrefix()

	origRun := npmRunEnv
	defer func() { npmRunEnv = origRun }()
	npmRunEnv = func(args, env []string) ExecResult {
		if !strings.Contains(strings.Join(env, " "), "npm_config_prefix="+prefix) {
			t.Fatalf("npm_config_prefix not set in env: %v", env)
		}
		writeFakePkg(t, prefix, "context-mode", "1.2.3")
		return ExecResult{Code: 0}
	}

	v, ok := npmUserPrefixInstall("context-mode", "context-mode@latest", "")
	if !ok || v != "1.2.3" {
		t.Fatalf("want 1.2.3/true, got %q/%v", v, ok)
	}
	if !strings.HasPrefix(os.Getenv("PATH"), npmGlobalBinDir(prefix, IsWin)) {
		t.Fatalf("user-prefix bin dir not prepended to PATH: %q", os.Getenv("PATH"))
	}
}

// TestNpmGlobalInstall_FallbackUsed proves the new case kicks in only after every
// standard attempt fails, without altering the existing success path.
func TestNpmGlobalInstall_FallbackUsed(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	defer SetHomeOverride("")
	t.Setenv("npm_config_prefix", "")
	t.Setenv("NPM_CONFIG_PREFIX", "")
	t.Setenv("PATH", "/usr/bin")
	prefix := userLocalNpmPrefix()

	origResolve, origRun, origRead, origRunEnv := npmResolve, npmRun, npmReadInstalled, npmRunEnv
	defer func() {
		npmResolve, npmRun, npmReadInstalled, npmRunEnv = origResolve, origRun, origRead, origRunEnv
	}()

	npmResolve = func(pkg, spec string) (string, string, bool) { return "", "", false }
	npmRun = func(args []string) ExecResult { return ExecResult{Code: 1} } // every standard attempt fails
	npmReadInstalled = func(pkg string) *string { return nil }
	npmRunEnv = func(args, env []string) ExecResult {
		writeFakePkg(t, prefix, "context-mode", "4.5.6")
		return ExecResult{Code: 0}
	}

	v, ok := NpmGlobalInstall("context-mode", "latest")
	if !ok || v != "4.5.6" {
		t.Fatalf("fallback should install 4.5.6, got %q/%v", v, ok)
	}
}

// standard install success must NOT touch the fallback (no regression).
func TestNpmGlobalInstall_HappyPathUnchanged(t *testing.T) {
	origResolve, origRun, origRead, origRunEnv := npmResolve, npmRun, npmReadInstalled, npmRunEnv
	defer func() {
		npmResolve, npmRun, npmReadInstalled, npmRunEnv = origResolve, origRun, origRead, origRunEnv
	}()

	npmResolve = func(pkg, spec string) (string, string, bool) { return "1.0.0", "", true }
	npmRun = func(args []string) ExecResult { return ExecResult{Code: 0} } // first attempt succeeds
	npmReadInstalled = func(pkg string) *string { v := "1.0.0"; return &v }
	fallbackCalled := false
	npmRunEnv = func(args, env []string) ExecResult {
		fallbackCalled = true
		return ExecResult{Code: 0}
	}

	v, ok := NpmGlobalInstall("context-mode", "latest")
	if !ok || v != "1.0.0" {
		t.Fatalf("happy path should return 1.0.0, got %q/%v", v, ok)
	}
	if fallbackCalled {
		t.Fatal("fallback must NOT run when a standard attempt succeeds")
	}
}
