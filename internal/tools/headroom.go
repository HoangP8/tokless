package tools

import (
	"errors"
	"fmt"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

var setProxyRoutingEnabled = util.SetProxyRoutingEnabled
var acquireProxyLifecycleLock = headroompkg.AcquireProxyLifecycleLock

// headroomWired reports agents with an applicable config that tokless can wire.
func headroomWired(id string) bool {
	return agents.ProxyEndpointFor(id) != "" && agents.ProxyAgentApplicable(id)
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
		if util.ProxyRoutingPreferenceSet() && !util.ProxyRoutingEnabled() {
			return true, nil
		}
		releaseLifecycle, err := acquireProxyLifecycleLock()
		if err != nil {
			return false, fmt.Errorf("proxy lifecycle lock: %w", err)
		}
		defer releaseLifecycle()
		if util.ProxyRoutingPreferenceSet() && !util.ProxyRoutingEnabled() {
			return true, nil
		}
		preferenceSet := util.ProxyRoutingPreferenceSet()
		preferenceEnabled := util.ProxyRoutingEnabled()
		runtimeBefore, runtimeWasSet := util.ReadProxyRuntime()
		restoreRuntime := func() error {
			if runtimeWasSet {
				return util.SaveHeadroomProxyRuntime(runtimeBefore)
			}
			return util.ClearHeadroomProxyRuntime()
		}
		wasRunning, autostartBefore := false, false
		if !isTest() {
			wasRunning = headroompkg.ProxyRunning()
			autostartBefore = headroompkg.ProxyAutostartConfigured()
			if err := headroompkg.StartProxy(); err != nil {
				return false, errors.Join(err, restoreRuntime())
			}
			if !autostartBefore {
				err := headroompkg.EnableProxyAutostart()
				if err != nil && !errors.Is(err, headroompkg.ErrProxyAutostartUnavailable) {
					if !wasRunning {
						if stopErr := headroompkg.StopProxy(); stopErr != nil {
							err = errors.Join(err, fmt.Errorf("proxy rollback: %w", stopErr))
						}
					}
					return false, errors.Join(err, restoreRuntime())
				}
			}
		}
		wasWired := agents.ProxyAgentWired(agent)
		rollback := func() error {
			var rollbackErrs []error
			if !wasWired && agents.ProxyAgentWired(agent) && !agents.RemoveProxyAgent(agent) {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("%s proxy wiring rollback failed", agent))
			}
			if !isTest() {
				if !wasRunning {
					stop := headroompkg.StopProxy
					if autostartBefore {
						stop = headroompkg.StopProxyPreservingAutostart
					}
					if err := stop(); err != nil {
						rollbackErrs = append(rollbackErrs, fmt.Errorf("proxy rollback: %w", err))
					}
				} else if !autostartBefore {
					if err := headroompkg.DisableProxyAutostart(); err != nil {
						rollbackErrs = append(rollbackErrs, fmt.Errorf("autostart rollback: %w", err))
					}
					if err := headroompkg.StartProxy(); err != nil {
						rollbackErrs = append(rollbackErrs, fmt.Errorf("proxy restart rollback: %w", err))
					}
				}
			}
			if !preferenceSet {
				if err := util.ClearProxyRoutingPreference(); err != nil {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("proxy preference rollback: %w", err))
				}
			} else if err := util.SetProxyRoutingEnabled(preferenceEnabled); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("proxy preference rollback: %w", err))
			}
			if err := restoreRuntime(); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("proxy runtime rollback: %w", err))
			}
			return errors.Join(rollbackErrs...)
		}
		if !agents.ConfigureProxyAgent(agent) {
			return false, errors.Join(
				fmt.Errorf("%s proxy wiring not applied (differing existing config value, or write failed)", agent),
				rollback(),
			)
		}
		if !util.ProxyRoutingPreferenceSet() {
			if err := setProxyRoutingEnabled(true); err != nil {
				return false, errors.Join(fmt.Errorf("headroom proxy preference: %w", err), rollback())
			}
		}
		if !headroomVerify(agent) {
			return false, errors.Join(fmt.Errorf("%s proxy wiring verification failed", agent), rollback())
		}
		return true, nil
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
		releaseLifecycle, err := acquireProxyLifecycleLock()
		if err != nil {
			return false, fmt.Errorf("proxy lifecycle lock: %w", err)
		}
		defer releaseLifecycle()
		if !headroomWired(agent) || !agents.ProxyAgentWired(agent) {
			return false, nil
		}
		if !agents.RemoveProxyAgent(agent) {
			return false, fmt.Errorf("%s proxy unwire failed — removal did not take effect", agent)
		}
		if agents.ProxyAgentWired(agent) {
			return false, fmt.Errorf("%s proxy unwire incomplete — managed wiring remains", agent)
		}
		return true, nil
	}
}

func headroomVerify(agent string) bool {
	if util.ProxyRoutingPreferenceSet() && !util.ProxyRoutingEnabled() {
		return true
	}
	if !isTest() && !util.HeadroomInstalled() {
		return false
	}
	if !headroomWired(agent) {
		return true
	}
	if agent == "opencode" {
		return agents.OpenCodeProxySatisfied()
	}
	return agents.ProxyAgentWired(agent)
}

var headroom = &core.ToolManifest{
	ID: "headroom", Label: "Headroom", Description: "On-demand token compression proxy for large, self-contained text.",
	Homepage: "https://github.com/headroomlabs-ai/headroom", InstallHint: "Tokless-managed uv tool: headroom-ai[proxy] (Python 3.13).",
	Channel: core.ChannelUV, Install: headroompkg.EnsureInstalled,
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
