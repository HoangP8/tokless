package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStableExecutableUnixRequiresExecutableBit(t *testing.T) {
	old := IsWin
	IsWin = false
	defer func() { IsWin = old }()

	path := filepath.Join(t.TempDir(), "tokless")
	if err := os.WriteFile(path, []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if stableExecutable(path) {
		t.Fatal("non-executable Unix file accepted")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if !stableExecutable(path) {
		t.Fatal("executable Unix file rejected")
	}
}

func TestStableExecutableWindowsUsesExecutableExtension(t *testing.T) {
	old := IsWin
	IsWin = true
	defer func() { IsWin = old }()
	t.Setenv("PATHEXT", ".EXE;.CMD")

	dir := t.TempDir()
	for _, name := range []string{"tokless.exe", "tokless.cmd"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("binary"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !stableExecutable(path) {
			t.Errorf("Windows executable rejected: %s", path)
		}
	}
	for _, name := range []string{"tokless", "tokless.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("not executable"), 0o644); err != nil {
			t.Fatal(err)
		}
		if stableExecutable(path) {
			t.Errorf("non-executable Windows path accepted: %s", path)
		}
	}
}

func TestStableExecutableRejectsCommandInjectionCharacters(t *testing.T) {
	old := IsWin
	IsWin = true
	defer func() { IsWin = old }()

	for _, suffix := range []string{"\n", "\r", "\x00", "\t", "\v", "\f", " ", "\u00a0", "\targ"} {
		path := filepath.Join(t.TempDir(), "tokless.exe"+suffix)
		if stableExecutable(path) {
			t.Fatalf("path with unsafe character accepted: %q", suffix)
		}
	}
}

func TestWriteFileAtomicNeverLeavesPartialPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := WriteFileAtomic(path, strings.Repeat("new", 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.Repeat("new", 1000) {
		t.Fatal("atomic write changed payload")
	}
}
