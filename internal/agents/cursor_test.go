package agents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestMain(m *testing.M) {
	values := map[string]string{}
	present := map[string]bool{}
	for _, name := range []string{"CURSOR_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		values[name], present[name] = os.LookupEnv(name)
		_ = os.Unsetenv(name)
	}
	util.SetHomeOverride("")
	code := m.Run()
	for name, value := range values {
		if present[name] {
			_ = os.Setenv(name, value)
		} else {
			_ = os.Unsetenv(name)
		}
	}
	util.SetHomeOverride("")
	os.Exit(code)
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable fixture")
	}
}

func skipUnlessLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("checks Linux Cursor config paths")
	}
}

func TestCursorDetectionAcceptsCursorAgentCLI(t *testing.T) {
	skipOnWindows(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("TOKLESS_TEST", "0")
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	t.Cleanup(func() { util.SetHomeOverride("") })
	bin := filepath.Join(binDir, "cursor-agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'Cursor Agent 1.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Logf("path=%q run=%+v", cursorCLIPath(), util.Run(bin, []string{"--version"}, util.RunOptions{Capture: true, Quiet: true}))
	if got := cursor.Detect(); !got.Installed || got.Source != "cli" {
		t.Fatalf("cursor-agent not detected as Cursor CLI: %+v", got)
	}
}

func TestCursorDetectionAcceptsAgentCLI(t *testing.T) {
	skipOnWindows(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("TOKLESS_TEST", "0")
	t.Setenv("PATH", t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'Cursor Agent 1.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cursor.Detect(); !got.Installed || got.Source != "cli" {
		t.Fatalf("agent not detected as Cursor CLI: %+v", got)
	}
}

func TestCursorDetectionRejectsUnrelatedAgentCLI(t *testing.T) {
	skipOnWindows(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("TOKLESS_TEST", "0")
	t.Setenv("PATH", t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'unrelated agent 1.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cursor.Detect(); got.Installed {
		t.Fatalf("unrelated agent detected as Cursor: %+v", got)
	}
}

func TestCursorDetectionWindowsOfficialPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("ProgramFiles", filepath.Join(root, "programs"))
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TOKLESS_TEST", "0")
	oldGOOS, oldRoot := goosForDetect, cursorDetectRoot
	goosForDetect, cursorDetectRoot = "windows", ""
	t.Cleanup(func() { goosForDetect, cursorDetectRoot = oldGOOS, oldRoot })
	path := filepath.Join(root, "programs", "cursor", "Cursor.exe")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cursor.Detect(); !got.Installed || got.Source != "desktop" {
		t.Fatalf("official Windows install not detected: %+v", got)
	}
}

func TestCursorDetectionWSLWindowsDesktop(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	home := filepath.Join(root, "mnt", "c", "Users", "CursorUser")
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("PATH", filepath.Join(root, "bin"))
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"cmd.exe": "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'",
		"wslpath": "printf '%s\\n' '" + home + "'",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe := filepath.Join(home, "AppData", "Local", "Programs", "cursor", "Cursor.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cursor.Detect(); !got.Installed || got.Source != "desktop" {
		t.Fatalf("WSL Windows Cursor not detected: %+v", got)
	}
	if got := cursor.ConfigDir(); got != filepath.Join(util.Home(), ".cursor") {
		t.Fatalf("ConfigDir = %q, want %q", got, filepath.Join(home, ".cursor"))
	}
}

func TestCursorDetectionMacOfficialBundles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	oldGOOS, oldRoot := goosForDetect, cursorDetectRoot
	goosForDetect, cursorDetectRoot = "darwin", root
	t.Cleanup(func() { goosForDetect, cursorDetectRoot = oldGOOS, oldRoot })
	app := filepath.Join(root, "Applications", "Cursor Nightly.app", "Contents")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte("co.anysphere.cursor.nightly"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cursor.Detect(); !got.Installed || got.Source != "desktop" {
		t.Fatalf("official macOS bundle not detected: %+v", got)
	}
}

func TestCursorDetectionLinuxDesktopFileValidation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	oldGOOS, oldRoot := goosForDetect, cursorDetectRoot
	goosForDetect, cursorDetectRoot = "linux", root
	t.Cleanup(func() { goosForDetect, cursorDetectRoot = oldGOOS, oldRoot })
	path := filepath.Join(root, "usr", "share", "applications", "cursor.desktop")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("[Desktop Entry]\nType=Application\nName=Cursor\nExec=/opt/cursor/Cursor %U\n")
	if got := cursor.Detect(); !got.Installed || got.Source != "desktop" {
		t.Fatalf("valid Linux desktop file not detected: %+v", got)
	}
	write("[Desktop Entry]\nType=Application\nName=Cursor\nExec=/opt/other/editor %U\n")
	if got := cursor.Detect(); got.Installed {
		t.Fatalf("invalid Linux Exec detected: %+v", got)
	}
}

func TestCursorCodegraphHookIsNotInstalled(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	foreign := `{"hooks":{"workspaceOpen":[{"type":"command","command":"user-hook"}]}}`
	if err := util.WriteFile(cursorHooksFile(), foreign); err != nil {
		t.Fatal(err)
	}
	if before, _ := util.ReadFileSafe(cursorHooksFile()); before != foreign {
		t.Fatalf("foreign setup changed: %q", before)
	}
	if util.Exists(cursorHooksFile()) {
		raw, _ := util.ReadFileSafe(cursorHooksFile())
		if raw != foreign {
			t.Fatalf("foreign hook changed: %s", raw)
		}
	}
}

func TestHasCursorCodegraphIndexHookRequiresBothExactLifecycleEntries(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	if !InstallCursorCodegraphIndexHook() || !HasCursorCodegraphIndexHook() {
		t.Fatal("Cursor CodeGraph lifecycle hooks not installed")
	}
	raw, _ := util.ReadFileSafe(cursorHooksFile())
	for _, event := range []string{"sessionStart", "workspaceOpen"} {
		cfg := util.TryParseJsonc(raw)
		hooksValue, _ := cfg.Get("hooks")
		hooks := hooksValue.(*util.OrderedMap)
		hooks.Delete(event)
		if err := util.WriteFile(cursorHooksFile(), util.StringifyJSON(cfg)); err != nil {
			t.Fatal(err)
		}
		if HasCursorCodegraphIndexHook() {
			t.Fatalf("deleted %s hook passed verification", event)
		}
		if err := util.WriteFile(cursorHooksFile(), raw); err != nil {
			t.Fatal(err)
		}
	}
	cfg := util.TryParseJsonc(raw)
	hooksValue, _ := cfg.Get("hooks")
	hooks := hooksValue.(*util.OrderedMap)
	hooks.Set("workspaceOpen", []any{func() any { e := util.NewOrderedMap(); e.Set("command", "foreign"); return e }()})
	if err := util.WriteFile(cursorHooksFile(), util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}
	if HasCursorCodegraphIndexHook() {
		t.Fatal("corrupted workspaceOpen hook passed verification")
	}
}

func TestCursorRtkHookUpstreamContractIsIsolatedAndIdempotent(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	if util.Exists(cursorHooksFile()) {
		t.Fatal("fresh home unexpectedly has Cursor hooks")
	}
	foreign := `{
  "custom": "keep",
  "hooks": {
    "preToolUse": [{"command": "user-guard", "matcher": "Shell"}],
    "workspaceOpen": [{"command": "user-open"}]
  }
}`
	if util.TryParseJsonc(foreign) == nil {
		t.Fatal("foreign setup JSON failed to parse")
	}
	parsed := util.TryParseJsonc(foreign)
	if _, ok := parsed.Get("hooks"); !ok {
		t.Fatal("foreign hooks missing after parse")
	}
	if err := util.WriteFile(cursorHooksFile(), foreign); err != nil {
		t.Fatal(err)
	}
	InstallCursorRtkHook()
	InstallCursorRtkHook()
	raw, _ := util.ReadFileSafe(cursorHooksFile())
	assertCursorRtkHookContract(t, raw)
	if !HasCursorRtkHook() || !strings.Contains(raw, "rtk hook cursor") || !strings.Contains(raw, "custom") || !strings.Contains(raw, "user-guard") || !strings.Contains(raw, "user-open") {
		t.Fatalf("unexpected Cursor RTK hooks: %q", raw)
	}
	RemoveCursorRtkHook()
	raw, _ = util.ReadFileSafe(cursorHooksFile())
	if strings.Contains(raw, "rtk hook cursor") || !strings.Contains(raw, "user-guard") || !strings.Contains(raw, "user-open") || !strings.Contains(raw, "custom") {
		t.Fatalf("unwire changed foreign Cursor config: %s", raw)
	}
}

func assertCursorRtkHookContract(t *testing.T, raw string) {
	t.Helper()
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("hooks.json failed to parse: %q", raw)
	}
	hooksValue, ok := cfg.Get("hooks")
	hooks, ok := hooksValue.(*util.OrderedMap)
	if !ok {
		t.Fatalf("hooks is not an object: %#v", hooksValue)
	}
	entriesValue, ok := hooks.Get("preToolUse")
	entries, ok := entriesValue.([]any)
	if !ok {
		t.Fatalf("preToolUse is not an array: %#v", entriesValue)
	}
	count := 0
	for _, value := range entries {
		entry, ok := value.(*util.OrderedMap)
		if !ok {
			continue
		}
		command, commandOK := entry.Get("command")
		matcher, matcherOK := entry.Get("matcher")
		if commandOK && matcherOK && command == "rtk hook cursor" && matcher == "Shell" {
			if entry.Len() != 2 {
				t.Fatalf("RTK hook has %d keys, want 2: %#v", entry.Len(), entry.Keys())
			}
			count++
		}
	}
	if count != 1 {
		t.Fatalf("found %d exact RTK hook entries, want exactly 1", count)
	}
}

func TestCursorRtkHookFreshHomeAndEmptyCleanup(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	InstallCursorRtkHook()
	if !HasCursorRtkHook() || !strings.Contains(util.CursorPathsResolved().Dir, ".cursor") {
		t.Fatal("RTK Cursor hook not installed in fresh home")
	}
	RemoveCursorRtkHook()
	raw, ok := util.ReadFileSafe(cursorHooksFile())
	if !ok || !strings.Contains(raw, `"version"`) || strings.Contains(raw, "rtk hook cursor") {
		t.Fatalf("unexpected remaining Cursor config: %s", raw)
	}
}

func TestCursorRtkHookRefusesUnsafeConfigs(t *testing.T) {
	withoutWSL(t)
	cases := []string{
		"{\n  // keep comment\n  \"version\": 1\n}",
		`{"version":`,
		`{"version":2}`,
		`{"version":1,"hooks":[]}`,
		`{"version":1,"hooks":{"preToolUse":{}}}`,
	}
	for _, original := range cases {
		t.Run(strings.ReplaceAll(original, "\n", " "), func(t *testing.T) {
			util.SetHomeOverride(t.TempDir())
			t.Cleanup(func() { util.SetHomeOverride("") })
			if err := util.WriteFile(cursorHooksFile(), original); err != nil {
				t.Fatal(err)
			}
			InstallCursorRtkHook()
			raw, _ := util.ReadFileSafe(cursorHooksFile())
			if raw != original {
				t.Fatalf("unsafe config changed on install: %q", raw)
			}
			RemoveCursorRtkHook()
			raw, _ = util.ReadFileSafe(cursorHooksFile())
			if raw != original {
				t.Fatalf("unsafe config changed on remove: %q", raw)
			}
		})
	}
}

func TestCursorRtkHookInstallDoesNotRewriteUnchangedConfig(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	original := `{"version":1,"hooks":{"preToolUse":[{"command":"rtk hook cursor","matcher":"Shell"}],"workspaceOpen":[{"command":"user-open"}]},"custom":{"keep":true}}`
	if err := util.WriteFile(cursorHooksFile(), original); err != nil {
		t.Fatal(err)
	}
	InstallCursorRtkHook()
	raw, _ := util.ReadFileSafe(cursorHooksFile())
	if raw != original {
		t.Fatalf("unchanged config rewritten: %q", raw)
	}
}

func TestCursorRtkHookInstallRemovesToklessStaleAndDuplicateEntries(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	original := `{"version":1,"hooks":{"preToolUse":[{"command":"/old/tokless rtk hook cursor","matcher":"Shell"},{"command":"rtk hook cursor","matcher":"Shell"},{"command":"rtk hook cursor","matcher":"Shell"},{"command":"foreign","matcher":"Shell"},{"command":"/old/tokless rtk hook cursor","matcher":"Shell","extra":true}]}}`
	if err := util.WriteFile(cursorHooksFile(), original); err != nil {
		t.Fatal(err)
	}
	if !InstallCursorRtkHook() {
		t.Fatal("failed to install Cursor RTK hook")
	}
	raw, _ := util.ReadFileSafe(cursorHooksFile())
	cfg := util.TryParseJsonc(raw)
	hooks, _ := cfg.Get("hooks")
	hookMap := hooks.(*util.OrderedMap)
	entries, _ := hookMap.Get("preToolUse")
	values := entries.([]any)
	count := 0
	for _, entry := range values {
		if cursorRtkHookEntryMatches(entry) {
			count++
		}
	}
	if count != 1 || len(values) != 3 || !strings.Contains(raw, "foreign") || !strings.Contains(raw, "extra") {
		t.Fatalf("stale/duplicate cleanup changed wrong entries: %s", raw)
	}
}

func TestCursorMcpMalformedRefusalAndForeignPreservation(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.CursorGlobalMcpPath()
	if err := util.WriteFile(p, `{invalid`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("codegraph"); changed {
		t.Fatal("malformed config overwritten")
	}
	raw, _ := util.ReadFileSafe(p)
	if raw != `{invalid` {
		t.Fatalf("config changed: %q", raw)
	}
	if err := util.WriteFile(p, `{"mcpServers":{"foreign":{"command":"keep","args":[]}}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("codegraph"); !changed {
		t.Fatal("MCP entry not added")
	}
	raw, _ = util.ReadFileSafe(p)
	if !strings.Contains(raw, `"foreign"`) || !CursorMcpHas("codegraph") {
		t.Fatalf("foreign or managed entry missing: %s", raw)
	}
}

func TestCursorCodegraphMcpCarriesWorkspaceFolder(t *testing.T) {
	withoutWSL(t)
	entry := cursorMcpEntry("codegraph")
	argsValue, ok := entry.Get("args")
	if !ok {
		t.Fatal("CodeGraph args missing")
	}
	args := argsValue.([]any)
	if !containsString(args, "--workspace") || !containsString(args, "${workspaceFolder}") {
		t.Fatalf("native CodeGraph args = %#v", args)
	}
}

func TestCursorCodegraphMcpWSLBridgeDoesNotUseWorkspaceFolder(t *testing.T) {
	root := t.TempDir()
	windowsHome := filepath.Join(root, "windows-home")
	bin := filepath.Join(root, "bin")
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("PATH", bin)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"cmd.exe": "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'",
		"wslpath": "printf '%s\\n' '" + windowsHome + "'",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor", "Cursor.exe"), []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}

	entry := cursorMcpEntryFor("codegraph", true)
	argsValue, _ := entry.Get("args")
	args := argsValue.([]any)
	if len(args) < 3 || args[0] != "-d" || args[1] != "Ubuntu" || args[2] != "--" {
		t.Fatalf("WSL CodeGraph args = %#v", args)
	}
	if containsString(args, "--workspace") || containsString(args, "${workspaceFolder}") {
		t.Fatalf("WSL CodeGraph bridge forwards workspace variable: %#v", args)
	}
}

func containsString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestRemoveCursorMcpRejectsJSONCCommentsWithoutChangingConfig(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.CursorGlobalMcpPath()
	if err := util.WriteFile(p, `{"mcpServers":{}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("codegraph"); !changed {
		t.Fatal("MCP entry not added")
	}
	raw, _ := util.ReadFileSafe(p)
	original := "// keep this comment\n" + raw
	if err := util.WriteFile(p, original); err != nil {
		t.Fatal(err)
	}

	if RemoveCursorMcp("codegraph") {
		t.Fatal("removed MCP entry from commented JSONC config")
	}
	unchanged, _ := util.ReadFileSafe(p)
	if unchanged != original {
		t.Fatalf("commented config changed: %q", unchanged)
	}
}

func TestCursorMcpPreservesEntriesWithExtraFields(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.CursorGlobalMcpPath()
	entry := cursorMcpEntry("codegraph")
	entry.Set("env", map[string]any{"KEEP": "me"})
	cfg := util.NewOrderedMap()
	servers := util.NewOrderedMap()
	servers.Set("codegraph", entry)
	cfg.Set("mcpServers", servers)
	original := util.StringifyJSON(cfg)
	if err := util.WriteFile(p, original); err != nil {
		t.Fatal(err)
	}

	if changed, _ := ConfigureCursorMcp("codegraph"); changed {
		t.Fatal("configured entry with foreign extra field")
	}
	if RemoveCursorMcp("codegraph") {
		t.Fatal("removed entry with foreign extra field")
	}
	raw, _ := util.ReadFileSafe(p)
	if raw != original {
		t.Fatalf("entry with extra field changed: %q", raw)
	}
}

func TestConfigureCursorMcpMigratesStaleToklessPaths(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	for _, toolID := range []string{"codegraph", "context-mode"} {
		t.Run(toolID, func(t *testing.T) {
			p := util.CursorGlobalMcpPath()
			entry := cursorMcpEntry(toolID)
			entry.Set("command", "/tmp/opencode/tokless-linux")
			cfg := util.NewOrderedMap()
			servers := util.NewOrderedMap()
			servers.Set(toolID, entry)
			cfg.Set("mcpServers", servers)
			if err := util.WriteFile(p, util.StringifyJSON(cfg)); err != nil {
				t.Fatal(err)
			}
			if changed, _ := ConfigureCursorMcp(toolID); !changed {
				t.Fatal("stale direct tokless entry not updated")
			}
			if !CursorMcpHas(toolID) {
				t.Fatal("updated direct tokless entry does not match")
			}
		})
	}
}

func TestConfigureCursorMcpMigratesStaleWSLToklessPaths(t *testing.T) {
	root := t.TempDir()
	util.SetHomeOverride(filepath.Join(root, "linux-home"))
	t.Cleanup(func() { util.SetHomeOverride("") })
	home := filepath.Join(root, "mnt", "c", "Users", "CursorUser")
	bin := filepath.Join(root, "bin")
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("PATH", bin)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"cmd.exe": "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'",
		"wslpath": "printf '%s\\n' '" + home + "'",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cursorExe := filepath.Join(home, "AppData", "Local", "Programs", "cursor", "Cursor.exe")
	if err := os.MkdirAll(filepath.Dir(cursorExe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorExe, []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, toolID := range []string{"codegraph", "context-mode"} {
		t.Run(toolID, func(t *testing.T) {
			p := filepath.Join(home, ".cursor", "mcp.json")
			entry := cursorMcpEntryFor(toolID, true)
			args, _ := entry.Get("args")
			argList := args.([]any)
			argList[toklessArgIndex(argList)] = "/tmp/opencode/tokless-linux"
			entry.Set("args", argList)
			cfg := util.NewOrderedMap()
			servers := util.NewOrderedMap()
			servers.Set(toolID, entry)
			cfg.Set("mcpServers", servers)
			if err := util.WriteFile(p, util.StringifyJSON(cfg)); err != nil {
				t.Fatal(err)
			}
			if changed, _ := ConfigureCursorMcp(toolID); !changed {
				t.Fatal("stale WSL tokless entry not updated")
			}
			if !CursorMcpHas(toolID) {
				t.Fatal("updated WSL tokless entry does not match")
			}
		})
	}
}

func TestCursorMcpRefusesMalformedManagedShapeWithoutChangingConfig(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.CursorGlobalMcpPath()
	original := `{"mcpServers":{"codegraph":{"type":{"foreign":true},"command":"codegraph","args":["serve","--mcp"]}}}`
	if err := util.WriteFile(p, original); err != nil {
		t.Fatal(err)
	}

	if changed, _ := ConfigureCursorMcp("codegraph"); changed {
		t.Fatal("malformed CodeGraph entry changed")
	}
	if RemoveCursorMcp("codegraph") {
		t.Fatal("malformed CodeGraph entry removed")
	}
	raw, _ := util.ReadFileSafe(p)
	if raw != original {
		t.Fatalf("malformed entry changed: %s", raw)
	}
}

func TestCursorMcpMigratesLegacyCodegraphEntry(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	for _, tc := range []struct {
		command string
		args    string
	}{
		{"codegraph", `["serve","--mcp"]`},
		{"codegraph.exe", `["serve","--mcp"]`},
		{"codegraph.cmd", `["serve","--mcp"]`},
		{"codegraph.bat", `["serve","--mcp"]`},
		{`C:\\Users\\ADMIN\\AppData\\Local\\Programs\\Cursor\\resources\\app\\bin\\codegraph.exe`, `["serve","--mcp"]`},
		{"codegraph", `["serve","--mcp","--path","${workspaceFolder}"]`},
	} {
		p := util.CursorGlobalMcpPath()
		if err := util.WriteFile(p, `{"mcpServers":{"codegraph":{"type":"stdio","command":"`+tc.command+`","args":`+tc.args+`},"foreign":{"command":"keep"}}}`); err != nil {
			t.Fatal(err)
		}
		if changed, _ := ConfigureCursorMcp("codegraph"); !changed {
			t.Fatalf("legacy entry not migrated: %s", tc.command)
		}
		raw, _ := util.ReadFileSafe(p)
		if !strings.Contains(raw, `"run-mcp"`) || !strings.Contains(raw, `"--agent"`) || !strings.Contains(raw, `"cursor"`) || !strings.Contains(raw, `"foreign"`) || !CursorMcpHas("codegraph") {
			t.Fatalf("unexpected migrated config for %s: %s", tc.command, raw)
		}
		if !RemoveCursorMcp("codegraph") || CursorMcpHas("codegraph") {
			t.Fatal("migrated entry failed managed remove verification")
		}
	}
}

func TestCursorMcpRejectsLookalikeLegacyCodegraphCommands(t *testing.T) {
	withoutWSL(t)
	for _, command := range []string{"my-codegraph", "codegraph-wrapper", "/tmp/codegraph-wrapper"} {
		t.Run(command, func(t *testing.T) {
			util.SetHomeOverride(t.TempDir())
			t.Cleanup(func() { util.SetHomeOverride("") })
			p := util.CursorGlobalMcpPath()
			original := `{"mcpServers":{"codegraph":{"type":"stdio","command":"` + command + `","args":["serve","--mcp"]}}}`
			if err := util.WriteFile(p, original); err != nil {
				t.Fatal(err)
			}
			if changed, _ := ConfigureCursorMcp("codegraph"); changed {
				t.Fatal("lookalike legacy command migrated")
			}
			raw, _ := util.ReadFileSafe(p)
			if raw != original {
				t.Fatalf("lookalike command changed: %s", raw)
			}
		})
	}
}

func TestCursorMcpMigratesLegacyNpxCodegraphFallback(t *testing.T) {
	skipOnWindows(t)
	withoutWSL(t)
	for _, command := range []string{"npx", "npx.exe", "npx.cmd", "npx.bat"} {
		t.Run(command, func(t *testing.T) {
			util.SetHomeOverride(t.TempDir())
			t.Cleanup(func() { util.SetHomeOverride("") })
			p := util.CursorGlobalMcpPath()
			original := `{"mcpServers":{"codegraph":{"type":"stdio","command":"tokless","args":["run-mcp","--agent","cursor","--workspace","${workspaceFolder}","` + command + `","--no-install","@colbymchenry/codegraph","serve","--mcp"]}}}`
			if err := util.WriteFile(p, original); err != nil {
				t.Fatal(err)
			}
			if changed, _ := ConfigureCursorMcp("codegraph"); !changed {
				t.Fatal("legacy npx CodeGraph entry not migrated")
			}
			if !CursorMcpHas("codegraph") {
				t.Fatal("migrated npx CodeGraph entry not recognized as managed")
			}
		})
	}
}

func TestCursorDetectionWindowsKnownCLIDirs(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	local := filepath.Join(root, "local")
	util.SetHomeOverride(filepath.Join(root, "home"))
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TOKLESS_TEST", "0")
	oldGOOS, oldRoot, oldIsWin := goosForDetect, cursorDetectRoot, util.IsWin
	goosForDetect, cursorDetectRoot = "windows", ""
	util.IsWin = true
	t.Cleanup(func() {
		goosForDetect, cursorDetectRoot, util.IsWin = oldGOOS, oldRoot, oldIsWin
	})
	binDir := filepath.Join(local, "Programs", "Cursor", "resources", "app", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "cursor-agent.exe")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'Cursor Agent 1.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cursor.Detect(); !got.Installed || got.Source != "cli" {
		t.Fatalf("Cursor CLI not detected in Windows install dir: %+v", got)
	}
}

func TestCursorMcpRefusesLegacyCodegraphEntryWithExtraFields(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.CursorGlobalMcpPath()
	original := `{"mcpServers":{"codegraph":{"type":"stdio","command":"codegraph","args":["serve","--mcp"],"timeout":30}}}`
	if err := util.WriteFile(p, original); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("codegraph"); changed {
		t.Fatal("legacy CodeGraph entry with extra field migrated")
	}
	raw, _ := util.ReadFileSafe(p)
	if raw != original {
		t.Fatalf("legacy entry with extra field changed: %s", raw)
	}
	if CursorMcpHas("codegraph") || RemoveCursorMcp("codegraph") {
		t.Fatal("legacy entry with extra field reported as managed")
	}
}

func TestCursorMcpRejectsForeignCodegraphEntry(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.CursorGlobalMcpPath()
	original := `{"mcpServers":{"codegraph":{"type":"stdio","command":"codegraph","args":["serve","--mcp","--config","foreign.json"]}}}`
	if err := util.WriteFile(p, original); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("codegraph"); changed {
		t.Fatal("foreign CodeGraph entry migrated")
	}
	raw, _ := util.ReadFileSafe(p)
	if raw != original {
		t.Fatalf("foreign entry changed: %s", raw)
	}
}

func withoutWSL(t *testing.T) {
	t.Helper()
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")
}

func TestCursorMcpUsesWindowsCursorConfigAndWSLBridge(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	linuxHome := filepath.Join(root, "linux-home")
	windowsHome := filepath.Join(root, "windows-home")
	util.SetHomeOverride(linuxHome)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("TOKLESS_CURSOR_WINDOWS_BRIDGE", "1")
	t.Setenv("PATH", filepath.Join(root, "bin"))
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable("cmd.exe", "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'")
	writeExecutable("wslpath", "printf '%s\\n' '"+windowsHome+"'")
	if err := os.MkdirAll(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor", "Cursor.exe"), []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("context-mode"); !changed {
		t.Fatal("MCP entry not added")
	}
	p := filepath.Join(windowsHome, ".cursor", "mcp.json")
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		t.Fatalf("Windows Cursor config not written: %s", p)
	}
	cfg := util.TryParseJsonc(raw)
	serversValue, _ := cfg.Get("mcpServers")
	servers, _ := serversValue.(*util.OrderedMap)
	entryValue, _ := servers.Get("context-mode")
	entry, _ := entryValue.(*util.OrderedMap)
	command, _ := entry.Get("command")
	argsValue, _ := entry.Get("args")
	args, _ := argsValue.([]any)
	if command != "wsl.exe" || len(args) < 3 || args[0] != "-d" || args[1] != "Ubuntu" || args[2] != "--" {
		t.Fatalf("unexpected WSL bridge: command=%#v args=%#v", command, args)
	}
	native, exists := util.ReadFileSafe(filepath.Join(linuxHome, ".cursor", "mcp.json"))
	if !exists || strings.Contains(native, `"command": "wsl.exe"`) {
		t.Fatalf("native Linux Cursor config missing or bridged: %s", native)
	}
}

func TestCursorMcpHasValidatesNativeAndWindowsBridgeEntries(t *testing.T) {
	skipOnWindows(t)
	_, linuxHome, windowsHome := setupCursorMcpBridgeTest(t, true)
	if changed, _ := ConfigureCursorMcp("context-mode"); !changed {
		t.Fatal("MCP entry not added")
	}

	for _, target := range []struct {
		path   string
		bridge bool
	}{
		{filepath.Join(linuxHome, ".cursor", "mcp.json"), false},
		{filepath.Join(windowsHome, ".cursor", "mcp.json"), true},
	} {
		raw, ok := util.ReadFileSafe(target.path)
		if !ok {
			t.Fatalf("MCP config missing: %s", target.path)
		}
		cfg := util.TryParseJsonc(raw)
		servers, ok := cursorMcpMapRead(cfg)
		if !ok {
			t.Fatalf("mcpServers missing: %s", target.path)
		}
		entry, ok := servers.Get("context-mode")
		if !ok || !cursorEntryMatches(entry, cursorMcpEntryFor("context-mode", target.bridge)) {
			t.Fatalf("entry does not match generated target entry: %s", target.path)
		}
	}
	if !CursorMcpHas("context-mode") {
		t.Fatal("native and bridge MCP entries not recognized")
	}
}

func TestCursorMcpHasRejectsWindowsBridgeMismatch(t *testing.T) {
	skipOnWindows(t)
	_, _, windowsHome := setupCursorMcpBridgeTest(t, true)
	if changed, _ := ConfigureCursorMcp("context-mode"); !changed {
		t.Fatal("MCP entry not added")
	}
	p := filepath.Join(windowsHome, ".cursor", "mcp.json")
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		t.Fatal("Windows Cursor config missing")
	}
	if err := util.WriteFile(p, strings.Replace(raw, `"command": "wsl.exe"`, `"command": "wrong.exe"`, 1)); err != nil {
		t.Fatal(err)
	}
	if CursorMcpHas("context-mode") {
		t.Fatal("bridge mismatch reported as valid")
	}
}

func TestCursorMcpHasIgnoresWindowsConfigWhenBridgeDisabled(t *testing.T) {
	skipOnWindows(t)
	_, linuxHome, windowsHome := setupCursorMcpBridgeTest(t, false)
	if changed, _ := ConfigureCursorMcp("context-mode"); !changed {
		t.Fatal("native MCP entry not added")
	}
	if err := util.WriteFile(filepath.Join(windowsHome, ".cursor", "mcp.json"), `{"mcpServers":{"context-mode":{"type":"stdio","command":"wrong.exe","args":[]}}}`); err != nil {
		t.Fatal(err)
	}
	if !CursorMcpHas("context-mode") {
		t.Fatal("native MCP entry not recognized with bridge disabled")
	}
	if _, ok := util.ReadFileSafe(filepath.Join(linuxHome, ".cursor", "mcp.json")); !ok {
		t.Fatal("native Cursor config missing")
	}
}

func setupCursorMcpBridgeTest(t *testing.T, bridge bool) (root, linuxHome, windowsHome string) {
	t.Helper()
	root = t.TempDir()
	linuxHome = filepath.Join(root, "linux-home")
	windowsHome = filepath.Join(root, "windows-home")
	util.SetHomeOverride(linuxHome)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("TOKLESS_CURSOR_WINDOWS_BRIDGE", "")
	if bridge {
		t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
		t.Setenv("WSL_INTEROP", "1")
		t.Setenv("TOKLESS_CURSOR_WINDOWS_BRIDGE", "1")
		t.Setenv("PATH", filepath.Join(root, "bin"))
		if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "bin", "cmd.exe"), []byte("#!/bin/sh\nprintf '%s\\n' 'C:\\\\Users\\\\CursorUser'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "bin", "wslpath"), []byte("#!/bin/sh\nprintf '%s\\n' '"+windowsHome+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor", "Cursor.exe"), []byte("cursor"), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		withoutWSL(t)
	}
	return root, linuxHome, windowsHome
}

func TestCursorMcpMigratesLegacyCodegraphToWSLBridge(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	linuxHome := filepath.Join(root, "linux-home")
	windowsHome := filepath.Join(root, "windows-home")
	util.SetHomeOverride(linuxHome)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("TOKLESS_CURSOR_WINDOWS_BRIDGE", "1")
	t.Setenv("PATH", filepath.Join(root, "bin"))
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"cmd.exe": "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'",
		"wslpath": "printf '%s\\n' '" + windowsHome + "'",
	} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor", "Cursor.exe"), []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(windowsHome, ".cursor", "mcp.json")
	if err := util.WriteFile(p, `{"mcpServers":{"codegraph":{"type":"stdio","command":"codegraph","args":["serve","--mcp"]},"foreign":{"command":"keep"}}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("codegraph"); !changed {
		t.Fatal("legacy CodeGraph entry not migrated")
	}
	raw, _ := util.ReadFileSafe(p)
	if !strings.Contains(raw, `"command": "wsl.exe"`) || !strings.Contains(raw, `"run-mcp"`) || !strings.Contains(raw, `"foreign"`) {
		t.Fatalf("unexpected migrated WSL config: %s", raw)
	}
}

func TestCursorMcpStaysNativeInWSLWithoutWindowsCursor(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	linuxHome := filepath.Join(root, "linux-home")
	windowsHome := filepath.Join(root, "windows-home")
	util.SetHomeOverride(linuxHome)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("PATH", filepath.Join(root, "bin"))
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "cmd.exe"), []byte("#!/bin/sh\nprintf '%s\\n' 'C:\\\\Users\\\\CursorUser'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "wslpath"), []byte("#!/bin/sh\nprintf '%s\\n' '"+windowsHome+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("context-mode"); !changed {
		t.Fatal("MCP entry not added")
	}
	raw, ok := util.ReadFileSafe(filepath.Join(linuxHome, ".cursor", "mcp.json"))
	if !ok {
		t.Fatal("native Linux Cursor config not written")
	}
	if strings.Contains(raw, `"command": "wsl.exe"`) {
		t.Fatalf("unexpected WSL bridge without Windows Cursor: %s", raw)
	}
	if _, exists := util.ReadFileSafe(filepath.Join(windowsHome, ".cursor", "mcp.json")); exists {
		t.Fatal("Windows Cursor config written without Windows Cursor")
	}
}

func TestCursorMcpStaysNativeInWSLWithWindowsCursorByDefault(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	linuxHome := filepath.Join(root, "linux-home")
	windowsHome := filepath.Join(root, "windows-home")
	util.SetHomeOverride(linuxHome)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("PATH", filepath.Join(root, "bin"))
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"cmd.exe": "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'",
		"wslpath": "printf '%s\\n' '" + windowsHome + "'",
	} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe := filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor", "Cursor.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCursorMcp("context-mode"); !changed {
		t.Fatal("native MCP entry not added")
	}
	if _, exists := util.ReadFileSafe(filepath.Join(windowsHome, ".cursor", "mcp.json")); exists {
		t.Fatal("Windows Cursor config written without bridge opt-in")
	}
}

func TestCursorRtkUsesWindowsCursorConfigAndWSLBridge(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	linuxHome := filepath.Join(root, "linux-home")
	windowsHome := filepath.Join(root, "windows-home")
	util.SetHomeOverride(linuxHome)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("TOKLESS_CURSOR_WINDOWS_BRIDGE", "1")
	t.Setenv("PATH", filepath.Join(root, "bin"))
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable("cmd.exe", "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'")
	writeExecutable("wslpath", "printf '%s\\n' '"+windowsHome+"'")
	if err := os.MkdirAll(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor", "Cursor.exe"), []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(windowsHome, ".cursor", "hooks.json"), `{"custom":"keep","hooks":{"preToolUse":[{"command":"wsl.exe -d Ubuntu -- /old/tokless rtk hook cursor","matcher":"Shell"},{"command":"wsl.exe -d Ubuntu -- /old/tokless rtk hook cursor","matcher":"Shell"},{"command":"foreign","matcher":"Shell"}]}}`); err != nil {
		t.Fatal(err)
	}

	if !InstallCursorRtkHook() {
		t.Fatal("failed to install WSL Cursor RTK hook")
	}
	hooks, ok := util.ReadFileSafe(filepath.Join(windowsHome, ".cursor", "hooks.json"))
	if !ok || !strings.Contains(hooks, `"command": "wsl.exe -d \"Ubuntu\" --`) || !strings.Contains(hooks, "rtk hook cursor") {
		t.Fatalf("unexpected WSL Cursor hook: %s", hooks)
	}
	if strings.Contains(hooks, `"command": "rtk hook cursor"`) || !strings.Contains(hooks, `"command": "foreign"`) || !strings.Contains(hooks, `"custom": "keep"`) {
		t.Fatalf("legacy or foreign WSL Cursor hook handling incorrect: %s", hooks)
	}
	cfg := util.TryParseJsonc(hooks)
	hookMap, _ := cfg.Get("hooks")
	preToolUse, _ := hookMap.(*util.OrderedMap).Get("preToolUse")
	if len(preToolUse.([]any)) != 2 || !HasCursorRtkHook() {
		t.Fatalf("WSL stale/duplicate cleanup failed: %s", hooks)
	}
	if !HasCursorRtkHook() {
		t.Fatal("WSL Cursor RTK hook not detected")
	}
	if !ConfigureCursorRtkPermissions() {
		t.Fatal("failed to configure WSL Cursor RTK permissions")
	}
	if !HasCursorRtkPermissions() {
		t.Fatal("WSL Cursor RTK permissions not detected")
	}
	nativeHooks, exists := util.ReadFileSafe(filepath.Join(linuxHome, ".cursor", "hooks.json"))
	if !exists || strings.Contains(nativeHooks, `wsl.exe`) {
		t.Fatalf("native Linux Cursor hook missing or bridged: %s", nativeHooks)
	}
}

func TestCursorHookCommandsQuotePathsAndPreserveWSLArgs(t *testing.T) {
	oldWin := util.IsWin
	defer func() { util.IsWin = oldWin }()
	util.IsWin = false
	withoutWSL(t)
	path := cursorCodegraphHookCommand()
	if !strings.HasPrefix(path, "'") || !strings.Contains(path, "cursor-hook codegraph-index") {
		t.Fatalf("POSIX hook path not quoted: %q", path)
	}
	if !cursorOwnedCodegraphHookEntry(func() any { e := util.NewOrderedMap(); e.Set("command", path); return e }()) {
		t.Fatal("exact CodeGraph entry not recognized")
	}
	e := util.NewOrderedMap()
	e.Set("command", path)
	e.Set("extra", true)
	if cursorOwnedCodegraphHookEntry(e) {
		t.Fatal("CodeGraph entry with extra field treated as owned")
	}
}

func TestRemoveCursorCodegraphHookNoOpIsByteStable(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	original := "{\n  \"custom\": true,\n  \"hooks\": {\n    \"workspaceOpen\": [{\"command\": \"foreign\", \"extra\": 1}]\n  }\n}\n"
	if err := util.WriteFile(cursorHooksFile(), original); err != nil {
		t.Fatal(err)
	}
	if !RemoveCursorCodegraphIndexHook() {
		t.Fatal("no-op removal failed")
	}
	raw, _ := util.ReadFileSafe(cursorHooksFile())
	if raw != original {
		t.Fatalf("no-op removal rewrote config: %q", raw)
	}
}

func TestCursorMcpPermissionsCreatePreserveAndIdempotent(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := cursorPermissionsFile()
	original := `{"version":1,"terminalAllowlist":["user-tool"],"mcpAllowlist":["foreign:*"]}`
	if err := util.WriteFile(p, original); err != nil {
		t.Fatal(err)
	}
	if !ConfigureCursorMcpPermissions("codegraph") || !HasCursorMcpPermissions("codegraph") {
		t.Fatal("CodeGraph MCP permission not created")
	}
	first, _ := util.ReadFileSafe(p)
	if !strings.Contains(first, `"foreign:*"`) || !strings.Contains(first, `"codegraph:*"`) || !strings.Contains(first, `"user-tool"`) {
		t.Fatalf("existing permissions not preserved: %s", first)
	}
	if !ConfigureCursorMcpPermissions("codegraph") {
		t.Fatal("idempotent configure failed")
	}
	second, _ := util.ReadFileSafe(p)
	if first != second {
		t.Fatalf("idempotent configure rewrote config: %q != %q", first, second)
	}
}

func TestCursorMcpPermissionsRemoveOnlyToklessEntries(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := cursorPermissionsFile()
	if err := util.WriteFile(p, `{"version":1,"mcpAllowlist":["foreign:*","codegraph:*"]}`); err != nil {
		t.Fatal(err)
	}
	if !RemoveCursorMcpPermissions("codegraph") || HasCursorMcpPermissions("codegraph") {
		t.Fatal("CodeGraph MCP permission not removed")
	}
	raw, _ := util.ReadFileSafe(p)
	if !strings.Contains(raw, `"foreign:*"`) || strings.Contains(raw, `"codegraph:*"`) {
		t.Fatalf("foreign permission changed: %s", raw)
	}
}

func TestCursorMcpPermissionsRefuseMalformedAndCommentedConfigs(t *testing.T) {
	withoutWSL(t)
	for _, original := range []string{`{"mcpAllowlist":[`} {
		t.Run(strings.ReplaceAll(original, "\n", " "), func(t *testing.T) {
			util.SetHomeOverride(t.TempDir())
			t.Cleanup(func() { util.SetHomeOverride("") })
			p := cursorPermissionsFile()
			if err := util.WriteFile(p, original); err != nil {
				t.Fatal(err)
			}
			if ConfigureCursorMcpPermissions("context-mode") {
				t.Fatal("unsafe permissions config changed on configure")
			}
			raw, _ := util.ReadFileSafe(p)
			if raw != original {
				t.Fatalf("unsafe config changed: %q", raw)
			}
		})
	}
}

func TestCursorRtkPermissionsIDEJSONCAndCLIStrict(t *testing.T) {
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.WriteFile(cursorPermissionsFile(), "// IDE config\n{\"version\":1,\n\"mcpAllowlist\":[\"foreign:*\"],\n}\n"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(cursorCLIConfigFile(), `{"permissions":{"allow":["Shell(foreign)"]}}`); err != nil {
		t.Fatal(err)
	}
	if !ConfigureCursorRtkPermissions() || !HasCursorRtkPermissions() {
		t.Fatal("RTK permissions not configured")
	}
	ide, _ := util.ReadFileSafe(cursorPermissionsFile())
	cli, _ := util.ReadFileSafe(cursorCLIConfigFile())
	ideCfg := util.TryParseJsonc(ide)
	if ideCfg == nil || !strings.Contains(ide, "foreign:*") || !strings.Contains(ide, `"rtk"`) {
		t.Fatalf("IDE permissions not preserved: %s", ide)
	}
	if !strings.Contains(cli, `"Shell(foreign)"`) || !strings.Contains(cli, `"Shell(rtk)"`) || strings.Contains(ide, "Shell(rtk)") || strings.Contains(cli, "terminalAllowlist") {
		t.Fatalf("permission surfaces crossed: IDE=%s CLI=%s", ide, cli)
	}

	original := "// CLI comments are not allowed\n{\"permissions\":{\"allow\":[]}}"
	if err := util.WriteFile(cursorCLIConfigFile(), original); err != nil {
		t.Fatal(err)
	}
	if ConfigureCursorRtkPermissions() {
		t.Fatal("commented CLI config accepted")
	}
	got, _ := util.ReadFileSafe(cursorCLIConfigFile())
	if got != original {
		t.Fatalf("commented CLI config mutated: %q", got)
	}
}

func TestCursorCLIConfigUsesLinuxOverrides(t *testing.T) {
	skipUnlessLinux(t)
	withoutWSL(t)
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })

	for _, tc := range []struct {
		name, cursorDir, xdg, want string
	}{
		{name: "xdg", xdg: t.TempDir(), want: "cursor"},
		{name: "cursor config dir", cursorDir: t.TempDir(), want: "direct"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CURSOR_CONFIG_DIR", tc.cursorDir)
			t.Setenv("XDG_CONFIG_HOME", tc.xdg)
			wantDir := tc.xdg
			if tc.cursorDir != "" {
				wantDir = tc.cursorDir
			} else {
				wantDir = filepath.Join(wantDir, tc.want)
			}
			want := filepath.Join(wantDir, "cli-config.json")
			if got := cursorCLIConfigFile(); got != want {
				t.Fatalf("cursorCLIConfigFile() = %q, want %q", got, want)
			}
			if !ConfigureCursorRtkPermissions() {
				t.Fatal("ConfigureCursorRtkPermissions() failed")
			}
			if _, ok := util.ReadFileSafe(want); !ok {
				t.Fatalf("CLI config not written at %q", want)
			}
			if _, ok := util.ReadFileSafe(cursorPermissionsFile()); !ok {
				t.Fatal("IDE permissions config not written at native home path")
			}
		})
	}
}

