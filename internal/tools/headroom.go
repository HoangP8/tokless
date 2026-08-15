package tools

import (
	"fmt"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

// headroomWired reports agents whose proxy config tokless manages by writing a
// config file (claude, omp, codex, opencode, kilo, pi, droid, antigravity).
// Manual agents (grok, copilot, cline, cursor) and any unregistered agent are
// not wired and are handled as no-ops.
func headroomWired(id string) bool {
	return agents.ProxyEndpointFor(id) != ""
}

// headroomWire configures the headroom HTTP proxy for a single agent and
// ensures the daemon is running so the wiring is immediately usable.
func headroomWire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		if !headroomWired(agent) {
			return true, nil
		}
		if !isTest() {
			if err := headroompkg.StartProxy(); err != nil {
				return false, err
			}
		}
		if !agents.ConfigureProxyAgent(agent) {
			return false, fmt.Errorf("%s proxy wiring not applied (differing existing config value, or write failed)", agent)
		}
		return headroomVerify(agent), nil
	}
}

// headroomUnwire removes the headroom proxy config tokless wrote for an agent.
// Manual agents have no written config and are reported as not wired.
func headroomUnwire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		if !headroomWired(agent) {
			return false, nil
		}
		if !agents.ProxyAgentWired(agent) {
			return false, nil
		}
		if !agents.RemoveProxyAgent(agent) {
			return false, fmt.Errorf("%s proxy unwire failed — removal did not take effect", agent)
		}
		return true, nil
	}
}

func headroomVerify(agent string) bool {
	if !isTest() && !util.HeadroomInstalled() {
		return false
	}
	if !headroomWired(agent) {
		return true
	}
	return agents.ProxyAgentWired(agent)
}

var headroom = &core.ToolManifest{
	ID: "headroom", Label: "Headroom", Description: "On-demand token compression proxy for large, self-contained text.",
	Homepage: "https://github.com/headroomlabs-ai/headroom", InstallHint: "Tokless-managed uv tool: headroom-ai[proxy] (Python 3.13).",
	Channel: core.ChannelBinary, NotTrackable: true, Install: headroompkg.EnsureInstalled,
	WireFor: map[string]core.AgentFn{}, UnwireFor: map[string]core.AgentFn{}, VerifyFor: map[string]core.VerifyFn{},
}

func init() {
	// All registered agents are wired, but Register() runs from main after
	// package init, so core.AgentIDs() is incomplete here. Enumerate the proxy-
	// supported set explicitly; unregistered-but-supported agents are filtered
	// by ProxyEndpointFor at wire time.
	for _, agent := range []string{"claude", "opencode", "codex", "cursor", "antigravity", "copilot", "droid", "grok", "pi", "omp", "kilo", "cline"} {
		headroom.WireFor[agent] = headroomWire(agent)
		headroom.UnwireFor[agent] = headroomUnwire(agent)
		headroom.VerifyFor[agent] = func(agent string) core.VerifyFn { return func() *bool { return core.BoolPtr(headroomVerify(agent)) } }(agent)
	}
}