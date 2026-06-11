package util

import (
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
