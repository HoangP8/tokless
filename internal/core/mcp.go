package core

// McpRequest carries per-call options for MCP adapter operations.
type McpRequest struct {
	ToolID   string
	Command  []string // for agents that need an explicit command (kilo, cline)
	Ide      bool     // for agents with an IDE surface (copilot)
	AnyShape bool     // when true, HasMcp accepts either v1 or v2 shape
}

// McpReply is the result of a ConfigureMcp or RemoveMcp call.
type McpReply struct {
	Changed bool
	File    string
	Err     error
}

// McpOption configures an McpRequest.
type McpOption func(*McpRequest)

// WithCommand supplies the canonical command for agents that require it.
func WithCommand(cmd []string) McpOption {
	return func(r *McpRequest) { r.Command = cmd }
}

// WithIde targets the IDE-side MCP config (copilot).
func WithIde(ide bool) McpOption {
	return func(r *McpRequest) { r.Ide = ide }
}

// WithAnyShape loosens HasMcp to accept either v1 or v2 schema shapes.
func WithAnyShape() McpOption {
	return func(r *McpRequest) { r.AnyShape = true }
}

// McpAdapter abstracts MCP configure/remove/has for a single agent.
type McpAdapter interface {
	ConfigureMcp(toolID string, opts ...McpOption) McpReply
	RemoveMcp(toolID string, opts ...McpOption) McpReply
	HasMcp(toolID string, opts ...McpOption) bool
}
