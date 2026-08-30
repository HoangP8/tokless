package commands

import (
	"strconv"

	"github.com/HoangP8/tokless/internal/agents"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

// ProxyAgentIDs is the full `tokless proxy` agent vocabulary in display order:
// wired agents first (config-file injection), then manual/env agents.
func ProxyAgentIDs() []string {
	return []string{
		"claude", "codex", "opencode", "omp", "kilo", "pi", "droid", "antigravity", "grok", "copilot", "cline", // wired
		"cursor", // manual / launch-env
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
	case "cursor":
		return []string{
			"cursor-via-proxy is DEPRECATED — upstream supports manual settings-UI setup only.",
			"Keep cursor on native api2.cursor.sh OAuth.",
		}
	}
	return nil
}

var configureProxyAgent = configureProxyAgentImpl

func configureProxyAgentImpl(id string) bool {
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
var startProxy = headroompkg.StartProxy
var enableProxyAutostart = headroompkg.EnableProxyAutostart
var proxyAutostartEnabled = headroompkg.ProxyAutostartEnabled
var proxyRunning = headroompkg.ProxyRunning

// RunProxyUp starts the headroom proxy daemon and points agents at it.
func RunProxyUp(opts InitOptions) int {
	cmdHeader("proxy up", "start the headroom HTTP proxy and point agents at it")
	if headroompkg.ResolveHeadroomBin() == "" {
		util.L.Err("headroom binary not found — run `tokless` first to install headroom")
		return 1
	}
	wasRunning := proxyRunning()
	if err := startProxy(); err != nil {
		util.L.Err(err.Error())
		return 1
	}
	if hasGrokAgent(resolveProxyAgents(opts)) && agents.ProxyAgentApplicable("grok") {
		if err := headroompkg.StartGrokOAuthProxy(); err != nil {
			util.L.Sub("grok oauth proxy: " + err.Error())
		} else {
			util.L.Ok("grok oauth proxy on http://127.0.0.1:" + strconv.Itoa(util.GrokOAuthProxyPort()) + " (upstream cli-chat-proxy.grok.com)")
		}
	}
	wired, failed := 0, 0
	configured := make([]string, 0)
	agentsBefore := make(map[string]bool)
	for _, id := range resolveProxyAgents(opts) {
		if proxyInstructions(id) != nil {
			continue
		}
		if !agents.ProxyAgentApplicable(id) {
			continue
		}
		agentsBefore[id] = proxyAgentWired(id)
		switch {
		case configureProxyAgent(id):
			util.L.Ok(id + ": wired to " + agents.ProxyEndpointFor(id))
			wired++
			if !agentsBefore[id] {
				configured = append(configured, id)
			}
		default:
			if agentsBefore[id] {
				continue
			}
			util.L.Err(id + ": not wired (differing existing config value, or write failed)")
			failed++
		}
	}
	rollback := func() {
		for i := len(configured) - 1; i >= 0; i-- {
			id := configured[i]
			if removeProxyAgent(id) {
				continue
			}
			util.L.Sub("agent rollback failed: " + id)
		}
		if !wasRunning {
			if err := stopProxy(); err != nil {
				util.L.Sub("proxy rollback failed: " + err.Error())
			}
			if err := headroompkg.StopGrokOAuthProxy(); err != nil {
				util.L.Sub("grok oauth proxy rollback: " + err.Error())
			}
		}
	}
	if failed > 0 {
		rollback()
		return 1
	}
	if err := enableProxyAutostart(); err != nil {
		util.L.Sub("autostart: " + err.Error())
		if startErr := startProxy(); startErr != nil {
			util.L.Err(startErr.Error())
			rollback()
			return 1
		}
	} else if proxyAutostartEnabled() {
		util.L.Sub("autostart: user service enabled (survives reboot)")
	} else if !proxyRunning() {
		util.L.Err("proxy did not remain running")
		rollback()
		return 1
	}
	util.L.Raw("")
	if failed > 0 {
		return 1
	}
	if wired == 0 {
		util.L.Raw("  " + util.C.Gray("No agents wired."))
	}
	if hasGrokAgent(resolveProxyAgents(opts)) {
		util.L.Raw("")
		util.L.Raw("  " + util.C.Dim("Grok CLI OAuth: automatic — the grok launcher routes xAI traffic through the local proxy."))
		util.L.Raw("  " + util.C.Dim("Grok session upstream: https://cli-chat-proxy.grok.com (dedicated grok proxy on port " + strconv.Itoa(util.GrokOAuthProxyPort()) + ")."))
	}
	return 0
}

func hasGrokAgent(ids []string) bool {
	for _, id := range ids {
		if id == "grok" {
			return true
		}
	}
	return false
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
			restoreProxyAgents(wiredBefore)
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
		restoreProxyAgents(wiredBefore)
		return 1
	}
	if err := headroompkg.DisableProxyAutostart(); err != nil {
		util.L.Sub("autostart: " + err.Error())
		restoreProxyAgents(wiredBefore)
		return 1
	}
	if err := stopProxy(); err != nil {
		util.L.Err(err.Error())
		if restoreErr := headroompkg.EnableProxyAutostart(); restoreErr != nil {
			util.L.Sub("autostart restore: " + restoreErr.Error())
		}
		restoreProxyAgents(wiredBefore)
		if err := headroompkg.StopGrokOAuthProxy(); err != nil {
			util.L.Sub("grok oauth proxy: " + err.Error())
		}
		return 1
	}
	if err := headroompkg.StopGrokOAuthProxy(); err != nil {
		util.L.Sub("grok oauth proxy: " + err.Error())
	}
	util.L.Raw("")
	return 0
}

func restoreProxyAgents(wiredBefore map[string]bool) {
	for id, wired := range wiredBefore {
		if wired && !proxyAgentWired(id) && !configureProxyAgent(id) {
			util.L.Sub("agent restore failed: " + id)
		}
	}
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
	if headroompkg.ProxyAutostartEnabled() {
		util.L.Raw("  " + statusOK("autostart: user service enabled"))
	} else {
		util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" autostart: off (run proxy up once)"))
	}
	if headroompkg.GrokOAuthProxyRunning() {
		if headroompkg.GrokOAuthProxyOwned() {
			util.L.Raw("  " + statusOK("grok oauth proxy: running on http://127.0.0.1:" + strconv.Itoa(util.GrokOAuthProxyPort()) + " (upstream cli-chat-proxy.grok.com)"))
		} else {
			util.L.Raw("  " + statusWarn("grok oauth proxy: unverified listener on http://127.0.0.1:" + strconv.Itoa(util.GrokOAuthProxyPort()) + " (not owned by tokless)"))
		}
	} else if agents.GrokProxyApplicable() {
		util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" grok oauth proxy: not running (grok CLI falls back to direct)"))
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