func TestCursorCLIConfigWSLWindowsPathWins(t *testing.T) {
	skipUnlessLinux(t)
	root := t.TempDir()
	linuxHome := filepath.Join(root, "linux-home")
	windowsHome := filepath.Join(root, "windows-home")
	util.SetHomeOverride(linuxHome)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(root, "override"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"cmd.exe": "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'",
		"wslpath": "printf '%s\\n' '" + windowsHome + "'",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	exe := filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor", "Cursor.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, "override", "cli-config.json")
	if got := cursorCLIConfigFile(); got != want {
		t.Fatalf("cursorCLIConfigFile() = %q, want %q", got, want)
	}
	if !ConfigureCursorRtkPermissions() {
		t.Fatal("ConfigureCursorRtkPermissions() failed")
	}
	if _, ok := util.ReadFileSafe(want); !ok {
		t.Fatalf("CLI config not written at native path %q", want)
	}
	if _, ok := util.ReadFileSafe(filepath.Join(windowsHome, ".cursor", "cli-config.json")); ok {
		t.Fatal("CLI config unexpectedly written at Windows IDE path")
	}
}

func TestRemoveCursorRtkPermissionsAbsentAndMixedSurfaces(t *testing.T) {
	withoutWSL(t)
	for _, tc := range []struct {
		name     string
		ide, cli bool
	}{
		{name: "absent", ide: false, cli: false},
		{name: "ide only", ide: true, cli: false},
		{name: "cli only", ide: false, cli: true},
		{name: "both", ide: true, cli: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			util.SetHomeOverride(t.TempDir())
			t.Cleanup(func() { util.SetHomeOverride("") })
			if tc.ide {
				if err := util.WriteFile(cursorPermissionsFile(), `{"version":1,"terminalAllowlist":["foreign","rtk"],"mcpAllowlist":["keep:*"]}`); err != nil {
					t.Fatal(err)
				}
			}
			if tc.cli {
				if err := util.WriteFile(cursorCLIConfigFile(), `{"permissions":{"allow":["Shell(foreign)","Shell(rtk)"]}}`); err != nil {
					t.Fatal(err)
				}
			}
			if !RemoveCursorRtkPermissions() || HasCursorRtkPermissions() {
				t.Fatal("RTK permission removal failed or was not idempotent")
			}
			if tc.ide {
				raw, _ := util.ReadFileSafe(cursorPermissionsFile())
				if strings.Contains(raw, `"rtk"`) || !strings.Contains(raw, "foreign") || !strings.Contains(raw, "keep:*") {
					t.Fatalf("IDE foreign entries changed: %s", raw)
				}
			}
			if tc.cli {
				raw, _ := util.ReadFileSafe(cursorCLIConfigFile())
				if strings.Contains(raw, "Shell(rtk)") || !strings.Contains(raw, "Shell(foreign)") {
					t.Fatalf("CLI foreign entries changed: %s", raw)
				}
			}
		})
	}
}

