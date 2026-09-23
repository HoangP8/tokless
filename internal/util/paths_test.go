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

func TestToklessPersistedAbsUsesInstallMarker(t *testing.T) {
	oldWin := IsWin
	IsWin = false
	defer func() { IsWin = oldWin }()
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })

	path := filepath.Join(t.TempDir(), "tokless")
	if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteInstallMarker("test", path, "test"); err != nil {
		t.Fatal(err)
	}
	if got := ToklessPersistedAbs(); got != path {
		t.Fatalf("ToklessPersistedAbs() = %q, want %q", got, path)
	}
}

func TestToklessPersistedAbsUsesTestCommandWhenNoInstallExists(t *testing.T) {
	oldWin := IsWin
	IsWin = false
	defer func() { IsWin = oldWin }()
	t.Setenv("TOKLESS_TEST", "1")
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })
	t.Setenv("PATH", t.TempDir())

	if got := ToklessPersistedAbs(); got != "tokless" {
		t.Fatalf("ToklessPersistedAbs() = %q, want test command", got)
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

func TestSplitCommand(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`"/home/user with spaces/bin/tokless" rtk-hook cline`, []string{"/home/user with spaces/bin/tokless", "rtk-hook", "cline"}},
		{`'C:/Program Files/tokless/tokless.exe' rtk-hook droid`, []string{"C:/Program Files/tokless/tokless.exe", "rtk-hook", "droid"}},
		{`tokless rtk-rewrite -- "git status"`, []string{"tokless", "rtk-rewrite", "--", "git status"}},
		{`C:\Users\user\tokless.exe rtk-hook claude`, []string{`C:\Users\user\tokless.exe`, "rtk-hook", "claude"}},
	} {
		got := SplitCommand(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("SplitCommand(%q) length = %d, want %d: %v", tc.in, len(got), len(tc.want), got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("SplitCommand(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestStableExecutableAllowsPathsWithSpaces(t *testing.T) {
	oldWin := IsWin
	IsWin = false
	defer func() { IsWin = oldWin }()

	dir := filepath.Join(t.TempDir(), "user with spaces", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "tokless")
	if err := os.WriteFile(exe, []byte("fake-bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !stableExecutable(exe) {
		t.Fatalf("stableExecutable rejected valid executable with spaces: %q", exe)
	}
}
