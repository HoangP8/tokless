package util

// Upstream context-mode ids accepted by `context-mode server --agent <id>`.
// Each id picks how the server finds the project (see context-mode project resolution).
var ContextModeAgentIDs = []string{"claude-code", "codex", "gemini", "qwen", "copilot-cli", "vscode", "kiro", "zcode", "pi", "opencode"}

// CtxAgentFlag is the run-mcp flag that carries the upstream context-mode id.
// Unlike run-mcp --agent it never starts the headroom proxy.
const CtxAgentFlag = "--ctx-agent"

const workspacePlaceholder = "${workspaceFolder}"

type contextModeHost struct {
	id        string
	workspace bool // host expands ${workspaceFolder} in MCP args
}

// Tokless harness id -> upstream context-mode host. Anything missing runs as
// "gemini", which resolves the project from the spawn dir (run-mcp may still
// pick claude-code at runtime when CLAUDE_PROJECT_DIR is set). pi stays on gemini:
// upstream "pi" reads its cwd from the native Pi extension, not from MCP.
// IDE hosts with a workspace placeholder use "vscode" + --workspace-folder;
// "copilot-ide" is the VS Code surface of the copilot harness.
var contextModeHosts = map[string]contextModeHost{
	"claude":      {id: "claude-code"},
	"codex":       {id: "codex"},
	"copilot":     {id: "copilot-cli"},
	"copilot-ide": {id: "vscode", workspace: true},
	"cursor":      {id: "vscode", workspace: true},
	"zcode":       {id: "zcode"},
	"opencode":    {id: "opencode"},
	"opencode2":   {id: "opencode"},
}

// ContextModeAgentFor returns the upstream context-mode agent id for a tokless harness.
func ContextModeAgentFor(harness string) string {
	if h, ok := contextModeHosts[harness]; ok {
		return h.id
	}
	return "gemini"
}

// ContextModeHostFlags returns the run-mcp flags that tell context-mode which host it serves.
// Nil for the default "gemini" id, so those entries keep the pre-flag shape.
func ContextModeHostFlags(harness string) []string {
	id := ContextModeAgentFor(harness)
	if id == "gemini" {
		return nil
	}
	flags := []string{CtxAgentFlag, id}
	if contextModeHosts[harness].workspace {
		flags = append(flags, "--workspace", workspacePlaceholder)
	}
	return flags
}

// ContextModeSpawnFor is the context-mode MCP spawn written for a harness.
func ContextModeSpawnFor(harness string) McpSpawn {
	return withContextModeFlags(PickMcpSpawn("context-mode"), ContextModeHostFlags(harness))
}

// McpSpawnForAgent is McpSpawnFor with the harness known; only context-mode differs per harness.
func McpSpawnForAgent(harness, toolID string) McpSpawn {
	if toolID == "context-mode" {
		return ContextModeSpawnFor(harness)
	}
	return PickMcpSpawn(toolID)
}

// withContextModeFlags inserts host flags after `run-mcp --context-mode`.
func withContextModeFlags(s McpSpawn, flags []string) McpSpawn {
	if len(s.Args) < 2 {
		return s
	}
	args := append([]string{}, s.Args[:2]...)
	args = append(args, flags...)
	args = append(args, s.Args[2:]...)
	return McpSpawn{Command: s.Command, Args: args}
}

// SplitContextModeArgs parses `run-mcp --context-mode [host flags] target...`.
// flags holds the consumed host flags (empty for entries written before they existed).
func SplitContextModeArgs(args []string) (flags, target []string, ok bool) {
	if len(args) < 3 || args[0] != "run-mcp" || args[1] != "--context-mode" {
		return nil, nil, false
	}
	i := 2
	for i+1 < len(args) && (args[i] == CtxAgentFlag || args[i] == "--workspace") {
		i += 2
	}
	if i >= len(args) {
		return nil, nil, false
	}
	return args[2:i], args[i:], true
}

// StripContextModeFlags drops the host flags, giving the pre-flag entry shape.
func StripContextModeFlags(args []string) []string {
	flags, target, ok := SplitContextModeArgs(args)
	if !ok || len(flags) == 0 {
		return args
	}
	return append(append([]string{}, args[:2]...), target...)
}

// ContextModeFlagsCurrent reports whether args carry exactly the host flags for harness.
func ContextModeFlagsCurrent(harness string, args []string) bool {
	flags, _, ok := SplitContextModeArgs(args)
	return ok && stringSlicesEq(flags, ContextModeHostFlags(harness))
}

func stringSlicesEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SameModuloContextModeFlags reports whether two full commands (binary first) match
// once context-mode host flags are ignored; lets ledgers accept old-shape entries.
func SameModuloContextModeFlags(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 || a[0] != b[0] {
		return false
	}
	return stringSlicesEq(StripContextModeFlags(a[1:]), StripContextModeFlags(b[1:]))
}
