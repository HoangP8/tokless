package commands

import (
	"github.com/HoangP8/tokless/internal/agents"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

// ProxyAgentIDs is the full `tokless proxy` agent vocabulary in display order:
// wired agents first (config-file injection), then manual/env agents.
func ProxyAgentIDs() []string {
	return []string{
		"claude", "opencode", "omp", "kilo", "pi", "droid", "antigravity", "grok", "copilot", "cline", // wired
		"codex", "cursor", // manual / launch-env
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
	switch id {
	case "codex":
		return []string{"codex-via-proxy is DEPRECATED — headroom cannot compress the /v1/responses shape (upstream prefix-cache safety) and codex no longer supports wire_api=chat; keep codex on its native provider."}
	case "cursor":
		return []string{
			"cursor-via-proxy is DEPRECATED — upstream supports manual settings-UI setup only.",
			"Keep cursor on native api2.cursor.sh OAuth.",
		}
	}
	return nil
}

func configureProxyAgent(id string) bool {
	return agents.ConfigureProxyAgent(id)
}

var removeProxyAgent = removeProxyAgentImpl

func removeProxyAgentImpl(id string) bool {
	removed := agents.RemoveProxyAgent(id)
	if removed && proxyAgentWired(id) {
		return false
	}
	return removed
}

var proxyAgentWired = proxyAgentWiredImpl

func proxyAgentWiredImpl(id string) bool {
	return agents.ProxyAgentWired(id)
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
		if proxyInstructions(id) != nil {
			continue
		}
		switch {
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
		if proxyInstructions(id) == nil {
			wiredBefore[id] = proxyAgentWired(id)
		}
	}
	for _, id := range resolveProxyAgents(opts) {
		if proxyInstructions(id) != nil {
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
		if detection.State == agents.ProxyStateManaged && detection.Capability.WireKind != agents.ProxyWireManual {
			label += " " + util.C.Dim("→") + " " + agents.ProxyEndpointFor(id)
			util.L.Raw("  " + statusOK(label))
		} else {
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" "+label))
		}
	}
	util.L.Raw("")
	return 0
}
