package agents

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
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

func TestAgentsDetectVSCodeExtensions(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	restrictPath(t)
	for _, test := range []struct {
		name       string
		extensions []string
		detect     func() core.Detection
	}{
		{"Copilot", []string{"github.copilot-chat"}, copilot.Detect},
		{"Kilo", []string{"kilocode.kilo-code"}, kilo.Detect},
		{"Codex", []string{"openai.chatgpt"}, codex.Detect},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, extension := range test.extensions {
				dir := filepath.Join(home, ".vscode", "extensions", extension+"-1.0.0")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := test.detect(); !got.Installed || got.Source != "ide" {
				t.Fatalf("IDE detection = %+v, want ide", got)
			}
		})
	}
}

func TestDetectCopilotChatExtensionWithoutCompletion(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	restrictPath(t)
	if err := os.MkdirAll(filepath.Join(home, ".vscode", "extensions", "github.copilot-chat-1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := copilot.Detect(); !got.Installed || got.Source != "ide" {
		t.Fatalf("Copilot Chat without completion = %+v, want ide", got)
	}
}

func TestCopilotExtensionDoesNotEnableCLIProxy(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	restrictPath(t)
	if err := os.MkdirAll(filepath.Join(home, ".vscode", "extensions", "github.copilot-chat-1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ProxyAgentApplicable("copilot") {
		t.Fatal("Copilot Chat extension must not enable Copilot CLI proxy routing")
	}
}

func TestVSCodeExtensionInstalledPathsAndExactMatch(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("VSCODE_EXTENSIONS", "")
	t.Setenv("XDG_DATA_HOME", "")

	for _, dir := range vscodeExtensionDirs() {
		if err := os.MkdirAll(filepath.Join(dir, "Publisher.Extension-1.0.0"), 0o755); err != nil {
			t.Fatal(err)
		}
		if !vscodeExtensionInstalled("publisher.extension") {
			t.Fatalf("extension in %s was not detected", dir)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(vscodeExtensionDirs()[0], "publisher.extensionx-1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if vscodeExtensionInstalled("publisher.extension") {
		t.Fatal("near-match extension was detected")
	}
}

func TestVSCodeExtensionInstalledUsesEnvironmentRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VSCODE_EXTENSIONS", dir)
	if err := os.MkdirAll(filepath.Join(dir, "publisher.extension-1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !vscodeExtensionInstalled("publisher.extension") {
		t.Fatal("extension in VSCODE_EXTENSIONS was not detected")
	}
}

func TestVSCodeExtensionInstalledIgnoresObsoleteExtension(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	dir := t.TempDir()
	t.Setenv("VSCODE_EXTENSIONS", dir)
	name := "publisher.extension-1.0.0"
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".obsolete"), []byte(`{"`+name+`":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if vscodeExtensionInstalled("publisher.extension") {
		t.Fatal("obsolete extension was detected")
	}
}

func TestVSCodeExtensionInstalledUsesXDGCodeServerRoot(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)
	dir := filepath.Join(dataDir, "code-server", "extensions", "publisher.extension-1.0.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !vscodeExtensionInstalled("publisher.extension") {
		t.Fatal("extension in XDG code-server root was not detected")
	}
}

func TestDetectVSCodeAgentCombinesCLIAndIDE(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	restrictPath(t)
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "fakecli")
	if util.IsWin {
		bin += ".exe"
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".vscode", "extensions", "publisher.extension-1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := detectVSCodeAgent("fakecli", t.TempDir(), []string{binDir}, "publisher.extension"); !got.Installed || got.Source != "cli+ide" {
		t.Fatalf("CLI + VS Code detection = %+v, want cli+ide", got)
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
	_ = os.WriteFile(p.Settings, []byte(`{"mcpServers":{"codegraph":{"command":"old"}},"permissions":{"allow":["mcp(codegraph/*)"]}}`), 0o644)
	_ = os.MkdirAll(filepath.Dir(p.McpConfigCLI), 0o755)
	_ = os.WriteFile(p.McpConfigCLI, []byte(`{"shared-setting":true,"mcpServers":{"shared-user":{"command":"keep-shared"}}}`), 0o644)
	cliMcp := filepath.Join(home, ".gemini", "antigravity-cli", "mcp_config.json")
	_ = os.MkdirAll(filepath.Dir(cliMcp), 0o755)
	_ = os.WriteFile(cliMcp, []byte(`{"cli-setting":true,"mcpServers":{"cli-user":{"command":"keep-cli"}}}`), 0o644)

	changed, _ := ConfigureAntigravityMcp("codegraph")
	if !changed {
		t.Fatal("expected first configure to write")
	}
	if changed, _ := ConfigureAntigravityMcp("codegraph"); changed {
		t.Fatal("second configure must be idempotent")
	}
	ConfigureAntigravityMcp("context-mode")

	raw, _ := os.ReadFile(p.McpConfigCLI)
	for _, want := range []string{`"codegraph"`, `"serve"`, `"--mcp"`, `"context-mode"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("mcp_config.json missing %s:\n%s", want, raw)
		}
	}
	rawCLI, _ := os.ReadFile(cliMcp)
	for _, want := range []string{`"codegraph"`, `"serve"`, `"--mcp"`, `"context-mode"`} {
		if !strings.Contains(string(rawCLI), want) {
			t.Fatalf("CLI mcp_config.json missing %s:\n%s", want, rawCLI)
		}
	}
	if !AntigravityMcpHas("codegraph") || !AntigravityMcpHas("context-mode") {
		t.Fatal("AntigravityMcpHas should see both tools in shared and CLI configs")
	}

	rawLegacy, _ := os.ReadFile(p.McpConfig)
	if !strings.Contains(string(rawLegacy), `"user-server"`) || !strings.Contains(string(rawLegacy), `"keepme"`) || strings.Contains(string(rawLegacy), `"codegraph"`) {
		t.Fatalf("legacy config not preserved/cleaned:\n%s", rawLegacy)
	}
	rawLegacySettings, _ := os.ReadFile(p.Settings)
	if !strings.Contains(string(rawLegacySettings), `"codegraph"`) || strings.Contains(string(rawLegacySettings), `mcp(codegraph/*)`) {
		t.Fatalf("legacy settings not cleaned:\n%s", rawLegacySettings)
	}
	rawCliSettings, _ := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"))
	if !strings.Contains(string(rawCliSettings), `mcp(codegraph/*)`) {
		t.Fatalf("CLI settings should allow codegraph MCP:\n%s", rawCliSettings)
	}
	RemoveAntigravityMcp("codegraph")
	if AntigravityMcpHas("codegraph") {
		t.Fatal("managed codegraph entry should be removed")
	}
	if err := os.WriteFile(p.McpConfig, []byte(`{"mcpServers":{"user-server":{"command":"keepme"},"codegraph":{"command":"foreign"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Settings, []byte(`{"mcpServers":{"codegraph":{"command":"foreign"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	RemoveAntigravityMcp("codegraph")
	for _, f := range []string{p.McpConfig, p.Settings} {
		raw, _ = os.ReadFile(f)
		if !strings.Contains(string(raw), `"codegraph"`) {
			t.Fatalf("foreign codegraph entry removed from %s", f)
		}
	}
	raw, _ = os.ReadFile(p.McpConfig)
	if !strings.Contains(string(raw), `"user-server"`) {
		t.Fatalf("remove clobbered unrelated entries:\n%s", raw)
	}
	raw, _ = os.ReadFile(p.McpConfigCLI)
	for _, want := range []string{`"context-mode"`, `"shared-setting"`, `"shared-user"`, `"keep-shared"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("remove clobbered canonical entry %s:\n%s", want, raw)
		}
	}
	raw, _ = os.ReadFile(cliMcp)
	for _, want := range []string{`"context-mode"`, `"cli-setting"`, `"cli-user"`, `"keep-cli"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("remove clobbered CLI entry %s:\n%s", want, raw)
		}
	}
	if cfg := util.TryParseJsonc(string(raw)); cfg != nil {
		servers, _ := cfg.Get("mcpServers")
		servers.(*util.OrderedMap).Delete("context-mode")
		_ = os.WriteFile(cliMcp, []byte(util.StringifyJSON(cfg)), 0o644)
	}
	if !AntigravityMcpHas("context-mode") {
		t.Fatal("AntigravityMcpHas should still find context-mode in shared config")
	}
}

func TestConfigureAntigravityMcpPreservesMalformedConfigAndLegacyEntry(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	p := util.AntigravityPathsResolved()
	_ = os.MkdirAll(filepath.Dir(p.McpConfigCLI), 0o755)
	const malformed = `{"mcpServers":`
	_ = os.WriteFile(p.McpConfigCLI, []byte(malformed), 0o644)
	_ = os.MkdirAll(filepath.Dir(p.McpConfig), 0o755)
	_ = os.WriteFile(p.McpConfig, []byte(`{"mcpServers":{"codegraph":{"command":"legacy"}}}`), 0o644)

	ConfigureAntigravityMcp("codegraph")
	raw, _ := os.ReadFile(p.McpConfigCLI)
	if string(raw) != malformed {
		t.Fatalf("malformed config was overwritten: %q", raw)
	}
	raw, _ = os.ReadFile(p.McpConfig)
	if !strings.Contains(string(raw), `"codegraph"`) {
		t.Fatalf("legacy fallback was removed after partial configure: %s", raw)
	}
	if AntigravityMcpHas("codegraph") {
		t.Fatal("malformed canonical config must not count as wired")
	}
	if util.Exists(filepath.Join(home, ".gemini", "antigravity-cli", "mcp_config.json")) {
		t.Fatal("mirror file must not be created")
	}
}

func agyTestSetup(t *testing.T) (home string, primary string, mirror string) {
	t.Helper()
	home = t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	primary = util.AntigravityPathsResolved().McpConfigCLI
	mirror = filepath.Join(home, ".gemini", "antigravity-cli", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(primary), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, primary, mirror
}

func TestConfigureAntigravityMcpUTF8BOMPrimary(t *testing.T) {
	_, primary, _ := agyTestSetup(t)
	_ = os.WriteFile(primary, []byte("\xEF\xBB\xBF"+`{"mcpServers":{"user-server":{"command":"keepme"}}}`), 0o644)

	changed, _ := ConfigureAntigravityMcp("codegraph")
	if !changed {
		t.Fatal("expected write")
	}
	if !AntigravityMcpBounded("codegraph") {
		t.Fatal("codegraph should be wired in BOM-prefixed canonical config")
	}
	raw, _ := os.ReadFile(primary)
	if !strings.Contains(string(raw), `"keepme"`) {
		t.Fatalf("user server lost:\n%s", raw)
	}
	if changed, _ := ConfigureAntigravityMcp("codegraph"); changed {
		t.Fatal("second configure must write nothing")
	}
}

func TestConfigureAntigravityMcpNormalizesBOMWithEqualEntry(t *testing.T) {
	_, primary, _ := agyTestSetup(t)
	_ = os.WriteFile(primary, []byte(`{}`), 0o644)
	if changed, _ := ConfigureAntigravityMcp("codegraph"); !changed {
		t.Fatal("expected initial write")
	}
	raw, _ := os.ReadFile(primary)
	_ = os.WriteFile(primary, append([]byte{0xEF, 0xBB, 0xBF}, raw...), 0o644)
	if !AntigravityMcpBounded("codegraph") {
		t.Fatal("BOM-prefixed config with the right entry must verify")
	}
	if changed, _ := ConfigureAntigravityMcp("codegraph"); !changed {
		t.Fatal("an equal entry in a BOM file must be re-encoded once")
	}
	got, _ := os.ReadFile(primary)
	if bytes.HasPrefix(got, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("BOM was not stripped")
	}
}

func TestConfigureAntigravityMcpLogsParseError(t *testing.T) {
	_, primary, _ := agyTestSetup(t)
	_ = os.WriteFile(primary, []byte(`{"mcpServers":`), 0o644)

	out, _ := util.CaptureLogs(func() error {
		ConfigureAntigravityMcp("codegraph")
		return nil
	})
	if !strings.Contains(out, "couldn't parse Antigravity MCP config") || !strings.Contains(out, primary) {
		t.Fatalf("parse failure must name the file, got:\n%s", out)
	}
}

func TestConfigureAntigravityMcpBadMirrorDoesNotVetoWiring(t *testing.T) {
	_, primary, mirror := agyTestSetup(t)
	_ = os.WriteFile(primary, []byte(`{}`), 0o644)
	_ = os.MkdirAll(filepath.Dir(mirror), 0o755)
	_ = os.WriteFile(mirror, []byte(`{invalid`), 0o644)

	ConfigureAntigravityMcp("codegraph")
	if !AntigravityMcpBounded("codegraph") {
		t.Fatal("primary match must be enough")
	}
	if raw, _ := os.ReadFile(mirror); string(raw) != `{invalid` {
		t.Fatalf("bad mirror modified: %q", raw)
	}
	if raw, _ := os.ReadFile(antigravitySettingsFiles()[0]); !strings.Contains(string(raw), `mcp(codegraph/*)`) {
		t.Fatalf("permissions allow rule missing despite bad mirror:\n%s", raw)
	}
}

func TestConfigureAntigravityMcpAlwaysSyncsMirror(t *testing.T) {
	_, primary, mirror := agyTestSetup(t)
	_ = os.WriteFile(primary, []byte(`{}`), 0o644)
	ConfigureAntigravityMcp("codegraph")
	raw, err := os.ReadFile(mirror)
	if err != nil {
		t.Fatalf("mirror not created on a clean home: %v", err)
	}
	if !strings.Contains(string(raw), `"codegraph"`) {
		t.Fatalf("mirror missing the entry:\n%s", raw)
	}
	_ = os.WriteFile(mirror, []byte(`{"mcpServers":{"u":{"command":"x"}}}`), 0o644)
	ConfigureAntigravityMcp("context-mode")
	raw, _ = os.ReadFile(mirror)
	if !strings.Contains(string(raw), `"context-mode"`) || !strings.Contains(string(raw), `"u"`) {
		t.Fatalf("existing mirror not synced:\n%s", raw)
	}
}

func TestConfigureAntigravityMcpAdoptsStaleToklessPath(t *testing.T) {
	_, primary, _ := agyTestSetup(t)
	_ = os.WriteFile(primary, []byte(`{"mcpServers":{"projectmem":{"command":"C:\\old\\bin\\tokless.exe","args":["run-mcp","--tool","projectmem","pjm-mcp"]}}}`), 0o644)
	if AntigravityMcpBounded("projectmem") {
		t.Fatal("stale path must not verify")
	}
	if changed, _ := ConfigureAntigravityMcp("projectmem"); !changed {
		t.Fatal("stale tokless entry should be rewritten")
	}
	if !AntigravityMcpBounded("projectmem") {
		t.Fatal("rewritten entry should verify")
	}
	raw, _ := os.ReadFile(primary)
	if !strings.Contains(string(raw), `"trust": true`) || strings.Contains(string(raw), `C:\\old`) {
		t.Fatalf("entry not refreshed:\n%s", raw)
	}
}

func TestCopilotVSCodeUTF16SettingsNotCorrupted(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	ext := filepath.Join(home, ".vscode", "extensions", "github.copilot-chat-1.0.0")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	path := copilotVSCodeSettingsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	u16 := []byte{0xFF, 0xFE}
	for _, c := range []byte(`{"editor.fontSize":14}`) {
		u16 = append(u16, c, 0)
	}
	if err := os.WriteFile(path, u16, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := configureCopilotVSCodeSettingsLocked(); err == nil {
		t.Fatal("UTF-16 settings must be rejected, not spliced")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, u16) {
		t.Fatalf("UTF-16 settings modified:\n%q", got)
	}
}

func TestConfigureAntigravityMcpUnreadablePrimaryNotClobbered(t *testing.T) {
	_, primary, _ := agyTestSetup(t)
	const body = `{"mcpServers":{"pencil":{"command":"keepme"}}}`
	_ = os.WriteFile(primary, []byte(body), 0o600)
	if err := os.Chmod(primary, 0o000); err != nil {
		t.Skipf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(primary, 0o600) })
	if _, err := os.ReadFile(primary); err == nil {
		t.Skip("file is still readable (running as root)")
	}

	ConfigureAntigravityMcp("codegraph")
	_ = os.Chmod(primary, 0o600)
	raw, _ := os.ReadFile(primary)
	if string(raw) != body {
		t.Fatalf("unreadable config was overwritten:\n%s", raw)
	}
}

func TestConfigureAntigravityMcpRefusesForeignEntries(t *testing.T) {
	for name, entry := range map[string]string{
		"pjm":               `{"command":"pjm","args":["mcp"]}`,
		"tokless no proxy":  `{"command":"tokless","args":["--version"]}`,
		"tokless no args":   `{"command":"tokless"}`,
		"wrong tool target": `{"command":"tokless","args":["run-mcp","--tool","codegraph","x"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, primary, _ := agyTestSetup(t)
			body := `{"mcpServers":{"projectmem":` + entry + `}}`
			_ = os.WriteFile(primary, []byte(body), 0o644)
			out, _ := util.CaptureLogs(func() error {
				if changed, _ := ConfigureAntigravityMcp("projectmem"); changed {
					t.Fatal("foreign entry must not be rewritten")
				}
				return nil
			})
			if !strings.Contains(out, "mcpServers.projectmem") || !strings.Contains(out, primary) {
				t.Fatalf("refusal must name the file and key, got:\n%s", out)
			}
			if AntigravityMcpBounded("projectmem") {
				t.Fatal("foreign entry must not verify")
			}
			if raw, _ := os.ReadFile(primary); string(raw) != body {
				t.Fatalf("foreign entry modified: %s", raw)
			}
		})
	}
}

func TestAntigravityProjectmemEntryIsBoundedProxy(t *testing.T) {
	entry := AntigravityMcpEntry("projectmem")
	args, _ := entry.Get("args")
	got, _ := args.([]string)
	if len(got) < 4 || got[0] != "run-mcp" || got[1] != "--tool" || got[2] != "projectmem" {
		t.Fatalf("projectmem must route through run-mcp --tool projectmem <inner>, got %v", got)
	}
	if trust, _ := entry.Get("trust"); trust != true {
		t.Fatal("new entries keep trust: true")
	}
}

func TestAntigravityMcpHasAnySemantics(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	p := util.AntigravityPathsResolved()

	// Case 1: Both configs empty → false
	if AntigravityMcpHas("codegraph") {
		t.Fatal("should be false when no configs exist")
	}

	// Case 2: Only shared config has tool → true
	_ = os.MkdirAll(filepath.Dir(p.McpConfigCLI), 0o755)
	_ = os.WriteFile(p.McpConfigCLI, []byte(`{"mcpServers":{"codegraph":{"command":"cg"}}}`), 0o644)
	if !AntigravityMcpHas("codegraph") {
		t.Fatal("should be true when shared config has the tool")
	}

	// Case 3: Canonical is malformed, mirror has tool → false (canonical only)
	_ = os.WriteFile(p.McpConfigCLI, []byte(`{invalid`), 0o644)
	cliMcp := filepath.Join(home, ".gemini", "antigravity-cli", "mcp_config.json")
	_ = os.MkdirAll(filepath.Dir(cliMcp), 0o755)
	_ = os.WriteFile(cliMcp, []byte(`{"mcpServers":{"codegraph":{"command":"cg"}}}`), 0o644)
	if AntigravityMcpHas("codegraph") {
		t.Fatal("mirror alone must not count as wired")
	}

	// Case 4: Both malformed → false
	_ = os.WriteFile(cliMcp, []byte(`{invalid`), 0o644)
	if AntigravityMcpHas("codegraph") {
		t.Fatal("should be false when both configs are malformed")
	}

	// Case 5: Shared has tool, CLI missing entirely → true
	_ = os.Remove(cliMcp)
	_ = os.WriteFile(p.McpConfigCLI, []byte(`{"mcpServers":{"codegraph":{"command":"cg"}}}`), 0o644)
	if !AntigravityMcpHas("codegraph") {
		t.Fatal("should be true when shared config has tool and CLI config is missing")
	}
}

func TestRemoveAntigravityCodegraphToolDefs(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	gemini := filepath.Join(home, ".gemini")
	for _, variant := range []string{"antigravity-cli", "antigravity-ide"} {
		toolDir := filepath.Join(gemini, variant, "mcp", "codegraph")
		if err := os.MkdirAll(toolDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, file := range codegraphToolDefFiles {
			if err := os.WriteFile(filepath.Join(toolDir, file), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(toolDir, "user.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	RemoveAntigravityCodegraphToolDefs()

	for _, variant := range []string{"antigravity-cli", "antigravity-ide"} {
		toolDir := filepath.Join(gemini, variant, "mcp", "codegraph")
		for _, file := range codegraphToolDefFiles {
			if _, err := os.Stat(filepath.Join(toolDir, file)); !os.IsNotExist(err) {
				t.Fatalf("%s still exists", filepath.Join(toolDir, file))
			}
		}
		if _, err := os.Stat(filepath.Join(toolDir, "user.json")); err != nil {
			t.Fatalf("unrelated file removed: %v", err)
		}
	}
}

func TestCleanupLegacyAntigravityContextMode(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	gemini := filepath.Join(home, ".gemini")
	hooksDir := filepath.Join(gemini, "config")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hooksFile := filepath.Join(hooksDir, "hooks.json")
	original := `{"rtk":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"tokless rtk-hook agy"}]}]},"ctx":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"context-mode hook gemini-cli beforetool"}]}]},"tokless-context-mode":{"PreToolUse":[{"matcher":"","hooks":[]}]}}`
	if err := os.WriteFile(hooksFile, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	CleanupLegacyAntigravityContextMode()

	got, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatalf("hooks.json missing after cleanup: %v", err)
	}
	gs := string(got)
	if strings.Contains(gs, `"ctx"`) || strings.Contains(gs, `"tokless-context-mode"`) {
		t.Fatalf("legacy groups still present:\n%s", gs)
	}
	if !strings.Contains(gs, `"rtk"`) || !strings.Contains(gs, "rtk-hook agy") {
		t.Fatalf("unrelated rtk group was clobbered:\n%s", gs)
	}
	if strings.Contains(gs, "context-mode hook gemini-cli") {
		t.Fatalf("legacy context-mode hook command still present:\n%s", gs)
	}

	CleanupLegacyAntigravityContextMode()
	got2, _ := os.ReadFile(hooksFile)
	if string(got2) != gs {
		t.Fatalf("second pass mutated hooks.json:\n%s\nvs\n%s", got2, gs)
	}
}

// TestCleanupLegacyAntigravityContextModeNoLegacyIsNoop ensures the migration
// is a clean no-op on a hooks.json that never had a legacy group.
func TestCleanupLegacyAntigravityContextModeNoLegacyIsNoop(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	hooksFile := filepath.Join(home, ".gemini", "config", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksFile), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"rtk":{"PreToolUse":[]}}`
	if err := os.WriteFile(hooksFile, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	CleanupLegacyAntigravityContextMode()
	got, _ := os.ReadFile(hooksFile)
	if string(got) != original {
		t.Fatalf("unexpected mutation:\nbefore:\n%s\nafter:\n%s", original, got)
	}
}

// PreInvocation must be a flat handler list.
func TestInstallAntigravityCodegraphPreInvocationIsFlat(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("TOKLESS_TEST", "1")
	defer util.SetHomeOverride("")

	hooksFile := filepath.Join(home, ".gemini", "config", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksFile), 0o755); err != nil {
		t.Fatal(err)
	}
	broken := `{"tokless-codegraph-index":{"PostToolUse":[{"matcher":"","hooks":[{"type":"command","command":"tokless agy-hook codegraph-index","timeout":120}]}],"PreInvocation":[{"matcher":"","hooks":[{"type":"command","command":"tokless agy-hook codegraph-index","timeout":120}]}]}}`
	if err := os.WriteFile(hooksFile, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	InstallAntigravityCodegraphIndexHook()
	InstallAntigravityRtkHook()

	raw, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("hooks.json invalid: %v\n%s", err, raw)
	}
	cg := m["tokless-codegraph-index"].(map[string]any)
	pre := cg["PreInvocation"].([]any)
	first := pre[0].(map[string]any)
	if _, ok := first["hooks"]; ok {
		t.Fatalf("PreInvocation still grouped:\n%s", raw)
	}
	cmd, _ := first["command"].(string)
	if !strings.Contains(cmd, "agy-hook codegraph-index") {
		t.Fatalf("PreInvocation missing command:\n%s", raw)
	}
	post := cg["PostToolUse"].([]any)[0].(map[string]any)
	if _, ok := post["hooks"]; !ok {
		t.Fatalf("PostToolUse should stay grouped:\n%s", raw)
	}
	if !HasAntigravityCodegraphIndexHook() || !HasAntigravityRtkHook() {
		t.Fatalf("Has* checks failed after install:\n%s", raw)
	}
}

func TestInstallAntigravityRtkHookRefusesMalformedConfigBeforeCleanup(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	hooksFile := filepath.Join(home, ".gemini", "config", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksFile, []byte(`{"hooks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := antigravityRewriteScript()
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("user script"), 0o644); err != nil {
		t.Fatal(err)
	}
	InstallAntigravityRtkHook()
	got, _ := os.ReadFile(hooksFile)
	if string(got) != `{"hooks":[]}` {
		t.Fatalf("malformed hooks changed: %s", got)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("cleanup ran before validation: %v", err)
	}
}

func TestInstallAntigravityRtkHookRefusesMalformedNestedHooks(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	hooksFile := filepath.Join(home, ".gemini", "config", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksFile), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"hooks":{"PreToolUse":{}}}`
	if err := os.WriteFile(hooksFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if InstallAntigravityRtkHook() {
		t.Fatal("wrong-typed nested hooks must refuse install")
	}
	got, _ := os.ReadFile(hooksFile)
	if string(got) != raw {
		t.Fatalf("malformed hooks changed: %s", got)
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
