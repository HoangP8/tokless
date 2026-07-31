package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uv leads because it brings its own Python; pip --user is the last resort.
func TestPyAttemptsOrder(t *testing.T) {
	got := pyAttempts("projectmem", false)
	if len(got) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(got))
	}
	wantBins := []string{"uv", "pipx"}
	for i, want := range wantBins {
		if got[i].bin != want {
			t.Errorf("attempt %d bin = %q, want %q", i, got[i].bin, want)
		}
	}
	if !strings.HasPrefix(got[2].bin, "python") {
		t.Errorf("last attempt bin = %q, want python*", got[2].bin)
	}
	if got[0].prepare == nil {
		t.Error("uv attempt must bootstrap uv first")
	}
}

func TestPyAttemptsUpgradeFlags(t *testing.T) {
	plain := pyAttempts("headroom-ai[mcp]", false)
	upgrade := pyAttempts("headroom-ai[mcp]", true)

	if containsArg(plain[0].args, "--force") {
		t.Error("non-upgrade uv install should not pass --force")
	}
	if !containsArg(upgrade[0].args, "--force") {
		t.Error("upgrade uv install should pass --force")
	}
	if !containsArg(upgrade[1].args, "--force") {
		t.Error("upgrade pipx install should pass --force")
	}
	for _, a := range upgrade {
		if !containsArg(a.args, "headroom-ai[mcp]") {
			t.Errorf("%s lost the package spec: %v", a.bin, a.args)
		}
	}
}

// A dependency pin has to reach every installer, or the tool works on one
// machine and breaks on the next.
func TestPyAttemptsCarryConstraints(t *testing.T) {
	got := pyAttempts("projectmem", false, "mcp<2")

	if !containsArg(got[0].args, "--with") || !containsArg(got[0].args, "mcp<2") {
		t.Errorf("uv attempt missing --with mcp<2: %v", got[0].args)
	}
	// pipx has no --with, so the pin goes in as a follow-up inject.
	if !containsArg(got[1].after, "mcp<2") || !containsArg(got[1].after, "inject") {
		t.Errorf("pipx attempt should inject the pin: %v", got[1].after)
	}
	if !containsArg(got[2].args, "mcp<2") {
		t.Errorf("pip attempt missing the pin: %v", got[2].args)
	}

	none := pyAttempts("projectmem", false)
	if containsArg(none[0].args, "--with") {
		t.Error("no constraints should mean no --with")
	}
	if len(none[1].after) != 0 {
		t.Errorf("no constraints should mean no inject: %v", none[1].after)
	}
}

// PyPI has no notion of extras, so the JSON lookup must use the base name.
func TestPypiBaseNameStripsExtras(t *testing.T) {
	cases := map[string]string{
		"headroom-ai[mcp]":       "headroom-ai",
		"headroom-ai[mcp,proxy]": "headroom-ai",
		"projectmem":             "projectmem",
	}
	for in, want := range cases {
		if got := pypiBaseName(in); got != want {
			t.Errorf("pypiBaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

// sandboxPyEnv hides the developer's own Python tools from the test.
func sandboxPyEnv(t *testing.T) {
	t.Helper()
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("UV_TOOL_DIR", "")
}

// Windows installs a copied launcher instead of a symlink, so the python next
// to the command isn't there and we have to know the layouts.
func TestPyToolPythonFindsManagedVenv(t *testing.T) {
	sandboxPyEnv(t)
	root := t.TempDir()
	t.Setenv("UV_TOOL_DIR", root)

	name := "bin/python3"
	if IsWin {
		name = "Scripts/python.exe"
	}
	want := filepath.Join(root, "projectmem", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// "pjm" isn't on PATH here, so this can only succeed via the layout lookup.
	if got := PyToolPython("pjm", "projectmem"); got != want {
		t.Errorf("PyToolPython = %q, want %q", got, want)
	}
}

func TestPyToolPythonStripsExtrasFromPackage(t *testing.T) {
	sandboxPyEnv(t)
	root := t.TempDir()
	t.Setenv("UV_TOOL_DIR", root)
	if got := PyToolPython("headroom", "headroom-ai[mcp]"); got != "" {
		t.Errorf("expected no python for an uninstalled tool, got %q", got)
	}
	// The lookup must use the base name — PyPI extras aren't part of the dir.
	if roots := pyToolVenvRoots(pypiBaseName("headroom-ai[mcp]")); !strings.HasSuffix(roots[0], "headroom-ai") {
		t.Errorf("first root = %q, want it to end in headroom-ai", roots[0])
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
