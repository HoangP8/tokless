package util

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// A hit must spawn by absolute path and leave PATH alone: the npm bin dir also holds node,
// and a PATH change would make unrelated dependency checks pass.
func TestPickMcpSpawnHitLeavesPathUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell shim")
	}
	SetHomeOverride(t.TempDir())
	defer SetHomeOverride("")
	prefix := t.TempDir()
	fakeBinIn(t, filepath.Join(prefix, "bin"), "context-mode")
	t.Setenv("PATH", t.TempDir())
	before := os.Getenv("PATH")

	orig := npmPrefix
	npmPrefix = func() string { return prefix }
	defer func() { npmPrefix = orig }()

	got := PickMcpSpawn("context-mode")
	if after := os.Getenv("PATH"); after != before {
		t.Fatalf("PATH changed on a hit: %q -> %q", before, after)
	}
	want := filepath.Join(prefix, "bin", "context-mode")
	if !slices.Contains(got.Args, want) {
		t.Fatalf("want absolute %q in args, got %v", want, got.Args)
	}
}

// A miss must not put the npm bin dir on PATH: later lookups (npx) would change and wiring look stale.
func TestPickMcpSpawnMissLeavesPathUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell shim")
	}
	SetHomeOverride(t.TempDir())
	defer SetHomeOverride("")
	prefix := t.TempDir()
	fakeBinIn(t, filepath.Join(prefix, "bin"), "npx")
	t.Setenv("PATH", t.TempDir())
	before := os.Getenv("PATH")

	orig := npmPrefix
	npmPrefix = func() string { return prefix }
	defer func() { npmPrefix = orig }()

	first := PickMcpSpawn("context-mode")
	if after := os.Getenv("PATH"); after != before {
		t.Fatalf("PATH changed on a miss: %q -> %q", before, after)
	}
	if second := PickMcpSpawn("context-mode"); !slices.Equal(first.Args, second.Args) {
		t.Fatalf("spawn changed between calls: %v vs %v", first.Args, second.Args)
	}
}
