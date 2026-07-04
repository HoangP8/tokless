package util

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func writeHealthyCodegraph(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo 1.2.3; exit 0; fi
if [ "$1" = "serve" ] && [ "$2" = "--mcp" ]; then
  while IFS= read -r line; do
    case "$line" in
      *'"id":1'*|*'"id": 1'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"codegraph","version":"1.2.3"}}}' ;;
      *'"id":2'*|*'"id": 2'*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"codegraph_explore"}]}}'; exit 0 ;;
    esac
  done
fi
exit 1
`
	os.WriteFile(path, []byte(script), 0755)
}

func writeFramedCodegraph(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/bash
if [ "$1" = "--version" ]; then echo 1.2.3; exit 0; fi
if [ "$1" = "serve" ] && [ "$2" = "--mcp" ]; then
  while true; do
    len=0
    while IFS= read -r line; do
      line=${line%$'\r'}
      [ -z "$line" ] && break
      case "$line" in Content-Length:*) len=${line#Content-Length: } ;; esac
    done
    [ "$len" -gt 0 ] || exit 1
    IFS= read -r -N "$len" body || exit 1
    if [[ "$body" == *'"id":1'* || "$body" == *'"id": 1'* ]]; then
      resp='{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"codegraph","version":"1.2.3"}}}'
      printf 'Content-Length: %s\r\n\r\n%s' "${#resp}" "$resp"
    fi
    if [[ "$body" == *'"id":2'* || "$body" == *'"id": 2'* ]]; then
      resp='{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"codegraph_explore"}]}}'
      printf 'Content-Length: %s\r\n\r\n%s' "${#resp}" "$resp"
      exit 0
    fi
  done
fi
exit 1
`
	os.WriteFile(path, []byte(script), 0755)
}

func TestPickMcpSpawnWindowsCmdShim(t *testing.T) {
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()

	IsWin = true

	t.Setenv("PATHEXT", ".EXE;.CMD")

	// 1. file is context-mode.cmd → must be wrapped in `cmd /c`.
	cmdDir := t.TempDir()
	os.WriteFile(filepath.Join(cmdDir, "context-mode.cmd"), []byte("dummy"), 0755)
	os.WriteFile(filepath.Join(cmdDir, "context-mode.CMD"), []byte("dummy"), 0755)

	t.Setenv("PATH", cmdDir)

	spawnCmd := PickMcpSpawn("context-mode", "serve", "--mcp")
	if spawnCmd.Command != "cmd" {
		t.Errorf("Expected Command == cmd, got %s", spawnCmd.Command)
	}
	expectedArgs := []string{"/c", Which("context-mode"), "serve", "--mcp"}
	if !filepath.IsAbs(expectedArgs[1]) {
		t.Fatalf("test setup: Which(context-mode) not absolute: %q", expectedArgs[1])
	}
	if !reflect.DeepEqual(spawnCmd.Args, expectedArgs) {
		t.Errorf("Expected Args == %v, got %v", expectedArgs, spawnCmd.Args)
	}

	// 2. file is context-mode.exe → spawned directly, no wrapper.
	exeDir := t.TempDir()
	os.WriteFile(filepath.Join(exeDir, "context-mode.exe"), []byte("dummy"), 0755)
	os.WriteFile(filepath.Join(exeDir, "context-mode.EXE"), []byte("dummy"), 0755)
	t.Setenv("PATH", exeDir+";"+cmdDir)

	spawnExe := PickMcpSpawn("context-mode", "serve", "--mcp")
	if spawnExe.Command != Which("context-mode") || !filepath.IsAbs(spawnExe.Command) {
		t.Errorf("Expected Command == absolute exe path %q, got %s", Which("context-mode"), spawnExe.Command)
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

	spawnFallback := PickMcpSpawn("context-mode")
	if spawnFallback.Command != "cmd" {
		t.Errorf("Expected fallback Command == cmd, got %s", spawnFallback.Command)
	}
	expectedFallbackArgs := []string{"/c", Which("npx"), "--no-install", "context-mode"}
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
	writeHealthyCodegraph(t, binPath)

	spawn := PickMcpSpawn("codegraph")
	if spawn.Command != binPath {
		t.Errorf("Expected Command == %s (absolute resolved path), got %s", binPath, spawn.Command)
	}
}

func TestPickMcpSpawnRejectsBrokenCodegraph(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	origHome := homeOverride
	defer func() { homeOverride = origHome }()
	SetHomeOverride(t.TempDir())
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()
	IsWin = false

	tempDir := t.TempDir()
	os.WriteFile(filepath.Join(tempDir, "codegraph"), []byte("#!/bin/sh\necho broken\n"), 0755)
	t.Setenv("PATH", tempDir)

	spawn := PickMcpSpawn("codegraph", "serve", "--mcp")
	if filepath.Base(spawn.Command) != "npx" {
		t.Fatalf("Expected npx fallback when codegraph binary is broken, got %q", spawn.Command)
	}
}

func TestCodegraphProbeSupportsFramedMcp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	origHome := homeOverride
	defer func() { homeOverride = origHome }()
	SetHomeOverride(t.TempDir())
	origIsWin := IsWin
	defer func() { IsWin = origIsWin }()
	IsWin = false

	tempDir := t.TempDir()
	bin := filepath.Join(tempDir, "codegraph")
	writeFramedCodegraph(t, bin)
	t.Setenv("PATH", tempDir)

	if !CodegraphBinaryHealthy(bin) {
		t.Fatalf("Expected framed MCP codegraph probe to pass")
	}
}
