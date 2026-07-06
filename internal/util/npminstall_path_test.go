package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNpmGlobalBinDir(t *testing.T) {
	if got := npmGlobalBinDir(`C:\Users\u\AppData\Roaming\npm`, true); got != `C:\Users\u\AppData\Roaming\npm` {
		t.Fatalf("windows: shims live in the prefix itself, got %q", got)
	}
	if got := npmGlobalBinDir("/usr/local", false); got != filepath.Join("/usr/local", "bin") {
		t.Fatalf("unix: want prefix/bin, got %q", got)
	}
}

func TestEnsureNpmGlobalBinOnPath(t *testing.T) {
	dir := t.TempDir()
	orig := npmPrefix
	npmPrefix = func() string { return dir }
	defer func() { npmPrefix = orig }()
	t.Setenv("PATH", "/usr/bin")

	EnsureNpmGlobalBinOnPath()

	want := npmGlobalBinDir(dir, IsWin)
	if !strings.HasPrefix(os.Getenv("PATH"), want) {
		t.Fatalf("PATH not prepended with %q: %q", want, os.Getenv("PATH"))
	}

	// empty prefix → no-op
	npmPrefix = func() string { return "" }
	before := os.Getenv("PATH")
	EnsureNpmGlobalBinOnPath()
	if os.Getenv("PATH") != before {
		t.Fatalf("empty prefix must not touch PATH")
	}
}
