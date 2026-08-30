package commands

import (
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
	oldStop := stopProxy
	stopProxy = func() error { stops++; return nil }
	t.Cleanup(func() { stopProxy = oldStop })
	if got := RunProxyDown(InitOptions{Agents: []string{"grok"}}); got != 0 {
		t.Fatalf("proxy down subset exit code = %d, want 0", got)
	}
	if stops != 0 {
		t.Fatalf("stop proxy call count = %d, want 0", stops)
	}
}

func TestRunProxyDownDryRunMakesNoChanges(t *testing.T) {
	proxyCmdTestHome(t)
	oldRemove, oldStop := removeProxyAgent, stopProxy
	t.Cleanup(func() { removeProxyAgent, stopProxy = oldRemove, oldStop })
	removeProxyAgent = func(string) bool { t.Fatal("dry-run removed proxy wiring"); return false }
	stopProxy = func() error { t.Fatal("dry-run stopped proxy"); return nil }
	if got := RunProxyDown(InitOptions{DryRun: true}); got != 0 {
		t.Fatalf("proxy down dry-run exit code = %d, want 0", got)
	}
}

func TestRunProxyDownStopsAfterFullUnwire(t *testing.T) {
	proxyCmdTestHome(t)
	var stops int
	oldStop := stopProxy
	stopProxy = func() error { stops++; return nil }
	t.Cleanup(func() { stopProxy = oldStop })
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
	oldStop := stopProxy
	stopProxy = func() error { stops++; return nil }
	t.Cleanup(func() { stopProxy = oldStop })
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
		t.Fatalf("proxy lifecycle start=%d stop=%d, want 1/0 (fail-soft keeps daemon)", started, stopped)
	}
	if len(wired) != 1 || !wired["claude"] {
		t.Fatalf("successful wiring must survive: %v", wired)
	}
	if removed != 0 {
		t.Fatalf("agent wiring failures must not remove agents, removed=%d", removed)
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
