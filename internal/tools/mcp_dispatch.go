package tools

import (
	"encoding/json"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// mcpAdapter returns the MCP adapter for an agent, or nil when unknown.
func mcpAdapter(agentID string) core.McpAdapter {
	if a := core.GetAgent(agentID); a != nil && a.Mcp != nil {
		return a.Mcp
	}
	return agents.McpAdapterFor(agentID)
}

// antigravityDryRunMcp logs the payload a real run would write.
func antigravityDryRunMcp(toolID string) {
	payload, _ := json.Marshal(agents.AntigravityMcpEntry(toolID))
	util.L.Info("[dry-run] would set mcpServers." + toolID + " in " + agents.AntigravityMcpConfigPath() + ": " + string(payload))
}

// mcpConfigure wires toolID for agentID. Unknown agents are a no-op success.
func mcpConfigure(agentID, toolID string) bool {
	a := mcpAdapter(agentID)
	if a == nil {
		return true
	}
	reply := a.ConfigureMcp(toolID, core.WithCommand(agents.McpCommand(agentID, toolID)))
	return reply.Err == nil && (reply.Changed || a.HasMcp(toolID))
}

// mcpRemove unwires toolID for agentID. Unknown agents are a no-op success.
func mcpRemove(agentID, toolID string) bool {
	a := mcpAdapter(agentID)
	if a == nil {
		return true
	}
	reply := a.RemoveMcp(toolID, core.WithCommand(agents.McpCommand(agentID, toolID)))
	return reply.Err == nil
}

// mcpHas reports whether agentID holds the toolID MCP entry. anyShape uses the
// loose presence gate (v1 or v2, foreign included); false uses the strict gate.
func mcpHas(agentID, toolID string, anyShape bool) bool {
	a := mcpAdapter(agentID)
	if a == nil {
		return false
	}
	opts := []core.McpOption{core.WithCommand(agents.McpCommand(agentID, toolID))}
	if anyShape {
		opts = append(opts, core.WithAnyShape())
	}
	return a.HasMcp(toolID, opts...)
}
