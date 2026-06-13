package util

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestPickMcpSpawnWindowsCmdShim(t *testing.T) {
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()

	IsWin = true

	t.Setenv("PATHEXT", ".EXE;.CMD")

	// 1. file is codegraph.cmd → must be wrapped in `cmd /c`.
	cmdDir := t.TempDir()
	os.WriteFile(filepath.Join(cmdDir, "codegraph.cmd"), []byte("dummy"), 0755)
	os.WriteFile(filepath.Join(cmdDir, "codegraph.CMD"), []byte("dummy"), 0755)

	t.Setenv("PATH", cmdDir)

	spawnCmd := PickMcpSpawn("codegraph", "serve", "--mcp")
	if spawnCmd.Command != "cmd" {
		t.Errorf("Expected Command == cmd, got %s", spawnCmd.Command)
	}
	expectedArgs := []string{"/c", Which("codegraph"), "serve", "--mcp"}
	if !filepath.IsAbs(expectedArgs[1]) {
		t.Fatalf("test setup: Which(codegraph) not absolute: %q", expectedArgs[1])
	}
	if !reflect.DeepEqual(spawnCmd.Args, expectedArgs) {
		t.Errorf("Expected Args == %v, got %v", expectedArgs, spawnCmd.Args)
	}

	// 2. file is codegraph.exe → spawned directly, no wrapper.
	exeDir := t.TempDir()
	os.WriteFile(filepath.Join(exeDir, "codegraph.exe"), []byte("dummy"), 0755)
	os.WriteFile(filepath.Join(exeDir, "codegraph.EXE"), []byte("dummy"), 0755)
	t.Setenv("PATH", exeDir+";"+cmdDir)

	spawnExe := PickMcpSpawn("codegraph", "serve", "--mcp")
	if spawnExe.Command != Which("codegraph") || !filepath.IsAbs(spawnExe.Command) {
		t.Errorf("Expected Command == absolute exe path %q, got %s", Which("codegraph"), spawnExe.Command)
	}
	expectedExeArgs := []string{"serve", "--mcp"}
	if !reflect.DeepEqual(spawnExe.Args, expectedExeArgs) {
		t.Errorf("Expected Args == %v, got %v", expectedExeArgs, spawnExe.Args)
	}

	// 3. binary absent → npx fallback, npx itself is a .cmd shim → wrapped.
	npxDir := t.TempDir()
	os.WriteFile(filepath.Join(npxDir, "npx.cmd"), []byte("dummy"), 0755)
	os.WriteFile(filepath.Join(npxDir, "npx.CMD"), []byte("dummy"), 0755)
	t.Setenv("PATH", npxDir)

	spawnFallback := PickMcpSpawn("codegraph")
	if spawnFallback.Command != "cmd" {
		t.Errorf("Expected fallback Command == cmd, got %s", spawnFallback.Command)
	}
	expectedFallbackArgs := []string{"/c", Which("npx"), "--no-install", "@colbymchenry/codegraph"}
	if !filepath.IsAbs(expectedFallbackArgs[1]) {
		t.Fatalf("test setup: Which(npx) not absolute: %q", expectedFallbackArgs[1])
	}
	if !reflect.DeepEqual(spawnFallback.Args, expectedFallbackArgs) {
		t.Errorf("Expected fallback Args == %v, got %v", expectedFallbackArgs, spawnFallback.Args)
	}
}

func TestPickMcpSpawnIsWinFalse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-semantics emulation not runnable on windows")
	}
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()

	IsWin = false
	tempDir := t.TempDir()
	t.Setenv("PATH", tempDir)

	// 3. file is codegraph (chmod 0755)
	binPath := filepath.Join(tempDir, "codegraph")
	os.WriteFile(binPath, []byte("dummy"), 0755)

	spawn := PickMcpSpawn("codegraph")
	if spawn.Command != binPath {
		t.Errorf("Expected Command == %s (absolute resolved path), got %s", binPath, spawn.Command)
	}
}
