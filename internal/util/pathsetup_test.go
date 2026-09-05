package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsurePersistentPathDoesNotEditUnixStartupFiles(t *testing.T) {
	if IsWin {
		t.Skip("Unix startup-file behavior")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	for _, name := range []string{".zshenv", ".zshrc", ".bashrc", ".profile"} {
		path := filepath.Join(home, name)
		if err := os.WriteFile(path, []byte("export KEEP=1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := EnsurePersistentPath(); got != nil {
		t.Fatalf("EnsurePersistentPath = %v, want no startup-file changes", got)
	}
	for _, name := range []string{".zshenv", ".zshrc", ".bashrc", ".profile"} {
		path := filepath.Join(home, name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "export KEEP=1\n" {
			t.Fatalf("%s changed: %q", name, got)
		}
	}
}

func TestEnsureProcessPathOnlyChangesCurrentProcess(t *testing.T) {
	if IsWin {
		t.Skip("Unix PATH behavior")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin")
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := EnsureProcessPath(); len(got) != 1 || got[0] != bin {
		t.Fatalf("EnsureProcessPath = %v, want %q", got, bin)
	}
	if !strings.HasPrefix(os.Getenv("PATH"), bin+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want process-local prefix", os.Getenv("PATH"))
	}
}
