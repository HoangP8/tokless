package util

import (
	"path/filepath"
	"testing"
)

func TestCavemanVersionDirs(t *testing.T) {
	tempDir := t.TempDir()
	SetHomeOverride(tempDir)
	defer SetHomeOverride("")

	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	dirs := cavemanVersionDirs()

	expected1 := filepath.Join(tempDir, ".gemini", "antigravity", "skills", "caveman")
	expected2 := filepath.Join(tempDir, ".gemini", "config", "skills", "caveman")
	expectedAgents := filepath.Join(tempDir, ".agents", "skills", "caveman")

	found1, found2, foundAgents := false, false, false
	for _, d := range dirs {
		if d == expected1 {
			found1 = true
		}
		if d == expected2 {
			found2 = true
		}
		if d == expectedAgents {
			foundAgents = true
		}
	}

	if !found1 {
		t.Errorf("expected to find %s in %v", expected1, dirs)
	}
	if !found2 {
		t.Errorf("expected to find %s in %v", expected2, dirs)
	}
	if !foundAgents {
		t.Errorf("expected to find %s in %v", expectedAgents, dirs)
	}
}
