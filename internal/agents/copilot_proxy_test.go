package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func copilotTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("TOKLESS_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	tokless := filepath.Join(home, ".local", "bin", "tokless")
	if err := os.MkdirAll(filepath.Dir(tokless), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokless, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteInstallMarker("test", tokless, "test"); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestConfigureCopilotProxyUsesExecutableShim(t *testing.T) {
	home := copilotTestHome(t)
	startup := filepath.Join(home, ".zshenv")
	if err := os.WriteFile(startup, []byte("export KEEP=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, file := ConfigureCopilotProxy()
	if !changed || file != copilotShimPath() {
		t.Fatalf("changed=%v file=%s", changed, file)
	}
	if got := mustReadCopilot(t, startup); got != "export KEEP=1\n" {
		t.Fatalf("startup file changed: %q", got)
	}
	raw := mustReadCopilot(t, file)
	if !strings.Contains(raw, "tokless:copilot-shim") || !strings.Contains(raw, "__copilot") {
		t.Fatalf("invalid shim: %q", raw)
	}
	if mode := fileModeCopilot(t, file); mode&0o111 == 0 {
		t.Fatalf("shim is not executable: %o", mode)
	}
}

func TestRestoreCopilotProxyFilesRemovesNewManagedFiles(t *testing.T) {
	copilotTestHome(t)
	snapshots, err := snapshotCopilotProxyFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		copilotShimPath():       copilotShimText(),
		copilotShimDigestPath(): copilotStashMarker(copilotShimText()),
		copilotConfigPath():     `{"managed_by":"tokless","type":"openai","base_url":"http://127.0.0.1"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := restoreCopilotProxyFiles(snapshots); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{copilotShimPath(), copilotShimDigestPath(), copilotConfigPath()} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("new managed file survived rollback: %s", path)
		}
	}
}

func TestRealCopilotPathFollowsUnixSymlink(t *testing.T) {
	home := copilotTestHome(t)
	dir := filepath.Join(home, "bin")
	real := filepath.Join(home, "lib", "copilot-real")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "copilot")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got := realCopilotPath(copilotShimPath(), map[string]string{"PATH": dir}); got != link {
		t.Fatalf("realCopilotPath = %q, want symlink %q", got, link)
	}
}

func TestCopilotCommandAliasPreservesWindowsScriptExtension(t *testing.T) {
	copilotTestHome(t)
	oldWin := util.IsWin
	util.IsWin = true
	t.Cleanup(func() { util.IsWin = oldWin })
	real := filepath.Join(t.TempDir(), "copilot.cmd")
	if err := os.WriteFile(real, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	alias, cleanup, err := copilotCommandAlias(real)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Base(alias) != "copilot.cmd" {
		t.Fatalf("alias = %q, want copilot.cmd", alias)
	}
}

func TestCopilotLegacyVSCodeBlockIsOwned(t *testing.T) {
	copilotTestHome(t)
	raw := "{\n" + copilotVSCodeLegacyBlock(false) + "\n}\n"
	if !copilotVSCodeLegacyManaged(raw) {
		t.Fatal("legacy VS Code block was not recognized as managed")
	}
}

func TestConfigureCopilotCLIProxyRefusesForeignWindowsCommand(t *testing.T) {
	copilotTestHome(t)
	oldWin := util.IsWin
	util.IsWin = true
	t.Cleanup(func() { util.IsWin = oldWin })
	path := copilotShimPath()
	foreign := "@echo off\r\nforeign-copilot\r\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, _, err := configureCopilotCLIProxySafe()
	if changed || err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("ConfigureCopilotCLIProxy = changed=%v err=%v, want refusal", changed, err)
	}
	if got := mustReadCopilot(t, path); got != foreign {
		t.Fatalf("foreign Windows command changed: %q", got)
	}
}

func TestConfigureCopilotProxyStashesAndRestoresSameDirectoryCLI(t *testing.T) {
	copilotTestHome(t)
	path := copilotShimPath()
	original := "#!/bin/sh\nprintf 'real copilot\\n'\n"
	if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}

	if changed, _ := ConfigureCopilotCLIProxy(); !changed {
		t.Fatal("foreign CLI was not wrapped")
	}
	if got := mustReadCopilot(t, copilotStashedPath()); got != original {
		t.Fatalf("stashed CLI = %q, want %q", got, original)
	}
	if !CopilotCLIProxyWired() {
		t.Fatal("stashed CLI wrapper not detected")
	}
	if !RemoveCopilotCLIProxy() {
		t.Fatal("CLI wrapper removal failed")
	}
	if got := mustReadCopilot(t, path); got != original {
		t.Fatalf("restored CLI = %q, want %q", got, original)
	}
}

func TestConfigureCopilotProxyIsIdempotentAndProtectsForeignShim(t *testing.T) {
	copilotTestHome(t)
	if changed, _ := ConfigureCopilotProxy(); !changed {
		t.Fatal("first configure should change")
	}
	if changed, _ := ConfigureCopilotProxy(); changed {
		t.Fatal("second configure should be no-op")
	}
	path := copilotShimPath()
	foreign := "#!/bin/sh\nexec other-copilot\n"
	if err := os.WriteFile(path, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCopilotCLIProxy(); !changed || !CopilotCLIProxyWired() {
		t.Fatal("foreign CLI was not safely wrapped")
	}
	if got := mustReadCopilot(t, copilotStashedPath()); got != foreign {
		t.Fatalf("foreign CLI was not preserved: %q", got)
	}
}

func TestConfigureCopilotProxyStoresBYOKMetadataOnly(t *testing.T) {
	copilotTestHome(t)
	t.Setenv("COPILOT_PROVIDER_BASE_URL", "https://provider.example/v1")
	t.Setenv("COPILOT_PROVIDER_TYPE", "anthropic")
	t.Setenv("COPILOT_PROVIDER_MODEL_ID", "model-x")
	if changed, _ := ConfigureCopilotCLIProxy(); !changed {
		t.Fatal("shim install failed")
	}
	cfg := mustReadCopilot(t, filepath.Join(util.ToklessDataDir(), "copilot.json"))
	for _, want := range []string{"https://provider.example/v1", "anthropic", "model-x"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("provider metadata missing %q: %s", want, cfg)
		}
	}
	if strings.Contains(cfg, "secret") || strings.Contains(cfg, "sk-") {
		t.Fatalf("provider credential persisted: %s", cfg)
	}
}

func TestCopilotShimRunsWithoutShellStartupFiles(t *testing.T) {
	home := copilotTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")
	if changed, _ := ConfigureCopilotCLIProxy(); !changed {
		t.Fatal("shim install failed")
	}
	args := filepath.Join(home, "args")
	shim := mustReadCopilot(t, copilotShimPath())
	tokless := util.ToklessPersistedAbs()
	if !strings.Contains(shim, tokless) {
		t.Skip("test executable has no stable persisted path")
	}
	stub := filepath.Join(home, "tokless")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+args+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(shim, tokless, stub, 1)
	if err := os.WriteFile(copilotShimPath(), []byte(raw), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"sh", "bash", "zsh"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		if err := exec.Command(shell, copilotShimPath(), "--model", "test", "--", "--flag").Run(); err != nil {
			t.Fatalf("%s invocation failed: %v", shell, err)
		}
		got := mustReadCopilot(t, args)
		if !strings.Contains(got, "__copilot\n") || !strings.Contains(got, "--model\n") || !strings.Contains(got, "--flag\n") {
			t.Fatalf("%s did not forward arguments: %q", shell, got)
		}
	}
}

func TestRunCopilotCLISeparatesNativeAndBYOKModes(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		baseURL      string
		wantArgs     []string
		wantTarget   string
		forbidTarget string
	}{
		{name: "native", forbidTarget: "OPENAI_TARGET_API_URL"},
		{name: "openai byok", providerType: "openai", baseURL: "https://openai.example/v1", wantArgs: []string{"--provider-type", "openai"}, wantTarget: "OPENAI_TARGET_API_URL", forbidTarget: "ANTHROPIC_TARGET_API_URL"},
		{name: "anthropic byok", providerType: "anthropic", baseURL: "https://anthropic.example", wantArgs: []string{"--provider-type", "anthropic"}, wantTarget: "ANTHROPIC_TARGET_API_URL", forbidTarget: "OPENAI_TARGET_API_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := copilotTestHome(t)
			binDir := filepath.Join(home, "bin")
			managedDir := filepath.Dir(util.HeadroomBin())
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(managedDir, 0o755); err != nil {
				t.Fatal(err)
			}
			argsFile := filepath.Join(home, "headroom.args")
			envFile := filepath.Join(home, "headroom.env")
			headroom := util.HeadroomBin()
			realCopilot := filepath.Join(binDir, "copilot")
			headroomScript := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\n/usr/bin/env > " + envFile + "\n"
			for path, content := range map[string]string{headroom: headroomScript, realCopilot: "#!/bin/sh\n"} {
				if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", binDir)
			t.Setenv("COPILOT_PROVIDER_BASE_URL", tt.baseURL)
			t.Setenv("COPILOT_PROVIDER_TYPE", tt.providerType)
			t.Setenv("COPILOT_PROVIDER_API_KEY", "test-key")
			t.Setenv("OPENAI_TARGET_API_URL", "stale-openai")
			t.Setenv("ANTHROPIC_TARGET_API_URL", "stale-anthropic")
			if got := RunCopilotCLI([]string{"--model", "test-model"}); got != 0 {
				t.Fatalf("RunCopilotCLI exit = %d", got)
			}
			argsRaw, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSpace(string(argsRaw)), "\n")
			for _, want := range append(tt.wantArgs, "copilot") {
				found := false
				for _, arg := range args {
					if arg == want {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("Headroom args %v missing %q", args, want)
				}
			}
			envRaw, err := os.ReadFile(envFile)
			if err != nil {
				t.Fatal(err)
			}
			env := string(envRaw)
			if tt.wantTarget != "" && !strings.Contains(env, tt.wantTarget+"="+tt.baseURL) {
				t.Fatalf("Headroom env missing %s: %s", tt.wantTarget, env)
			}
			if strings.Contains(env, tt.forbidTarget+"=") {
				t.Fatalf("Headroom env leaked %s: %s", tt.forbidTarget, env)
			}
		})
	}
}

func TestConfigureCopilotProxyKeepsVSCodeWiringSeparate(t *testing.T) {
	home := copilotTestHome(t)
	path := filepath.Join(home, ".config", "Code", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := mustReadCopilot(t, path)
	t.Setenv("VSCODE_EXTENSIONS", filepath.Join(home, "extensions"))
	if err := os.MkdirAll(filepath.Join(home, "extensions", "github.copilot-chat-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCopilotProxy(); !changed {
		t.Fatal("Copilot proxy configure failed")
	}
	if raw := mustReadCopilot(t, path); raw != before {
		t.Fatalf("VS Code settings changed: %s", raw)
	}
}

func TestConfigureCopilotAllProxyWiresAndRemovesVSCode(t *testing.T) {
	home := copilotTestHome(t)
	path := filepath.Join(home, ".config", "Code", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSCODE_EXTENSIONS", filepath.Join(home, "extensions"))
	if err := os.MkdirAll(filepath.Join(home, "extensions", "github.copilot-chat-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !ConfigureCopilotAllProxy() || !CopilotVSCodeProxyWired() {
		t.Fatal("all Copilot surfaces were not wired")
	}
	if !RemoveCopilotProxy() || CopilotVSCodeProxyWired() || CopilotCLIProxyWired() {
		t.Fatal("all Copilot surfaces were not removed")
	}
}

func TestConfigureCopilotProxyHandlesJSONCTrailingBraces(t *testing.T) {
	home := copilotTestHome(t)
	path := filepath.Join(home, ".config", "Code", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\n\t\"custom\": \"brace } in string\"\n}\n// trailing comment with }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSCODE_EXTENSIONS", filepath.Join(home, "extensions"))
	if err := os.MkdirAll(filepath.Join(home, "extensions", "github.copilot-chat-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := mustReadCopilot(t, path)
	if changed, _ := ConfigureCopilotProxy(); !changed {
		t.Fatal("Copilot proxy configure failed")
	}
	if raw := mustReadCopilot(t, path); raw != before {
		t.Fatalf("VS Code JSONC changed: %s", raw)
	}
}

func TestConfigureCopilotProxyAddsCommaBeforeJSONCComment(t *testing.T) {
	home := copilotTestHome(t)
	path := filepath.Join(home, ".config", "Code", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\n\t\"custom\": 1 // trailing\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSCODE_EXTENSIONS", filepath.Join(home, "extensions"))
	if err := os.MkdirAll(filepath.Join(home, "extensions", "github.copilot-chat-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCopilotProxy(); !changed {
		t.Fatal("Copilot proxy configure failed")
	}
	if raw := mustReadCopilot(t, path); raw != "{\n\t\"custom\": 1 // trailing\n}\n" {
		t.Fatalf("VS Code JSONC changed: %s", raw)
	}
}

func TestConfigureRemoveCopilotProxyPreservesTrailingJSONCComment(t *testing.T) {
	home := copilotTestHome(t)
	path := filepath.Join(home, ".config", "Code", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "{\n\t\"custom\": 1 // trailing\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCopilotProxy(); !changed || !RemoveCopilotProxy() {
		t.Fatal("Copilot proxy configure/remove failed")
	}
	if got := mustReadCopilot(t, path); got != original {
		t.Fatalf("settings round trip = %q, want %q", got, original)
	}
}

func TestRemoveCopilotProxyOnlyRemovesToklessFiles(t *testing.T) {
	home := copilotTestHome(t)
	startup := filepath.Join(home, ".zshenv")
	if err := os.WriteFile(startup, []byte("export KEEP=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCopilotCLIProxy(); !changed {
		t.Fatal("shim install failed")
	}
	if !RemoveCopilotProxy() || CopilotCLIProxyWired() {
		t.Fatal("shim removal failed")
	}
	if got := mustReadCopilot(t, startup); got != "export KEEP=1\n" {
		t.Fatalf("startup file changed: %q", got)
	}
}

func TestDetectCopilotProxyReportsExecutableLauncher(t *testing.T) {
	copilotTestHome(t)
	if got := detectCopilotProxy(ProxyCapability{ID: "copilot"}); got.State != ProxyStateUnconfigured {
		t.Fatalf("initial state = %s", got.State)
	}
	if changed, _ := ConfigureCopilotCLIProxy(); !changed {
		t.Fatal("shim install failed")
	}
	got := detectCopilotProxy(ProxyCapability{ID: "copilot"})
	if got.State != ProxyStateManaged || !strings.Contains(got.Detail, "executable") {
		t.Fatalf("managed state = %s (%s)", got.State, got.Detail)
	}
}

func mustReadCopilot(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func fileModeCopilot(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