func TestCursorMcpPermissionsUsesWindowsCursorConfigAndWSLPath(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	linuxHome := filepath.Join(root, "linux-home")
	windowsHome := filepath.Join(root, "windows-home")
	util.SetHomeOverride(linuxHome)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "1")
	t.Setenv("TOKLESS_CURSOR_WINDOWS_BRIDGE", "1")
	t.Setenv("PATH", filepath.Join(root, "bin"))
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"cmd.exe": "printf '%s\\n' 'C:\\\\Users\\\\CursorUser'",
		"wslpath": "printf '%s\\n' '" + windowsHome + "'",
	} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(windowsHome, "AppData", "Local", "Programs", "cursor", "Cursor.exe"), []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ConfigureCursorMcpPermissions("codegraph") || !ConfigureCursorRtkPermissions() {
		t.Fatal("WSL Cursor MCP permission not created")
	}
	if _, ok := util.ReadFileSafe(filepath.Join(windowsHome, ".cursor", "permissions.json")); !ok {
		t.Fatal("Windows Cursor permissions config not written")
	}
	if _, ok := util.ReadFileSafe(filepath.Join(windowsHome, ".cursor", "cli-config.json")); ok {
		t.Fatal("Windows Cursor CLI permissions config unexpectedly written")
	}
	if _, ok := util.ReadFileSafe(filepath.Join(linuxHome, ".cursor", "cli-config.json")); !ok {
		t.Fatal("WSL Cursor CLI permissions config not written")
	}
	if _, ok := util.ReadFileSafe(filepath.Join(linuxHome, ".cursor", "permissions.json")); !ok {
		t.Fatal("Linux Cursor permissions config not written")
	}
	raw, _ := util.ReadFileSafe(filepath.Join(linuxHome, ".cursor", "cli-config.json"))
	if !strings.Contains(raw, `Mcp(codegraph:*)`) {
		t.Fatalf("MCP CLI permission missing from native target: %s", raw)
	}
}
