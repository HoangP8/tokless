package commands

import (
	"github.com/HoangP8/tokless/internal/agents"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

// ProxyAgentIDs is the full `tokless proxy` agent vocabulary in display order:
// wired agents first (config-file injection), then manual/env agents, then MCP-only.
func ProxyAgentIDs() []string {
	return []string{
		"claude", "codex", "opencode", "omp", "kilo", "pi", "droid", // wired
		"grok", "copilot", "cline", "cursor", // manual / launch-env
		"antigravity", // MCP-only
	}
}

func resolveProxyAgents(opts InitOptions) []string {
	if opts.Agents != nil {
		return opts.Agents
	}
	return ProxyAgentIDs()
}

// proxyInstructions returns the exact manual/env guidance for agents that
// tokless cannot wire via a config file.
func proxyInstructions(id string) []string {
	base := headroompkg.ProxyURL()
	openai := headroompkg.ProxyOpenAIURL()
	switch id {
	case "grok":
		return []string{"export GROK_MODELS_BASE_URL=" + openai}
	case "copilot":
		return []string{"export COPILOT_PROVIDER_BASE_URL=" + openai}
	case "cline":
		return []string{
			"VS Code: Cline settings → API Provider",
			"  Anthropic Base URL: " + base,
			"  OpenAI Compatible Base URL: " + openai,
		}
	case "cursor":
		return []string{
			"Cursor settings → Models → OpenAI API Key → Override OpenAI Base URL",
			"  Base URL: " + openai,
		}
	}
	return nil
}

func configureProxyAgent(id string) bool {
	switch id {
	case "claude":
		_, _ = agents.ConfigureClaudeProxy()
		return agents.ClaudeProxyWired()
	case "codex":
		_, _ = agents.ConfigureCodexProxy()
		return agents.CodexProxyWired()
	case "opencode":
		_, _ = agents.ConfigureOpenCodeProxy()
		return agents.OpenCodeProxyWired()
	case "omp":
		_, _ = agents.ConfigureOmpProxy()
		return agents.OmpProxyWired()
	case "kilo":
		_, _ = agents.ConfigureKiloProxy()
		return agents.KiloProxyWired()
	case "pi":
		_, _ = agents.ConfigurePiProxy()
		return agents.PiProxyWired()
	case "droid":
		_, _ = agents.ConfigureDroidProxy()
		return agents.DroidProxyWired()
	}
	return false
}

var removeProxyAgent = removeProxyAgentImpl

func removeProxyAgentImpl(id string) bool {
	var removed bool
	switch id {
	case "claude":
		removed = agents.RemoveClaudeProxy()
	case "codex":
		removed = agents.RemoveCodexProxy()
	case "opencode":
		removed = agents.RemoveOpenCodeProxy()
	case "omp":
		removed = agents.RemoveOmpProxy()
	case "kilo":
		removed = agents.RemoveKiloProxy()
	case "pi":
		removed = agents.RemovePiProxy()
	case "droid":
		removed = agents.RemoveDroidProxy()
	}
	if removed && proxyAgentWired(id) {
		return false
	}
	return removed
}

var proxyAgentWired = proxyAgentWiredImpl

func proxyAgentWiredImpl(id string) bool {
	switch id {
	case "claude":
		return agents.ClaudeProxyWired()
	case "codex":
		return agents.CodexProxyWired()
	case "opencode":
		return agents.OpenCodeProxyWired()
	case "omp":
		return agents.OmpProxyWired()
	case "kilo":
		return agents.KiloProxyWired()
	case "pi":
		return agents.PiProxyWired()
	case "droid":
		return agents.DroidProxyWired()
	}
	return false
}

var stopProxy = headroompkg.StopProxy
var proxyRunning = headroompkg.ProxyRunning

// RunProxyUp starts the headroom proxy daemon and points agents at it.
func RunProxyUp(opts InitOptions) int {
	cmdHeader("proxy up", "start the headroom HTTP proxy and point agents at it")
	if headroompkg.ResolveHeadroomBin() == "" {
		util.L.Err("headroom binary not found — run `tokless` first to install headroom")
		return 1
	}
	if err := headroompkg.StartProxy(); err != nil {
		util.L.Err(err.Error())
		return 1
	}
	wired, failed := 0, 0
	for _, id := range resolveProxyAgents(opts) {
		switch {
		case id == "antigravity":
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" antigravity: MCP-only (no client base-URL knob)"))
		case proxyInstructions(id) != nil:
			util.L.Raw("  " + util.Sym.Bullet + " " + id + ": manual (cannot auto-wire) — set:")
			for _, line := range proxyInstructions(id) {
				util.L.Raw("      " + line)
			}
		case configureProxyAgent(id):
			util.L.Ok(id + ": wired to " + agents.ProxyEndpointFor(id))
			wired++
		default:
			util.L.Err(id + ": not wired (differing existing config value, or write failed)")
			failed++
		}
	}
	util.L.Raw("")
	if failed > 0 {
		return 1
	}
	if wired == 0 {
		util.L.Raw("  " + util.C.Gray("No agents wired."))
	}
	return 0
}

