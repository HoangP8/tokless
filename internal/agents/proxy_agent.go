package agents

import (
	"os"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ProxyAgentSpec is static metadata plus wire-side function references for one
// agent's headroom proxy integration.
type ProxyAgentSpec struct {
	ID        string
	Protocol  ProxyProtocol
	WireKind  ProxyWireKind
	Configure func() (changed bool, file string)
	Remove    func() bool
	Wired     func() bool
}

var proxyAgentSpecs = map[string]ProxyAgentSpec{
	"claude":      {"claude", ProxyProtocolAnthropicNative, ProxyWireManagedRoute, ConfigureClaudeProxy, RemoveClaudeProxy, ClaudeProxyWired},
	"omp":         {"omp", ProxyProtocolOpenAICompatible, ProxyWireAdditiveProvider, ConfigureOmpProxy, RemoveOmpProxy, OmpProxyWired},
	"codex":       {"codex", ProxyProtocolOpenAICompatible, ProxyWireManagedRoute, ConfigureCodexProxy, RemoveCodexProxy, CodexProxyWired},
	"opencode":    {"opencode", ProxyProtocolOpenAICompatible, ProxyWireAdditiveProvider, ConfigureOpenCodeProxy, RemoveOpenCodeProxy, OpenCodeProxyWired},
	"kilo":        {"kilo", ProxyProtocolOpenAICompatible, ProxyWireAdditiveProvider, ConfigureKiloProxy, RemoveKiloProxy, KiloProxyWired},
	"pi":          {"pi", ProxyProtocolOpenAICompatible, ProxyWireAdditiveProvider, ConfigurePiProxy, RemovePiProxy, PiProxyWired},
	"droid":       {"droid", ProxyProtocolOpenAICompatible, ProxyWireAdditiveProvider, ConfigureDroidProxy, RemoveDroidProxy, DroidProxyWired},
	"copilot":     {"copilot", ProxyProtocolOpenAICompatible, ProxyWireManagedRoute, func() (bool, string) { return ConfigureCopilotAllProxy(), "" }, RemoveCopilotProxy, CopilotProxyWired},
	"cline":       {"cline", ProxyProtocolOpenAICompatible, ProxyWireManagedRoute, ConfigureClineProxy, RemoveClineProxy, ClineProxyWired},
	"cursor":      {"cursor", ProxyProtocolNone, ProxyWireManual, nil, nil, nil},
	"antigravity": {"antigravity", ProxyProtocolGeminiNative, ProxyWireManagedRoute, ConfigureAntigravityProxy, RemoveAntigravityProxy, AntigravityProxyWired},
}

func proxySpecFor(id string) (ProxyAgentSpec, bool) {
	spec, ok := proxyAgentSpecs[id]
	return spec, ok
}

// ProxyAgentApplicable reports whether the agent's config file exists for
// tokless to extend; false means wire attempts are skipped, never forced.
func ProxyAgentApplicable(id string) bool {
	if id == "grok" {
		if _, err := os.Stat(grokConfigFile()); err == nil {
			return true
		}
		if _, err := os.Stat(grokBinFile()); err == nil {
			return true
		}
		return false
	}
	if id == "copilot" {
		return CopilotProxyWired() || util.Which("copilot") != ""
	}
	if ProxyAgentWired(id) {
		return true
	}
	agent := core.GetAgent(id)
	return agent != nil && agent.Detect != nil && agent.Detect().Installed
}

// ProxyAgentUsesHeadroom reports whether tokless should actively route the
// agent through the shared Headroom daemon. Grok owns its OAuth context in its
// native CLI and must not share the generic OpenAI-compatible auth path.
func ProxyAgentUsesHeadroom(id string) bool {
	return id != "grok" && id != "cursor"
}

// ConfigureProxyAgent wires id to the headroom proxy. Returns true when the
// resulting config matches exactly what tokless would inject.
func ConfigureProxyAgent(id string) bool {
	if id == "grok" {
		return false
	}
	spec, ok := proxySpecFor(id)
	if !ok || spec.Configure == nil {
		return false
	}
	_, _ = spec.Configure()
	if id == "copilot" {
		return CopilotProxyWired()
	}
	if id == "opencode" {
		return OpenCodeProxySatisfied()
	}
	return spec.Wired != nil && spec.Wired()
}

// RemoveProxyAgent unwires id only when its proxy config still matches what
// tokless set.
func RemoveProxyAgent(id string) bool {
	removed, _ := RemoveProxyAgentChecked(id)
	return removed
}

func RemoveProxyAgentChecked(id string) (bool, error) {
	if id == "grok" {
		return RemoveGrokProxyChecked()
	}
	spec, ok := proxySpecFor(id)
	if !ok || spec.Remove == nil {
		return false, nil
	}
	return spec.Remove(), nil
}

// ProxyAgentWired reports whether id's proxy config exactly matches tokless's.
func ProxyAgentWired(id string) bool {
	if id == "grok" {
		return GrokProxyWired()
	}
	spec, ok := proxySpecFor(id)
	if !ok || spec.Wired == nil {
		return false
	}
	return spec.Wired()
}
