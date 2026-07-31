package tools

import (
	"strings"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// Shared wiring for the two simple shapes: a plain MCP tool (server entry plus
// instruction section) and a skill (instruction section only). Tools that also
// need hooks (codegraph, context-mode) keep their own wiring.

var wiredAgents = []string{"claude", "opencode", "codex", "antigravity", "copilot", "droid", "pi"}

// skillAgentMaps wires a skill: its instruction section, nothing else.
func skillAgentMaps(owner string) (map[string]core.AgentFn, map[string]core.AgentFn, map[string]core.VerifyFn) {
	wire := map[string]core.AgentFn{}
	unwire := map[string]core.AgentFn{}
	verify := map[string]core.VerifyFn{}
	for _, agent := range wiredAgents {
		a := agent
		wire[a] = func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would add " + owner + " section to " + a)
				return true, nil
			}
			_ = WriteOwner(a, owner)
			syncCopilotIde(a)
			return HasOwner(a, owner), nil
		}
		unwire[a] = func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			RemoveOwner(a, owner)
			syncCopilotIde(a)
			return true, nil
		}
		verify[a] = func() *bool { return core.BoolPtr(HasOwner(a, owner)) }
	}
	return wire, unwire, verify
}

// Copilot reads instructions from a second copy in the project.
func syncCopilotIde(agent string) {
	if agent == "copilot" {
		agents.SyncCopilotIdeInstructions()
	}
}

func configureMcp(agent, toolID string) {
	switch agent {
	case "claude":
		agents.ConfigureClaudeMcp(toolID)
	case "opencode":
		agents.ConfigureOpenCodeMcp(toolID)
	case "codex":
		agents.ConfigureCodexMcp(toolID)
	case "antigravity":
		agents.ConfigureAntigravityMcp(toolID)
	case "copilot":
		agents.ConfigureCopilotMcp(toolID)
		agents.ConfigureCopilotIdeMcp(toolID)
	case "droid":
		agents.ConfigureDroidMcp(toolID)
	case "pi":
		agents.ConfigurePiMcp(toolID)
	}
}

func removeMcp(agent, toolID string) {
	switch agent {
	case "claude":
		agents.RemoveClaudeMcp(toolID)
	case "opencode":
		agents.RemoveOpenCodeMcp(toolID)
	case "codex":
		removeCodexMcpBlock(toolID)
	case "antigravity":
		agents.RemoveAntigravityMcp(toolID)
	case "copilot":
		agents.RemoveCopilotMcp(toolID)
		agents.RemoveCopilotIdeMcp(toolID)
	case "droid":
		agents.RemoveDroidMcp(toolID)
	case "pi":
		agents.RemovePiMcp(toolID)
	}
}

func removeCodexMcpBlock(toolID string) {
	cx := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(cx.Config)
	if !ok {
		return
	}
	if next := util.RemoveBlock(raw, "mcp_servers."+toolID); next != raw {
		_ = util.WriteFile(cx.Config, next)
	}
}

func mcpEntryPresent(agent, toolID string) bool {
	switch agent {
	case "claude":
		return util.McpEntryHas(util.ClaudeCodePaths().GlobalJSON, "mcpServers", toolID)
	case "opencode":
		return util.McpEntryHas(util.OpenCodePathsResolved().Config, "mcp", toolID)
	case "codex":
		raw, _ := util.ReadFileSafe(util.CodexPathsResolved().Config)
		return strings.Contains(raw, "[mcp_servers."+toolID+"]")
	case "antigravity":
		return agents.AntigravityMcpHas(toolID)
	case "copilot":
		return agents.CopilotMcpHas(toolID) && agents.CopilotIdeMcpHas(toolID)
	case "droid":
		return agents.DroidMcpHas(toolID)
	case "pi":
		return agents.PiMcpHas(toolID)
	}
	return false
}

func mcpVerify(agent, toolID string) bool {
	return mcpEntryPresent(agent, toolID) && HasOwner(agent, toolID)
}

// mcpWire adds the MCP entry and the instruction section. Pi has no built-in
// MCP client, so it needs the adapter package first.
func mcpWire(agent, toolID string, ready func() bool) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would wire " + toolID + " MCP + instructions for " + agent)
			return true, nil
		}
		if !isTest() {
			if ready != nil && !ready() {
				return false, nil
			}
			if agent == "pi" && !agents.PiInstallSource(agents.PiSrcMcpAdapter) {
				return false, nil
			}
		}
		configureMcp(agent, toolID)
		WriteOwner(agent, toolID)
		syncCopilotIde(agent)
		return mcpVerify(agent, toolID), nil
	}
}

func mcpUnwire(agent, toolID string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		removeMcp(agent, toolID)
		RemoveOwner(agent, toolID)
		if agent == "pi" && !agents.PiMcpHasAny() {
			agents.PiRemoveSource(agents.PiSrcMcpAdapter)
		}
		syncCopilotIde(agent)
		return true, nil
	}
}

func mcpAgentMaps(toolID string, ready func() bool) (map[string]core.AgentFn, map[string]core.AgentFn, map[string]core.VerifyFn) {
	wire := map[string]core.AgentFn{}
	unwire := map[string]core.AgentFn{}
	verify := map[string]core.VerifyFn{}
	for _, agent := range wiredAgents {
		a := agent
		wire[a] = mcpWire(a, toolID, ready)
		unwire[a] = mcpUnwire(a, toolID)
		verify[a] = func() *bool { return core.BoolPtr(mcpVerify(a, toolID)) }
	}
	return wire, unwire, verify
}
