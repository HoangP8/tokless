package tools

import (
	"fmt"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

func headroomWire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		if !headroompkg.CanConfigure(agent) {
			return false, fmt.Errorf("%s already has a non-Tokless headroom MCP entry; refusing to overwrite it", agent)
		}
		if !headroompkg.ConfigureMcp(agent) && !headroompkg.McpMatches(agent) {
			return false, nil
		}
		if agent != "cursor" {
			if agent == "kilo" {
				kiloWriteOwner("headroom")
			} else {
				WriteOwner(agent, "headroom")
			}
		}
		if agent == "copilot" {
			agents.ConfigureCopilotIdeMcp("headroom")
			agents.SyncCopilotIdeInstructions()
		}
		return headroomVerify(agent), nil
	}
}

func headroomUnwire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		owned := HasOwner(agent, "headroom")
		if agent == "kilo" {
			owned = kiloHasOwner("headroom")
		}
		if agent != "cursor" && (!owned || !headroompkg.McpMatches(agent)) {
			return false, nil
		}
		switch agent {
		case "claude":
			agents.RemoveClaudeMcp("headroom")
		case "opencode":
			agents.RemoveOpenCodeMcp("headroom")
		case "codex":
			p := util.CodexPathsResolved().Config
			if raw, ok := util.ReadFileSafe(p); ok {
				_ = util.WriteFile(p, util.RemoveBlock(raw, "mcp_servers.headroom"))
			}
		case "cursor":
			agents.RemoveCursorMcp("headroom")
		case "antigravity":
			agents.RemoveAntigravityMcp("headroom")
		case "copilot":
			agents.RemoveCopilotMcp("headroom")
			agents.RemoveCopilotIdeMcp("headroom")
		case "droid":
			agents.RemoveDroidMcp("headroom")
		case "grok":
			if _, err := agents.RemoveGrokMcp("headroom"); err != nil {
				return false, err
			}
		case "pi":
			agents.RemovePiMcp("headroom")
		case "omp":
			agents.RemoveOmpMcp("headroom")
		case "kilo":
			agents.RemoveKiloMcp("headroom")
		case "cline":
			agents.RemoveClineMcp("headroom")
		}
		if agent == "kilo" {
			kiloRemoveOwner("headroom")
		} else if agent != "cursor" {
			RemoveOwner(agent, "headroom")
		}
		return true, nil
	}
}

func headroomVerify(agent string) bool {
	if !isTest() && !util.HeadroomInstalled() {
		return false
	}
	if agent == "cursor" {
		return headroompkg.McpMatches(agent)
	}
	if agent == "kilo" {
		return headroompkg.McpMatches(agent) && kiloHasOwner("headroom")
	}
	if agent == "grok" {
		return agents.GrokHeadroomMcpHas() && HasOwner(agent, "headroom")
	}
	return headroompkg.McpMatches(agent) && HasOwner(agent, "headroom")
}

var headroom = &core.ToolManifest{
	ID: "headroom", Label: "Headroom", Description: "On-demand MCP compression for large, self-contained text.",
	Homepage: "https://github.com/headroomlabs-ai/headroom", InstallHint: "Tokless-managed uv tool: headroom-ai[mcp] (Python 3.13).",
	Channel: core.ChannelBinary, NotTrackable: true, Install: headroompkg.EnsureInstalled,
	WireFor: map[string]core.AgentFn{}, UnwireFor: map[string]core.AgentFn{}, VerifyFor: map[string]core.VerifyFn{},
}

func init() {
	for _, agent := range []string{"claude", "opencode", "codex", "cursor", "antigravity", "copilot", "droid", "grok", "pi", "omp", "kilo", "cline"} {
		headroom.WireFor[agent] = headroomWire(agent)
		headroom.UnwireFor[agent] = headroomUnwire(agent)
		headroom.VerifyFor[agent] = func(agent string) core.VerifyFn { return func() *bool { return core.BoolPtr(headroomVerify(agent)) } }(agent)
	}
}
