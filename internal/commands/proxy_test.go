package commands

import (
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

// TestProxyInstructionsEndpointShapes pins the manual/env guidance to the same
// /v1-vs-bare split, matching each upstream CLI's expected base-URL shape.
func TestProxyInstructionsEndpointShapes(t *testing.T) {
	openai := headroompkg.ProxyOpenAIURL()
	bare := headroompkg.ProxyURL()

	if got := proxyInstructions("grok"); len(got) != 1 || got[0] != "export GROK_MODELS_BASE_URL="+openai {
		t.Fatalf("grok instructions = %v", got)
	}
	if got := proxyInstructions("copilot"); len(got) != 1 || got[0] != "export COPILOT_PROVIDER_BASE_URL="+openai {
		t.Fatalf("copilot instructions = %v", got)
	}

	cline := strings.Join(proxyInstructions("cline"), "\n")
	if !strings.Contains(cline, "Anthropic Base URL: "+bare) {
		t.Fatalf("cline instructions missing bare anthropic URL: %q", cline)
	}
	if !strings.Contains(cline, "OpenAI Compatible Base URL: "+openai) {
		t.Fatalf("cline instructions missing openai /v1 URL: %q", cline)
	}

	cursor := strings.Join(proxyInstructions("cursor"), "\n")
	if !strings.Contains(cursor, "Base URL: "+openai) {
		t.Fatalf("cursor instructions missing openai /v1 URL: %q", cursor)
	}

	if proxyInstructions("claude") != nil || proxyInstructions("codex") != nil || proxyInstructions("antigravity") != nil {
		t.Fatal("wired/MCP-only agents must have no manual instructions")
	}
}

func TestRunProxyStatusProbesDaemonOnceAndDoesNotWireManual(t *testing.T) {
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
	if !strings.Contains(logs, string(agents.ProxyStateUnknown)) || !strings.Contains(logs, "ownership unknown") {
		t.Fatalf("manual status missing truthful state: %q", logs)
	}
}

func TestRunProxyDownRetainsDaemonForSelectedSubset(t *testing.T) {
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

func TestRunProxyDownStopsAfterFullUnwire(t *testing.T) {
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