// RunProxyDown unwires agents, then stops the daemon when operating on the
// complete agent set and every currently-wired config agent was unwired.
func RunProxyDown(opts InitOptions) int {
	cmdHeader("proxy down", "unwire agents and stop the headroom HTTP proxy")
	failed := 0
	selected := opts.Agents != nil
	wiredBefore := map[string]bool{}
	for _, id := range resolveProxyAgents(opts) {
		if proxyInstructions(id) == nil && id != "antigravity" {
			wiredBefore[id] = proxyAgentWired(id)
		}
	}
	for _, id := range resolveProxyAgents(opts) {
		if id == "antigravity" || proxyInstructions(id) != nil {
			continue // nothing written; nothing to unwire
		}
		switch {
		case removeProxyAgent(id):
			util.L.Ok(id + ": proxy wiring removed")
		case wiredBefore[id]:
			util.L.Err(id + ": unwire failed — removal did not take effect; leaving config untouched")
			failed++
		default:
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" "+id+": not wired"))
		}
	}
	if selected {
		util.L.Raw("")
		if failed > 0 {
			return 1
		}
		return 0
	}
	for id, wired := range wiredBefore {
		if wired && proxyAgentWired(id) {
			failed++
		}
	}
	if failed > 0 {
		util.L.Raw("")
		return 1
	}
	if err := stopProxy(); err != nil {
		util.L.Err(err.Error())
		return 1
	}
	util.L.Raw("")
	return 0
}

// RunProxyStatus prints daemon + per-agent capability and wiring state.
func RunProxyStatus(opts InitOptions) int {
	cmdHeader("proxy status", "headroom HTTP-proxy daemon and agent wiring")
	url := headroompkg.ProxyURL()
	if proxyRunning() {
		util.L.Raw("  " + statusOK("proxy: running on "+url))
	} else {
		util.L.Raw("  " + statusWarn("proxy: not running ("+url+")"))
	}
	for _, id := range resolveProxyAgents(opts) {
		detection := agents.DetectProxy(id)
		label := id + ": " + string(detection.Capability.WireKind) + ", " + string(detection.Capability.Protocol) + " — " + string(detection.State)
		if detection.Detail != "" {
			label += " (" + detection.Detail + ")"
		}
		if detection.State == agents.ProxyStateManaged && detection.Capability.WireKind != agents.ProxyWireManual && detection.Capability.WireKind != agents.ProxyWireMCPOnly {
			label += " " + util.C.Dim("→") + " " + agents.ProxyEndpointFor(id)
			util.L.Raw("  " + statusOK(label))
		} else {
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" "+label))
		}
	}
	util.L.Raw("")
	return 0
}
