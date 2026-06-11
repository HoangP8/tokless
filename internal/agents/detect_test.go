package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// restrictPath points PATH at an empty dir so no real CLI leaks in.
func restrictPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestDetectAgentCLIWins(t *testing.T) {
	restrictPath(t)
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "fakecli")
	if util.IsWin {
		bin += ".exe"
	}
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)

	d := detectAgent("fakecli", t.TempDir(), []string{binDir}, nil)
	if !d.Installed || d.Source != "cli" {
		t.Fatalf("CLI in known dir should detect as cli, got %+v", d)
	}
}

func TestDetectAgentDesktopFallback(t *testing.T) {
	restrictPath(t)
	app := filepath.Join(t.TempDir(), "Fake.app")
	os.MkdirAll(app, 0o755)

	d := detectAgent("no-such-cli", t.TempDir(), nil, []string{app})
	if !d.Installed || d.Source != "desktop" {
		t.Fatalf("desktop app present should detect as desktop, got %+v", d)
	}
}

func TestDetectAgentDesktopMissing(t *testing.T) {
	restrictPath(t)
	d := detectAgent("no-such-cli", t.TempDir(), nil, []string{filepath.Join(t.TempDir(), "absent.app")})
	if d.Installed {
		t.Fatalf("nothing present should be not installed, got %+v", d)
	}
}

func TestDetectAgentBothSurfaces(t *testing.T) {
	restrictPath(t)
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "fakecli")
	if util.IsWin {
		bin += ".exe"
	}
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	app := filepath.Join(t.TempDir(), "Fake.app")
	os.MkdirAll(app, 0o755)

	d := detectAgent("fakecli", t.TempDir(), []string{binDir}, []string{app})
	if !d.Installed || d.Source != "cli+desktop" {
		t.Fatalf("both surfaces present should report cli+desktop, got %+v", d)
	}
}

// setGoos overrides the OS seam for desktop path resolution.
func setGoos(t *testing.T, goos string) {
	t.Helper()
	old := goosForDetect
	goosForDetect = goos
	t.Cleanup(func() { goosForDetect = old })
}

func TestOpencodeDesktopPathsPerOS(t *testing.T) {
	setGoos(t, "windows")
	t.Setenv("LOCALAPPDATA", `C:\Users\u\AppData\Local`)
	got := opencodeDesktopPaths()
	want := filepath.Join(`C:\Users\u\AppData\Local`, "Programs", "OpenCode", "OpenCode.exe")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("windows: want [%s], got %v", want, got)
	}

	setGoos(t, "darwin")
	got = opencodeDesktopPaths()
	if len(got) != 1 || got[0] != "/Applications/OpenCode.app" {
		t.Fatalf("darwin: got %v", got)
	}

	setGoos(t, "linux")
	got = opencodeDesktopPaths()
	if len(got) != 1 || got[0] != "/usr/bin/ai.opencode.desktop" {
		t.Fatalf("linux: got %v", got)
	}
}

func TestClaudeDesktopPathsPerOS(t *testing.T) {
	setGoos(t, "windows")
	t.Setenv("LOCALAPPDATA", `C:\Users\u\AppData\Local`)
	t.Setenv("APPDATA", `C:\Users\u\AppData\Roaming`)
	got := claudeDesktopPaths()
	if len(got) != 2 ||
		got[0] != filepath.Join(`C:\Users\u\AppData\Local`, "AnthropicClaude", "claude.exe") ||
		got[1] != filepath.Join(`C:\Users\u\AppData\Roaming`, "Claude", "claude.exe") {
		t.Fatalf("windows: got %v", got)
	}

	setGoos(t, "darwin")
	got = claudeDesktopPaths()
	if len(got) != 1 || got[0] != "/Applications/Claude.app" {
		t.Fatalf("darwin: got %v", got)
	}

	// No Claude Desktop on Linux — must return nothing.
	setGoos(t, "linux")
	if got = claudeDesktopPaths(); len(got) != 0 {
		t.Fatalf("linux: expected no paths, got %v", got)
	}
}
