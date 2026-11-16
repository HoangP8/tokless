package util

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func fakeBinIn(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho 1.2.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// skipIfRealCodegraphNearNode skips when a real codegraph sits where the node-dir fallback looks.
func skipIfRealCodegraphNearNode(t *testing.T) {
	t.Helper()
	if n := ResolveNodeBinary(); n != "" && Exists(filepath.Join(filepath.Dir(n), "codegraph")) {
		t.Skip("real codegraph next to node")
	}
}

// A codegraph under a custom npm prefix that is not on PATH must still resolve (issue #69).
func TestResolveCodegraphBinCustomNpmPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell shim")
	}
	home := t.TempDir()
	SetHomeOverride(home)
	defer SetHomeOverride("")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("npm_config_prefix", "")
	t.Setenv("NPM_CONFIG_PREFIX", "")
	skipIfRealCodegraphNearNode(t)

	prefix := filepath.Join(home, ".npm-global")
	want := fakeBinIn(t, filepath.Join(prefix, "bin"), "codegraph")

	orig := npmPrefix
	npmPrefix = func() string { return prefix }
	defer func() { npmPrefix = orig }()

	if got := ResolveCodegraphBin(); got != want {
		t.Fatalf("custom npm prefix: want %q, got %q", want, got)
	}
}

// npm_config_prefix resolves even when `npm config get prefix` yields nothing.
func TestResolveCodegraphBinEnvNpmPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell shim")
	}
	SetHomeOverride(t.TempDir())
	defer SetHomeOverride("")
	t.Setenv("PATH", t.TempDir())
	skipIfRealCodegraphNearNode(t)
	prefix := t.TempDir()
	t.Setenv("npm_config_prefix", prefix)
	want := fakeBinIn(t, filepath.Join(prefix, "bin"), "codegraph")

	orig := npmPrefix
	npmPrefix = func() string { return "" }
	defer func() { npmPrefix = orig }()

	if got := ResolveCodegraphBin(); got != want {
		t.Fatalf("env npm prefix: want %q, got %q", want, got)
	}
}

// context-mode under a custom npm prefix must spawn by absolute path, not npx (issue #69).
func TestPickMcpSpawnContextModeCustomNpmPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell shim")
	}
	SetHomeOverride(t.TempDir())
	defer SetHomeOverride("")
	t.Setenv("PATH", t.TempDir())
	prefix := t.TempDir()
	want := fakeBinIn(t, filepath.Join(prefix, "bin"), "context-mode")

	orig := npmPrefix
	npmPrefix = func() string { return prefix }
	defer func() { npmPrefix = orig }()

	spawn := PickMcpSpawn("context-mode")
	if !slices.Contains(spawn.Args, want) || slices.Contains(spawn.Args, "--no-install") {
		t.Fatalf("want absolute %q and no npx fallback, got %v", want, spawn.Args)
	}
}
