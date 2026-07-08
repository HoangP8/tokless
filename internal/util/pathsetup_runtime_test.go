package util

import (
	"path/filepath"
	"testing"
)

func TestRuntimeBinDirs(t *testing.T) {
	oldIsWin := IsWin
	defer func() { IsWin = oldIsWin }()

	t.Run("IsWin false", func(t *testing.T) {
		IsWin = false
		if dirs := runtimeBinDirs(); dirs != nil {
			t.Errorf("runtimeBinDirs() = %v, want nil", dirs)
		}
	})

	t.Run("IsWin true", func(t *testing.T) {
		IsWin = true
		tempDir := t.TempDir()
		t.Setenv("APPDATA", filepath.Join(tempDir, "AppData"))
		t.Setenv("ProgramFiles", filepath.Join(tempDir, "ProgramFiles"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tempDir, "LocalAppData"))

		dirs := runtimeBinDirs()

		wantDirs := []string{
			nodeInstallDir(),
			filepath.Join(tempDir, "AppData", "npm"),
			filepath.Join(tempDir, "ProgramFiles", "nodejs"),
			filepath.Join(tempDir, "LocalAppData", "Programs", "nodejs"),
		}

		if len(dirs) != len(wantDirs) {
			t.Fatalf("runtimeBinDirs() returned %d dirs, want %d", len(dirs), len(wantDirs))
		}
		for i, dir := range dirs {
			if dir != wantDirs[i] {
				t.Errorf("runtimeBinDirs()[%d] = %q, want %q", i, dir, wantDirs[i])
			}
		}
	})
}

func TestExpectedBinDirsWindowsIncludesInstallDir(t *testing.T) {
	oldIsWin := IsWin
	defer func() { IsWin = oldIsWin }()
	IsWin = true
	tempDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(tempDir, "LocalAppData"))
	t.Setenv("USERPROFILE", filepath.Join(tempDir, "user"))

	dirs := ExpectedBinDirs()
	want := filepath.Join(tempDir, "LocalAppData", "Programs", "tokless")
	found := false
	for _, d := range dirs {
		if d == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ExpectedBinDirs() = %v, want %q included", dirs, want)
	}
}
