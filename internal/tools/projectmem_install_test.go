package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// TestProjectmemEnsureInstalledBootstrapsUV: with uv absent, the installer hook
// creates a fake uv, and `uv tool install projectmem` is expected to leave
// pjm-mcp on PATH.
func TestProjectmemEnsureInstalledBootstrapsUV(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is unix-only")
	}
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("TOKLESS_TEST", "0")
	util.SetHomeOverride(dir)
	t.Cleanup(func() { util.SetHomeOverride("") })

	pjmMcp := filepath.Join(dir, "pjm-mcp")
	uvPath := filepath.Join(dir, "uv")
	// Fake uv: mimics `uv tool install projectmem` by dropping a pjm-mcp shim.
	fakeUV := "#!/bin/sh\n" +
		"cat > " + util.ShQuote(pjmMcp) + " <<'PJM'\n#!/bin/sh\nexit 0\nPJM\n" +
		"chmod 0755 " + util.ShQuote(pjmMcp) + "\n" +
		"exit 0\n"

	restore := util.SetUVInstallCmdForTest(func(_ context.Context, name string, args []string) error {
		return os.WriteFile(uvPath, []byte(fakeUV), 0o755)
	})
	t.Cleanup(restore)

	ok, err := projectmemEnsureInstalled(core.RunOpts{})
	if err != nil {
		t.Fatalf("projectmemEnsureInstalled: %v", err)
	}
	if !ok {
		t.Fatal("projectmemEnsureInstalled reported not installed")
	}
	if !util.Exists(pjmMcp) {
		t.Fatalf("fake uv did not run projectmem install; %s missing", pjmMcp)
	}
}

// TestProjectmemEnsureInstalledNoUVKeepsGuidance: in test mode (installer
// skipped) a missing uv must yield the retained projectmem guidance.
func TestProjectmemEnsureInstalledNoUVKeepsGuidance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	util.SetHomeOverride(dir)
	t.Cleanup(func() { util.SetHomeOverride("") })

	ok, err := projectmemEnsureInstalled(core.RunOpts{})
	if ok || err == nil {
		t.Fatalf("expected failure without uv, got ok=%v err=%v", ok, err)
	}
	if !strings.Contains(err.Error(), "uv tool install projectmem") {
		t.Fatalf("error must retain projectmem guidance, got: %v", err)
	}
}

// TestInstallClaudeProjectmemHookSkipsEmptyCommand: with no marker and no
// tokless on PATH, ToklessPersistedAbs() returns "" in test mode — but the
// writer must NOT persist `"command": ""`.
func TestInstallClaudeProjectmemHookSkipsEmptyCommand(t *testing.T) {
	oldWin := util.IsWin
	util.IsWin = true
	t.Cleanup(func() { util.IsWin = oldWin })

	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("TOKLESS_TEST", "0")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", "")
	t.Cleanup(func() { util.SetHomeOverride("") })

	if exe := util.ToklessPersistedAbs(); exe != "" {
		t.Fatalf("precondition: ToklessPersistedAbs() = %q, want empty", exe)
	}

	settingsPath := util.ClaudeCodePaths().Settings
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {"type": "command", "command": "foreign-cmd", "timeout": 10}
        ]
      }
    ]
  }
}`
	if err := os.WriteFile(settingsPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if installClaudeProjectmemHook() {
		t.Fatal("installClaudeProjectmemHook must return false when tokless exe is empty")
	}

	after, ok := util.ReadFileSafe(settingsPath)
	if !ok {
		t.Fatal("settings.json must still exist")
	}
	if strings.Contains(after, `"command": ""`) || strings.Contains(after, `"command":""`) {
		t.Fatalf("empty command must never be persisted; got:\n%s", after)
	}
	if !strings.Contains(after, "foreign-cmd") {
		t.Fatalf("foreign hook must be preserved; got:\n%s", after)
	}
}

// TestEnsureProjectmemProjectRemovesAIInstructions: with the upstream
// AI_INSTRUCTIONS.md present, EnsureProjectmemProject must delete it — both on
// the TOKLESS_TEST=1 fast path and (via a fake pjm) on the fresh-init path.
func TestEnsureProjectmemProjectRemovesAIInstructions(t *testing.T) {
	const aiInstr = "## Project Memory (projectmem)"
	remove := func(t *testing.T, root string) {
		t.Helper()
		if _, err := os.Stat(filepath.Join(root, ".projectmem", "AI_INSTRUCTIONS.md")); err == nil {
			t.Fatal("AI_INSTRUCTIONS.md must be gone after EnsureProjectmemProject")
		}
	}

	// Case 1: TOKLESS_TEST=1 fast path — pre-gate removal must fire.
	t.Run("test_mode", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, ".projectmem")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "AI_INSTRUCTIONS.md"), []byte(aiInstr), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TOKLESS_TEST", "1")
		if err := EnsureProjectmemProject(root); err != nil {
			t.Fatalf("EnsureProjectmemProject: %v", err)
		}
		remove(t, root)
	})

	// Case 2: fresh init — pjm init recreates AI_INSTRUCTIONS.md; the post-init
	// removal must delete it. Faked pjm writes the file then reports success.
	t.Run("fresh_init", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("shell-script fixture is unix-only")
		}
		root := t.TempDir()
		bin := t.TempDir()
		t.Setenv("PATH", bin)
		t.Setenv("HOME", bin)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(bin, ".config"))
		t.Setenv("TOKLESS_TEST", "0")
		util.SetHomeOverride(bin)
		t.Cleanup(func() { util.SetHomeOverride("") })
		fakePjm := "#!/bin/sh\n" +
			"PATH=$PATH:/usr/bin:/bin\n" +
			"root=$(pwd)\n" +
			"mkdir -p \"$root/.projectmem\"\n" +
			"printf '%s\\n' '" + aiInstr + "' > \"$root/.projectmem/AI_INSTRUCTIONS.md\"\n" +
			": > \"$root/.projectmem/PJM_RAN\"\n" +
			"exit 0\n"
		pjmPath := filepath.Join(bin, "pjm")
		if err := os.WriteFile(pjmPath, []byte(fakePjm), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := EnsureProjectmemProject(root); err != nil {
			t.Fatalf("EnsureProjectmemProject: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, ".projectmem", "PJM_RAN")); err != nil {
			t.Fatalf("fake pjm init did not run: %v", err)
		}
		remove(t, root)
	})
}
