package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeConfigDirPrecedence(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()
	homeDir := t.TempDir()

	// OPENCODE_CONFIG_DIR set
	t.Setenv("OPENCODE_CONFIG_DIR", tmpDir1)
	paths := OpenCodePathsResolved()
	if paths.Dir != tmpDir1 {
		t.Errorf("Expected Dir to be %s, got %s", tmpDir1, paths.Dir)
	}
	if paths.Config != filepath.Join(tmpDir1, "opencode.jsonc") {
		t.Errorf("Expected Config to be %s, got %s", filepath.Join(tmpDir1, "opencode.jsonc"), paths.Config)
	}

	// XDG_CONFIG_HOME set
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", tmpDir2)
	pathsXdg := OpenCodePathsResolved()
	expectedXdgDir := filepath.Join(tmpDir2, "opencode")
	if pathsXdg.Dir != expectedXdgDir {
		t.Errorf("Expected Dir to be %s, got %s", expectedXdgDir, pathsXdg.Dir)
	}

	// Fallback to Home
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", homeDir)
	SetHomeOverride(homeDir)
	defer SetHomeOverride("")

	pathsHome := OpenCodePathsResolved()
	expectedHomeDir := filepath.Join(homeDir, ".config", "opencode")
	if pathsHome.Dir != expectedHomeDir {
		t.Errorf("Expected Dir to be %s, got %s", expectedHomeDir, pathsHome.Dir)
	}
}

func TestOpenCodePathsV2Resolution(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	t.Setenv("HOME", home)
	xdg := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	t.Setenv("OPENCODE_CONFIG", "")

	v1Dir := filepath.Join(xdg, "opencode")
	v2Dir := filepath.Join(xdg, "opencode2")
	if got := OpenCodePathsV2().Dir; got != v1Dir {
		t.Fatalf("default V2 dir = %q, want shared fallback %q", got, v1Dir)
	}

	if err := EnsureDir(v1Dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(filepath.Join(v1Dir, "opencode2.jsonc"), `{}`); err != nil {
		t.Fatal(err)
	}
	if got := OpenCodePathsV2().Dir; got != v1Dir {
		t.Fatalf("marker V2 dir = %q, want %q", got, v1Dir)
	}
	if err := os.Remove(filepath.Join(v1Dir, "opencode2.jsonc")); err != nil {
		t.Fatal(err)
	}
	if got := OpenCodePathsV2().Dir; got != v1Dir {
		t.Fatalf("after marker removed V2 dir = %q, want %q", got, v1Dir)
	}
	if err := WriteFile(filepath.Join(v2Dir, "opencode.jsonc"), `{}`); err != nil {
		t.Fatal(err)
	}
	if got := OpenCodePathsV2().Dir; got != v2Dir {
		t.Fatalf("existing V2 dir = %q, want %q", got, v2Dir)
	}

	if err := os.RemoveAll(v2Dir); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(home, ".opencode2")
	if err := WriteFile(filepath.Join(legacy, "opencode.jsonc"), `{}`); err != nil {
		t.Fatal(err)
	}
	if got := OpenCodePathsV2().Dir; got != legacy {
		t.Fatalf("legacy V2 dir = %q, want %q", got, legacy)
	}
	if err := os.RemoveAll(legacy); err != nil {
		t.Fatal(err)
	}
	if got, want := OpenCodePathsV2().Dir, OpenCodePathsResolved().Dir; got != want {
		t.Fatalf("shared fallback = %q, want v1 dir %q", got, want)
	}
}

func TestOpenCodeConfigFileOverride(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "custom.jsonc")
	t.Setenv("OPENCODE_CONFIG", config)
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(t.TempDir(), "ignored"))

	paths := OpenCodePathsResolved()
	if paths.Config != config {
		t.Fatalf("OpenCode paths = %+v, want config %q", paths, config)
	}
}

func TestClaudeConfigDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)

	paths := ClaudeCodePaths()
	if paths.Dir != tmp {
		t.Errorf("Expected Dir to be %s, got %s", tmp, paths.Dir)
	}
	expectedGlobalJSON := filepath.Join(tmp, ".claude.json")
	if paths.GlobalJSON != expectedGlobalJSON {
		t.Errorf("Expected GlobalJSON to be %s, got %s", expectedGlobalJSON, paths.GlobalJSON)
	}
	expectedSettings := filepath.Join(tmp, "settings.json")
	if paths.Settings != expectedSettings {
		t.Errorf("Expected Settings to be %s, got %s", expectedSettings, paths.Settings)
	}
}
