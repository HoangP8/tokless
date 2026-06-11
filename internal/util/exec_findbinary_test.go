package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindBinaryKnownDir(t *testing.T) {
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()
	IsWin = false

	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	extraDir := t.TempDir()
	claudePath := filepath.Join(extraDir, "claude")
	os.WriteFile(claudePath, []byte("dummy"), 0755)

	res := FindBinary("claude", []string{extraDir})
	if res != claudePath {
		t.Errorf("Expected FindBinary to return %s, got %s", claudePath, res)
	}

	pathEnv := os.Getenv("PATH")
	if !strings.HasPrefix(pathEnv, extraDir) {
		t.Errorf("Expected PATH to start with %s, got %s", extraDir, pathEnv)
	}
}

func TestFindBinaryWindowsExe(t *testing.T) {
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()
	IsWin = true

	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	// Test .exe
	extraDirExe := t.TempDir()
	claudeExePath := filepath.Join(extraDirExe, "claude.exe")
	os.WriteFile(claudeExePath, []byte("dummy"), 0755)

	resExe := FindBinary("claude", []string{extraDirExe})
	if resExe != claudeExePath {
		t.Errorf("Expected FindBinary to return %s, got %s", claudeExePath, resExe)
	}

	// Test .cmd
	extraDirCmd := t.TempDir()
	claudeCmdPath := filepath.Join(extraDirCmd, "claude.cmd")
	os.WriteFile(claudeCmdPath, []byte("dummy"), 0755)

	resCmd := FindBinary("claude", []string{extraDirCmd})
	if resCmd != claudeCmdPath {
		t.Errorf("Expected FindBinary to return %s, got %s", claudeCmdPath, resCmd)
	}
}

func TestFindBinaryMiss(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)
	extraDir := t.TempDir()

	res := FindBinary("claude", []string{extraDir})
	if res != "" {
		t.Errorf("Expected FindBinary to miss and return empty string, got %s", res)
	}
}
