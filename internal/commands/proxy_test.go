package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

// proxyCmdTestHome isolates agent config writes from the real $HOME so proxy
// command tests never touch live agent wiring.
func proxyCmdTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("test headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { util.SetHomeOverride("") })
}

// TestProxyInstructionsEndpointShapes pins the manual/env guidance to the same
// /v1-vs-bare split, matching each upstream CLI's expected base-URL shape.
func TestProxyInstructionsEndpointShapes(t *testing.T) {
	openai := headroompkg.ProxyOpenAIURL()
	bare := headroompkg.ProxyURL()

	_ = openai
	_ = bare

	cursor := strings.Join(proxyInstructions("cursor"), "\n")
	if !strings.Contains(cursor, "DEPRECATED") || !strings.Contains(cursor, "native") {
		t.Fatalf("cursor instructions must state deprecation/native OAuth: %q", cursor)
	}

	if proxyInstructions("claude") != nil || proxyInstructions("antigravity") != nil || proxyInstructions("codex") != nil {
		t.Fatal("wired agents must have no manual instructions")
	}
	if got := strings.Join(proxyInstructions("cursor"), " "); !strings.Contains(got, "DEPRECATED") {
		t.Fatalf("cursor instructions = %q", got)
	}
}

func TestRunProxyStatusProbesDaemonOnceAndDoesNotWireManual(t *testing.T) {
	proxyCmdTestHome(t)
	t.Setenv("GROK_MODELS_BASE_URL", headroompkg.ProxyOpenAIURL())
	var probes int
	oldRunning := proxyRunning
	proxyRunning = func() bool { probes++; return true }
	t.Cleanup(func() { proxyRunning = oldRunning })
	logs, err := util.CaptureLogs(func() error {
		if got := RunProxyStatus(InitOptions{Agents: []string{"grok"}}); got != 0 {
			t.Fatalf("status exit code = %d", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if probes != 1 {
		t.Fatalf("daemon probes = %d, want 1", probes)
	}
	if strings.Contains(logs, "→") || strings.Contains(logs, headroompkg.ProxyOpenAIURL()) {
		t.Fatalf("manual status rendered as wired endpoint: %q", logs)
	}
	if !strings.Contains(logs, string(agents.ProxyStateAbsent)) {
		t.Fatalf("grok status missing absent state: %q", logs)
	}
}

func TestRunProxyDownRetainsDaemonForSelectedSubset(t *testing.T) {
	proxyCmdTestHome(t)
	var stops int
	oldStop, oldDisable := stopProxy, disableProxyAutostart
	stopProxy = func() error { stops++; return nil }
	disableProxyAutostart = func() error { t.Fatal("subset disabled autostart"); return nil }
	t.Cleanup(func() {
		stopProxy, disableProxyAutostart = oldStop, oldDisable
	})
	if got := RunProxyDown(InitOptions{Agents: []string{"grok"}}); got != 0 {
		t.Fatalf("proxy down subset exit code = %d, want 0", got)
	}
	if stops != 0 {
		t.Fatalf("stop proxy call count = %d, want 0", stops)
	}
}

func TestRunProxyDownRetainsGrokDaemonWhenOtherAgentsSelected(t *testing.T) {
	proxyCmdTestHome(t)
	oldStop, oldDisable := stopProxy, disableProxyAutostart
	stopProxy = func() error { t.Fatal("shared stop on subset"); return nil }
	disableProxyAutostart = func() error { t.Fatal("autostart disable on subset"); return nil }
	t.Cleanup(func() {
		stopProxy, disableProxyAutostart = oldStop, oldDisable
	})
	if got := RunProxyDown(InitOptions{Agents: []string{"claude"}}); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
}

func TestRunProxyStatusHidesGrokWhenNotSelected(t *testing.T) {
	proxyCmdTestHome(t)
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	if err := os.WriteFile(filepath.Join(grokHome, "config.toml"), []byte("[models]\ndefault = \"grok\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldRunning := proxyRunning
	proxyRunning = func() bool { return true }
	t.Cleanup(func() { proxyRunning = oldRunning })
	logs, err := util.CaptureLogs(func() error {
		if got := RunProxyStatus(InitOptions{Agents: []string{"claude"}}); got != 0 {
			t.Fatalf("status exit = %d", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs, "grok oauth proxy") {
		t.Fatalf("status leaked grok daemon line: %q", logs)
	}
}

func TestRunProxyDownDryRunMakesNoChanges(t *testing.T) {
	proxyCmdTestHome(t)
	oldRemove, oldStop, oldDisable := removeProxyAgent, stopProxy, disableProxyAutostart
	t.Cleanup(func() { removeProxyAgent, stopProxy, disableProxyAutostart = oldRemove, oldStop, oldDisable })
	removeProxyAgent = func(string) bool { t.Fatal("dry-run removed proxy wiring"); return false }
	stopProxy = func() error { t.Fatal("dry-run stopped proxy"); return nil }
	disableProxyAutostart = func() error { t.Fatal("dry-run disabled autostart"); return nil }
	if got := RunProxyDown(InitOptions{DryRun: true}); got != 0 {
		t.Fatalf("proxy down dry-run exit code = %d, want 0", got)
	}
}

func TestRunProxyDownStopsAfterFullUnwire(t *testing.T) {
	proxyCmdTestHome(t)
	var stops int
	oldStop, oldDisable := stopProxy, disableProxyAutostart
	stopProxy = func() error { stops++; return nil }
	disableProxyAutostart = func() error { return nil }
	t.Cleanup(func() { stopProxy, disableProxyAutostart = oldStop, oldDisable })
	if got := RunProxyDown(InitOptions{}); got != 0 {
		t.Fatalf("proxy down full exit code = %d, want 0", got)
	}
	if stops != 1 {
		t.Fatalf("stop proxy call count = %d, want 1", stops)
	}
}

func TestRunProxyDownRetainsDaemonWhenUnwireFails(t *testing.T) {
	proxyCmdTestHome(t)
	var stops int
	oldStop, oldDisable := stopProxy, disableProxyAutostart
	stopProxy = func() error { stops++; return nil }
	disableProxyAutostart = func() error { t.Fatal("unwire-fail disabled autostart"); return nil }
	t.Cleanup(func() { stopProxy, disableProxyAutostart = oldStop, oldDisable })
	oldRemove := removeProxyAgent
	removeProxyAgent = func(string) bool { return false }
	t.Cleanup(func() { removeProxyAgent = oldRemove })
	oldWired := proxyAgentWired
	proxyAgentWired = func(string) bool { return true }
	t.Cleanup(func() { proxyAgentWired = oldWired })
	if got := RunProxyDown(InitOptions{}); got == 0 {
		t.Fatal("proxy down must report failed unwire")
	}
	if stops != 0 {
		t.Fatalf("stop proxy call count = %d, want 0", stops)
	}
}

func TestRunProxyUpKeepsSuccessfulWiringOnAgentFailure(t *testing.T) {
	proxyCmdTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")
	for _, dir := range []string{util.ClaudeCodePaths().Dir, util.CodexPathsResolved().Dir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldStart, oldStop := startProxy, stopProxy
	oldEnable, oldAuto := enableProxyAutostart, proxyAutostartEnabled
	oldRunning := proxyRunning
	oldConfigure, oldRemove, oldWired := configureProxyAgent, removeProxyAgent, proxyAgentWired
	t.Cleanup(func() {
		startProxy, stopProxy, proxyRunning = oldStart, oldStop, oldRunning
		enableProxyAutostart, proxyAutostartEnabled = oldEnable, oldAuto
		configureProxyAgent, removeProxyAgent, proxyAgentWired = oldConfigure, oldRemove, oldWired
	})
	started, stopped, removed := 0, 0, 0
	startProxy = func() error { started++; return nil }
	stopProxy = func() error { stopped++; return nil }
	enableProxyAutostart = func() error { return nil }
	proxyAutostartEnabled = func() bool { return true }
	proxyRunning = func() bool { return true }
	wired := map[string]bool{}
	configureProxyAgent = func(id string) bool {
		if id == "codex" {
			return false
		}
		wired[id] = true
		return true
	}
	removeProxyAgent = func(id string) bool {
		if !wired[id] {
			return false
		}
		delete(wired, id)
		removed++
		return true
	}
	proxyAgentWired = func(id string) bool { return wired[id] }

	if got := RunProxyUp(InitOptions{Agents: []string{"claude", "codex"}}); got != 1 {
		t.Fatalf("proxy up exit = %d, want 1 (agent wiring failure)", got)
	}
	if started != 1 || stopped != 0 {
		t.Fatalf("proxy lifecycle start=%d stop=%d, want 1/0 (shared daemon was already running)", started, stopped)
	}
	if len(wired) != 0 {
		t.Fatalf("failed proxy up must roll back wiring: %v", wired)
	}
	if removed != 1 {
		t.Fatalf("successful wiring must be rolled back, removed=%d", removed)
	}
}

func TestRunProxyUpReportsRollbackFailure(t *testing.T) {
	proxyCmdTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")
	oldStart, oldStop := startProxy, stopProxy
	oldEnable, oldAuto, oldRunning := enableProxyAutostart, proxyAutostartEnabled, proxyRunning
	oldConfigure, oldRemove, oldWired := configureProxyAgent, removeProxyAgent, proxyAgentWired
	t.Cleanup(func() {
		startProxy, stopProxy, enableProxyAutostart, proxyAutostartEnabled, proxyRunning = oldStart, oldStop, oldEnable, oldAuto, oldRunning
		configureProxyAgent, removeProxyAgent, proxyAgentWired = oldConfigure, oldRemove, oldWired
	})
	startProxy = func() error { return nil }
	stopProxy = func() error { return fmt.Errorf("injected stop failure") }
	enableProxyAutostart = func() error { return nil }
	proxyAutostartEnabled = func() bool { return true }
	proxyRunning = func() bool { return false }
	configureProxyAgent = func(id string) bool { return id == "claude" }
	removeProxyAgent = func(string) bool { return true }
	proxyAgentWired = func(string) bool { return false }

	logs, err := util.CaptureLogs(func() error {
		if got := RunProxyUp(InitOptions{Agents: []string{"claude", "codex"}}); got != 1 {
			return fmt.Errorf("exit=%d", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs, "proxy rollback failed: injected stop failure") {
		t.Fatalf("rollback failure not reported: %q", logs)
	}
}

func TestRunProxyUpReportsGrokConfigureFailureAndPreservesState(t *testing.T) {
	proxyCmdTestHome(t)
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	config := `[model_providers.local]
base_url = "https://provider.example/v1"
api_key = "sk-local"

[models]
default = "local-model"

[model.local-model]
model_provider = "local"
`
	configFile := filepath.Join(grokHome, "config.toml")
	if err := os.WriteFile(configFile, []byte(config), 0o640); err != nil {
		t.Fatal(err)
	}
	oldStart, oldStop, oldRunning := startProxy, stopProxy, proxyRunning
	startProxy = func() error { return nil }
	stopProxy = func() error { return nil }
	proxyRunning = func() bool { return false }
	t.Cleanup(func() { startProxy, stopProxy, proxyRunning = oldStart, oldStop, oldRunning })
	util.SetWriteFileOverride(func(path, content string) error {
		if path == configFile {
			return fmt.Errorf("injected Grok config write failure")
		}
		return os.WriteFile(path, []byte(content), 0o600)
	})
	t.Cleanup(func() { util.SetWriteFileOverride(nil) })

	exit := 0
	logs, err := util.CaptureLogs(func() error {
		exit = RunProxyUp(InitOptions{Agents: []string{"grok"}})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if exit != 1 {
		t.Fatalf("exit=%d logs=%q", exit, logs)
	}
	if !strings.Contains(logs, "injected Grok config write failure") {
		t.Fatalf("configure failure not surfaced: %q", logs)
	}
	if got, _ := util.ReadFileSafe(configFile); got != config {
		t.Fatal("Grok config changed after command-facing configure failure")
	}
	if _, ok := util.ReadFileSafe(filepath.Join(util.HeadroomPathsResolved().Root, "grok.proxy.stash.json")); ok {
		t.Fatal("Grok stash survived command-facing configure failure")
	}
	if info, err := os.Stat(configFile); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("config mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestRunProxyDownReportsGrokRemoveFailureAndPreservesState(t *testing.T) {
	proxyCmdTestHome(t)
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	configFile := filepath.Join(grokHome, "config.toml")
	if err := os.WriteFile(configFile, []byte(`[model_providers.local]
base_url = "https://provider.example/v1"
api_key = "sk-local"
`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := agents.ConfigureGrokProxyChecked(); err != nil {
		t.Fatal(err)
	}
	configBefore, _ := util.ReadFileSafe(configFile)
	stashFile := filepath.Join(util.HeadroomPathsResolved().Root, "grok.proxy.stash.json")
	stashBefore, _ := util.ReadFileSafe(stashFile)
	writes := 0
	util.SetWriteFileOverride(func(path, content string) error {
		if path == configFile {
			writes++
			if writes == 1 {
				return fmt.Errorf("injected Grok remove write failure")
			}
		}
		return os.WriteFile(path, []byte(content), 0o600)
	})
	t.Cleanup(func() { util.SetWriteFileOverride(nil) })

	logs, err := util.CaptureLogs(func() error {
		if got := RunProxyDown(InitOptions{Agents: []string{"grok"}}); got != 1 {
			return fmt.Errorf("exit=%d", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs, "injected Grok remove write failure") {
		t.Fatalf("remove failure not surfaced: %q", logs)
	}
	if got, _ := util.ReadFileSafe(configFile); got != configBefore {
		t.Fatal("Grok config changed after command-facing remove failure")
	}
	if got, _ := util.ReadFileSafe(stashFile); got != stashBefore {
		t.Fatal("Grok stash changed after command-facing remove failure")
	}
	if info, err := os.Stat(configFile); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("config mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestRunProxyUpDryRunMakesNoChanges(t *testing.T) {
	proxyCmdTestHome(t)
	oldStart, oldEnable := startProxy, enableProxyAutostart
	oldConfigure := configureProxyAgent
	t.Cleanup(func() {
		startProxy, enableProxyAutostart, configureProxyAgent = oldStart, oldEnable, oldConfigure
	})
	started := 0
	startProxy = func() error { started++; return nil }
	enableProxyAutostart = func() error { return nil }
	configured := 0
	configureProxyAgent = func(string) bool { configured++; return true }

	if got := RunProxyUp(InitOptions{Agents: []string{"copilot"}, DryRun: true}); got != 0 {
		t.Fatalf("dry-run exit = %d, want 0", got)
	}
	if started != 0 || configured != 0 {
		t.Fatalf("dry-run must not start proxy or wire agents, start=%d configured=%d", started, configured)
	}
}

func TestValidateProxyUpAgentsFailsClosedOnForeignState(t *testing.T) {
	proxyCmdTestHome(t)
	t.Setenv("TOKLESS_TEST", "1")
	if err := os.MkdirAll(filepath.Dir(util.ClaudeCodePaths().Settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(util.ClaudeCodePaths().Settings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://foreign.example"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateProxyUpAgents([]string{"claude"}); err == nil {
		t.Fatal("foreign proxy state must fail closed")
	}
}

func TestPlanAProxyPortsPinned(t *testing.T) {
	t.Setenv("TOKLESS_HEADROOM_PORT", "")
	t.Setenv("TOKLESS_COPILOT_PROXY_PORT", "")
	t.Setenv("TOKLESS_COPILOT_CLI_PROXY_PORT", "")
	if got := util.HeadroomProxyPort(); got != 8787 {
		t.Fatalf("shared port = %d, want 8787", got)
	}
	if got := agents.CopilotProxyPort(); got != 8789 {
		t.Fatalf("copilot VS Code port = %d, want 8789", got)
	}
	if got := agents.CopilotCLIProxyPort(); got != 8790 {
		t.Fatalf("copilot CLI port = %d, want 8790", got)
	}
	if util.HeadroomProxyPort() == 18787 {
		t.Fatal("shared port must not be 18787")
	}
}

func TestRunProxyUpStartsHeadroomBeforeConfiguringGrok(t *testing.T) {
	proxyCmdTestHome(t)
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	if err := os.WriteFile(filepath.Join(grokHome, "config.toml"), []byte("[models]\ndefault = \"grok-4.6\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldStart, oldStartGrok, oldStopGrok, oldOwned, oldEnable, oldAuto, oldRunning := startProxy, startGrokProxy, stopGrokProxy, grokProxyOwned, enableProxyAutostart, proxyAutostartEnabled, proxyRunning
	startProxy = func() error {
		raw, err := os.ReadFile(filepath.Join(grokHome, "config.toml"))
		if err != nil || string(raw) != "[models]\ndefault = \"grok-4.6\"\n" {
			t.Fatalf("Grok configured before Headroom start: %q (err=%v)", raw, err)
		}
		return nil
	}
	startGrokProxy = func() error {
		raw, err := os.ReadFile(filepath.Join(grokHome, "config.toml"))
		if err != nil || string(raw) == "# test\n" {
			t.Fatalf("Grok OAuth proxy started before Grok configuration: %q (err=%v)", raw, err)
		}
		return nil
	}
	enableProxyAutostart = func() error { return nil }
	proxyAutostartEnabled = func() bool { return true }
	proxyRunning = func() bool { return true }
	t.Cleanup(func() {
		startProxy, startGrokProxy, stopGrokProxy, grokProxyOwned = oldStart, oldStartGrok, oldStopGrok, oldOwned
		enableProxyAutostart, proxyAutostartEnabled, proxyRunning = oldEnable, oldAuto, oldRunning
	})
	if got := RunProxyUp(InitOptions{Agents: []string{"grok"}}); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if got := agents.DetectProxy("grok"); got.State != agents.ProxyStateManaged {
		t.Fatalf("Grok state = %s (%s), want managed", got.State, got.Detail)
	}
}

func TestRunProxyUpStopsOwnedGrokDaemonWhenStartupReturnsError(t *testing.T) {
	proxyCmdTestHome(t)
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	if err := os.WriteFile(filepath.Join(grokHome, "config.toml"), []byte("[models]\ndefault = \"grok-4.6\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldStart, oldStop, oldOwned := startGrokProxy, stopGrokProxy, grokProxyOwned
	oldRunning := proxyRunning
	startGrokProxy = func() error { return os.ErrPermission }
	stopped := 0
	stopGrokProxy = func() error { stopped++; return nil }
	grokProxyOwned = func() bool { return true }
	proxyRunning = func() bool { return true }
	t.Cleanup(func() {
		startGrokProxy, stopGrokProxy, grokProxyOwned, proxyRunning = oldStart, oldStop, oldOwned, oldRunning
	})

	if got := RunProxyUp(InitOptions{Agents: []string{"grok"}}); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}
	if stopped != 1 {
		t.Fatalf("owned Grok daemon stops = %d, want 1", stopped)
	}
}

func TestRunProxyUpStopsNewGrokDaemonWhenLaterAgentFails(t *testing.T) {
	proxyCmdTestHome(t)
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	if err := os.WriteFile(filepath.Join(grokHome, "config.toml"), []byte("[models]\ndefault = \"grok-4.6\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldStart, oldStop := startProxy, stopProxy
	oldStartGrok, oldStopGrok := startGrokProxy, stopGrokProxy
	oldEnable, oldAuto, oldRunning := enableProxyAutostart, proxyAutostartEnabled, proxyRunning
	oldConfigure, oldRemove, oldWired := configureProxyAgent, removeProxyAgent, proxyAgentWired
	t.Cleanup(func() {
		startProxy, stopProxy = oldStart, oldStop
		startGrokProxy, stopGrokProxy = oldStartGrok, oldStopGrok
		enableProxyAutostart, proxyAutostartEnabled, proxyRunning = oldEnable, oldAuto, oldRunning
		configureProxyAgent, removeProxyAgent, proxyAgentWired = oldConfigure, oldRemove, oldWired
	})
	started, stopped := 0, 0
	startProxy = func() error { return nil }
	stopProxy = func() error { return nil }
	startGrokProxy = func() error { started++; return nil }
	stopGrokProxy = func() error { stopped++; return nil }
	enableProxyAutostart = func() error { return nil }
	proxyAutostartEnabled = func() bool { return true }
	proxyRunning = func() bool { return true }
	configureProxyAgent = func(id string) bool { return id != "codex" }
	removeProxyAgent = func(string) bool { return true }
	proxyAgentWired = func(string) bool { return false }

	if got := RunProxyUp(InitOptions{Agents: []string{"grok", "codex"}}); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}
	if started != 1 || stopped != 1 {
		t.Fatalf("Grok daemon lifecycle start=%d stop=%d, want 1/1", started, stopped)
	}
}

func TestRunProxyUpGrokOnlyPreservesSharedPreference(t *testing.T) {
	proxyCmdTestHome(t)
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	if err := os.WriteFile(filepath.Join(grokHome, "config.toml"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := util.SetProxyRoutingEnabled(false); err != nil {
		t.Fatal(err)
	}
	oldStart, oldRunning := startGrokProxy, proxyRunning
	startGrokProxy = func() error { return nil }
	proxyRunning = func() bool { return true }
	t.Cleanup(func() { startGrokProxy, proxyRunning = oldStart, oldRunning })

	if got := RunProxyUp(InitOptions{Agents: []string{"grok"}}); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if util.ProxyRoutingEnabled() {
		t.Fatal("Grok-only proxy up enabled shared routing preference")
	}
}

func TestRunProxyUpRejectsProxyLanePortCollision(t *testing.T) {
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "8788")
	t.Setenv("TOKLESS_GROK_PROXY_PORT", "8788")
	if err := validateProxyLanePorts(); err == nil {
		t.Fatal("duplicate proxy lane ports must fail closed")
	}
}
