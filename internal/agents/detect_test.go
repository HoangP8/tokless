package agents

import (
	"os"
	"path/filepath"
	"strings"
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

func TestAntigravityDesktopPathsPerOS(t *testing.T) {
	setGoos(t, "windows")
	t.Setenv("LOCALAPPDATA", `C:\Users\u\AppData\Local`)
	got := antigravityDesktopPaths()
	if len(got) != 2 ||
		got[0] != filepath.Join(`C:\Users\u\AppData\Local`, "Programs", "Antigravity", "Antigravity.exe") ||
		got[1] != filepath.Join(`C:\Users\u\AppData\Local`, "Programs", "Antigravity IDE", "Antigravity IDE.exe") {
		t.Fatalf("windows: got %v", got)
	}

	setGoos(t, "darwin")
	got = antigravityDesktopPaths()
	if len(got) != 2 || got[0] != "/Applications/Antigravity.app" || got[1] != "/Applications/Antigravity IDE.app" {
		t.Fatalf("darwin: got %v", got)
	}

	setGoos(t, "linux")
	got = antigravityDesktopPaths()
	if len(got) != 4 || got[0] != "/opt/antigravity" || got[3] != "/usr/local/bin/antigravity-ide" {
		t.Fatalf("linux: got %v", got)
	}
}

func TestConfigureAntigravityMcpMergeAndRemove(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	p := util.AntigravityPathsResolved()
	_ = os.MkdirAll(p.Dir, 0o755)
	_ = os.WriteFile(p.McpConfig, []byte(`{"mcpServers":{"user-server":{"command":"keepme"}}}`), 0o644)

	changed, _ := ConfigureAntigravityMcp("codegraph")
	if !changed {
		t.Fatal("expected first configure to write")
	}
	if changed, _ := ConfigureAntigravityMcp("codegraph"); changed {
		t.Fatal("second configure must be idempotent")
	}
	ConfigureAntigravityMcp("context-mode")

	raw, _ := os.ReadFile(p.McpConfig)
	for _, want := range []string{`"user-server"`, `"keepme"`, `"codegraph"`, `"serve"`, `"--mcp"`, `"context-mode"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("mcp_config.json missing %s:\n%s", want, raw)
		}
	}
	if !AntigravityMcpHas("codegraph") || !AntigravityMcpHas("context-mode") {
		t.Fatal("AntigravityMcpHas should see both tools")
	}

	// CLI surface file must carry the same servers (agy reads config/, not antigravity/).
	rawCLI, err := os.ReadFile(p.McpConfigCLI)
	if err != nil {
		t.Fatalf("CLI mcp config not written: %v", err)
	}
	if !strings.Contains(string(rawCLI), `"codegraph"`) || !strings.Contains(string(rawCLI), `"context-mode"`) {
		t.Fatalf("CLI mcp config missing tools:\n%s", rawCLI)
	}

	RemoveAntigravityMcp("codegraph")
	if AntigravityMcpHas("codegraph") {
		t.Fatal("codegraph should be removed")
	}
	for _, f := range []string{p.McpConfig, p.McpConfigCLI} {
		raw, _ = os.ReadFile(f)
		if strings.Contains(string(raw), `"codegraph"`) {
			t.Fatalf("codegraph not removed from %s", f)
		}
	}
	raw, _ = os.ReadFile(p.McpConfig)
	if !strings.Contains(string(raw), `"user-server"`) || !strings.Contains(string(raw), `"context-mode"`) {
		t.Fatalf("remove clobbered unrelated entries:\n%s", raw)
	}
}

func TestAgyKnownBinDirsPerOS(t *testing.T) {
	setGoos(t, "windows")
	t.Setenv("LOCALAPPDATA", `C:\Users\u\AppData\Local`)
	got := agyKnownBinDirs()
	if len(got) != 1 || got[0] != filepath.Join(`C:\Users\u\AppData\Local`, "agy", "bin") {
		t.Fatalf("windows: got %v", got)
	}

	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")
	for _, goos := range []string{"darwin", "linux"} {
		setGoos(t, goos)
		got = agyKnownBinDirs()
		if len(got) != 1 || got[0] != filepath.Join(home, ".local", "bin") {
			t.Fatalf("%s: got %v", goos, got)
		}
	}
}
