package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func McpCommand(agentID, toolID string) []string {
	spawn := util.McpSpawnFor(toolID)
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex(agentID, util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	}
	return append([]string{spawn.Command}, spawn.Args...)
}

func ConfigureAmpMcp(toolID string) (bool, string) {
	p := util.AmpPathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.Config)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if strings.TrimSpace(raw) != "" {
			return false, p.Config
		}
		cfg = util.NewOrderedMap()
	}
	servers, ok := mapChild(cfg, "amp.mcpServers")
	if !ok {
		if _, exists := cfg.Get("amp.mcpServers"); exists {
			return false, p.Config
		}
		servers = util.NewOrderedMap()
		cfg.Set("amp.mcpServers", servers)
	}
	cmd := McpCommand("amp", toolID)
	entry := util.NewOrderedMap()
	entry.Set("type", "stdio")
	entry.Set("command", cmd[0])
	entry.Set("args", cmd[1:])
	entry.Set("disabled", false)
	if old, ok := servers.Get(toolID); ok && util.StringifyJSON(old) == util.StringifyJSON(entry) {
		return false, p.Config
	}
	servers.Set(toolID, entry)
	if util.StringifyJSON(cfg) == raw {
		return false, p.Config
	}
	return util.WriteFile(p.Config, util.StringifyJSON(cfg)) == nil, p.Config
}

func ampMcpHas(toolID string, command []string) bool {
	raw, ok := util.ReadFileSafe(util.AmpPathsResolved().Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	servers, ok := mapChild(cfg, "amp.mcpServers")
	if !ok {
		return false
	}
	v, ok := servers.Get(toolID)
	if !ok {
		return false
	}
	m, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	typ, _ := m.Get("type")
	got, _ := m.Get("command")
	args, _ := m.Get("args")
	disabled, _ := m.Get("disabled")
	return typ == "stdio" && got == command[0] && disabled != true && argsEq(args, command[1:])
}

func removeAmpMcp(toolID string) bool {
	f := util.AmpPathsResolved().Config
	raw, ok := util.ReadFileSafe(f)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	servers, ok := mapChild(cfg, "amp.mcpServers")
	if !ok {
		return false
	}
	if _, ok = servers.Get(toolID); !ok {
		return false
	}
	servers.Delete(toolID)
	return util.WriteFile(f, util.StringifyJSON(cfg)) == nil
}

// newMcpRequest applies options and defaults the command to the agent's canonical spawn.
func newMcpRequest(agentID, toolID string, opts ...core.McpOption) core.McpRequest {
	req := core.McpRequest{ToolID: toolID}
	for _, opt := range opts {
		opt(&req)
	}
	if req.Command == nil {
		req.Command = McpCommand(agentID, toolID)
	}
	return req
}

func jsonMcpAdapter(id string, pathFn func() string, nested string) agentMcpAdapter {
	return agentMcpAdapter{id: id,
		configure: func(req core.McpRequest) core.McpReply {
			path := pathFn()
			_ = util.EnsureDir(filepath.Dir(path))
			raw, _ := util.ReadFileSafe(path)
			cfg := util.TryParseJsonc(raw)
			if cfg == nil {
				if strings.TrimSpace(raw) != "" {
					return core.McpReply{File: path, Err: fmt.Errorf("mcp %s: unparseable config %s", req.ToolID, path)}
				}
				cfg = util.NewOrderedMap()
			}
			servers, ok := nestedChild(cfg, nested, true)
			if !ok {
				return core.McpReply{File: path, Err: fmt.Errorf("mcp %s: config path %s is not an object", req.ToolID, nested)}
			}
			if current, exists := servers.Get(req.ToolID); exists && !toklessMcpEntry(current) {
				return core.McpReply{File: path, Err: fmt.Errorf("mcp %s: foreign entry present; refusing to overwrite", req.ToolID)}
			}
			entry := util.NewOrderedMap()
			entry.Set("command", req.Command[0])
			entry.Set("args", req.Command[1:])
			entry.Set("type", "stdio")
			entry.Set("enable", true)
			servers.Set(req.ToolID, entry)
			return core.McpReply{Changed: util.WriteFile(path, util.StringifyJSON(cfg)) == nil, File: path}
		},
		remove: func(req core.McpRequest) core.McpReply {
			path := pathFn()
			raw, _ := util.ReadFileSafe(path)
			cfg := util.TryParseJsonc(raw)
			if cfg == nil {
				if strings.TrimSpace(raw) != "" {
					return core.McpReply{File: path, Err: fmt.Errorf("mcp %s: unparseable config %s", req.ToolID, path)}
				}
				return core.McpReply{File: path}
			}
			servers, ok := nestedChild(cfg, nested, false)
			if !ok {
				return core.McpReply{File: path}
			}
			current, exists := servers.Get(req.ToolID)
			if !exists {
				return core.McpReply{File: path}
			}
			if !toklessMcpEntry(current) {
				return core.McpReply{File: path, Err: fmt.Errorf("mcp %s: foreign entry present; refusing to remove", req.ToolID)}
			}
			servers.Delete(req.ToolID)
			err := util.WriteFile(path, util.StringifyJSON(cfg))
			return core.McpReply{Changed: err == nil, File: path, Err: err}
		},
		has: func(req core.McpRequest) bool {
			path := pathFn()
			cfg := loadMcpJson(path)
			servers, ok := nestedChild(cfg, nested, false)
			if !ok {
				return false
			}
			v, ok := servers.Get(req.ToolID)
			if !ok {
				return false
			}
			if req.AnyShape {
				return true
			}
			m, ok := v.(*util.OrderedMap)
			if !ok {
				return false
			}
			cmd, _ := m.Get("command")
			args, _ := m.Get("args")
			typ, _ := m.Get("type")
			return cmd == req.Command[0] && argsEq(args, req.Command[1:]) && (typ == "stdio" || typ == nil)
		},
	}
}

// nestedChild resolves a possibly dotted config path (e.g. "mcp.servers") to
// its child object, creating intermediate objects when create is true. Non-dotted
// keys behave like mapChild.
func nestedChild(cfg *util.OrderedMap, path string, create bool) (*util.OrderedMap, bool) {
	cur := cfg
	parts := strings.Split(path, ".")
	for _, part := range parts {
		v, ok := cur.Get(part)
		if !ok {
			if !create {
				return nil, false
			}
			child := util.NewOrderedMap()
			cur.Set(part, child)
			cur = child
			continue
		}
		child, ok := v.(*util.OrderedMap)
		if !ok {
			return nil, false
		}
		cur = child
	}
	return cur, true
}

func toklessMcpEntry(v any) bool {
	m, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	command, ok := m.Get("command")
	if !ok {
		return false
	}
	var first string
	switch c := command.(type) {
	case string:
		first = c
	case []any:
		if len(c) == 0 {
			return false
		}
		s, ok := c[0].(string)
		if !ok {
			return false
		}
		first = s
	default:
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.ReplaceAll(first, `\`, `/`)), ".exe"))
	return base == "tokless"
}

func loadMcpJson(path string) *util.OrderedMap {
	raw, _ := util.ReadFileSafe(path)
	if cfg := util.TryParseJsonc(raw); cfg != nil {
		return cfg
	}
	return util.NewOrderedMap()
}

func dshMcpAdapter() agentMcpAdapter {
	return agentMcpAdapter{id: "dsh",
		configure: func(req core.McpRequest) core.McpReply {
			path := filepath.Join(util.Home(), ".dsh", "cordis.patch.yml")
			cmd := req.Command
			entry := fmt.Sprintf("\n- insert:\n    - id: mcp-%s\n      name: '@deepseek-ai/dsh-mcp-client'\n      config:\n        serverName: %s\n        transport: stdio\n        command: %s\n        args: %s\n", req.ToolID, req.ToolID, yamlQuote(cmd[0]), yamlStringList(cmd[1:]))
			raw, _ := util.ReadFileSafe(path)
			if strings.Contains(raw, "serverName: "+req.ToolID) {
				return core.McpReply{File: path}
			}
			if strings.Contains(raw, "id: mcp-"+req.ToolID) {
				return core.McpReply{File: path, Err: fmt.Errorf("mcp %s: foreign entry present; refusing to overwrite", req.ToolID)}
			}
			_ = util.EnsureDir(filepath.Dir(path))
			err := util.WriteFile(path, raw+entry)
			return core.McpReply{Changed: err == nil, File: path, Err: err}
		},
		remove: func(req core.McpRequest) core.McpReply {
			path := filepath.Join(util.Home(), ".dsh", "cordis.patch.yml")
			raw, _ := util.ReadFileSafe(path)
			id := "    - id: mcp-" + req.ToolID + "\n"
			idx := strings.Index(raw, id)
			if idx < 0 {
				return core.McpReply{File: path}
			}
			start := strings.LastIndex(raw[:idx], "\n- insert:")
			if start < 0 {
				if strings.HasPrefix(raw, "- insert:") {
					start = 0
				} else {
					return core.McpReply{File: path, Err: fmt.Errorf("mcp %s: refusing to remove foreign block", req.ToolID)}
				}
			} else {
				start++
			}
			lineEnd := idx + len(id)
			end := strings.Index(raw[lineEnd:], "\n- ")
			if end < 0 {
				end = len(raw)
			} else {
				end += lineEnd
			}
			block := raw[start:end]
			if !strings.Contains(block, "id: mcp-"+req.ToolID) || !strings.Contains(block, "serverName: "+req.ToolID) || !strings.Contains(block, "dsh-mcp-client") || !strings.Contains(block, "tokless") {
				return core.McpReply{File: path, Err: fmt.Errorf("mcp %s: refusing to remove foreign block", req.ToolID)}
			}
			result := raw[:start] + raw[end:]
			if end == len(raw) {
				result = strings.TrimRight(result, "\r\n") + "\n"
			}
			err := util.WriteFile(path, result)
			return core.McpReply{Changed: err == nil, File: path, Err: err}
		},
		has: func(req core.McpRequest) bool {
			path := filepath.Join(util.Home(), ".dsh", "cordis.patch.yml")
			raw, _ := util.ReadFileSafe(path)
			return strings.Contains(raw, "serverName: "+req.ToolID)
		},
	}
}

func yamlQuote(s string) string        { b, _ := json.Marshal(s); return string(b) }
func yamlStringList(a []string) string { b, _ := json.Marshal(a); return string(b) }

// agentMcpAdapter folds a set of package-level MCP functions into core.McpAdapter.
type agentMcpAdapter struct {
	id        string
	configure func(core.McpRequest) core.McpReply
	remove    func(core.McpRequest) core.McpReply
	has       func(core.McpRequest) bool
}

func (a agentMcpAdapter) ConfigureMcp(toolID string, opts ...core.McpOption) core.McpReply {
	return a.configure(newMcpRequest(a.id, toolID, opts...))
}

func (a agentMcpAdapter) RemoveMcp(toolID string, opts ...core.McpOption) core.McpReply {
	return a.remove(newMcpRequest(a.id, toolID, opts...))
}

func (a agentMcpAdapter) HasMcp(toolID string, opts ...core.McpOption) bool {
	return a.has(newMcpRequest(a.id, toolID, opts...))
}

// --- claude ---

func claudeMcpServers() (*util.OrderedMap, bool) {
	raw, ok := util.ReadFileSafe(util.ClaudeCodePaths().GlobalJSON)
	if !ok {
		return nil, false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return nil, false
	}
	return mapChild(cfg, "mcpServers")
}

func claudeMcpPresent(toolID string) bool {
	servers, ok := claudeMcpServers()
	if !ok {
		return false
	}
	_, has := servers.Get(toolID)
	return has
}

// claudeMcpHas reports whether claude.json holds the exact command for toolID.
func claudeMcpHas(toolID string, command []string) bool {
	servers, ok := claudeMcpServers()
	if !ok || len(command) == 0 {
		return false
	}
	v, has := servers.Get(toolID)
	if !has {
		return false
	}
	em, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	cmd, _ := em.Get("command")
	args, _ := em.Get("args")
	if cmd == command[0] && argsEq(args, command[1:]) {
		return true
	}
	// Accept unwrapped codegraph (pre-Phase-2 shape) in ~/.claude.json.
	// Live config may use bare "codegraph", the resolved spawn path, or its base.
	if toolID == "codegraph" {
		unwrapped := util.PickMcpSpawn("codegraph", "serve", "--mcp")
		if (cmd == "codegraph" || cmd == unwrapped.Command || cmd == filepath.Base(unwrapped.Command)) && argsEq(args, unwrapped.Args) {
			return true
		}
	}
	return false
}

// --- codex ---

func codexMcpSection(toolID string) string {
	if toolID == "context-mode" {
		return "mcp_servers.context_mode"
	}
	return "mcp_servers." + toolID
}

func codexMcpBlock(raw, toolID string) (string, bool) {
	if text, ok := util.BlockText(raw, codexMcpSection(toolID)); ok {
		return text, true
	}
	if toolID == "context-mode" {
		return util.BlockText(raw, "mcp_servers.context-mode")
	}
	return "", false
}

func codexMcpPresent(toolID string) bool {
	raw, ok := util.ReadFileSafe(util.CodexPathsResolved().Config)
	if !ok {
		return false
	}
	_, has := codexMcpBlock(raw, toolID)
	return has
}

func codexMcpHas(toolID string, command []string) bool {
	raw, ok := util.ReadFileSafe(util.CodexPathsResolved().Config)
	if !ok || len(command) == 0 {
		return false
	}
	text, has := codexMcpBlock(raw, toolID)
	if !has {
		return false
	}
	cmd, args := parseCodexMcpBlock(text)
	return cmd == command[0] && argsEq(toAnySlice(args), command[1:])
}

func removeCodexMcpBlocks(raw, toolID string) string {
	next := util.RemoveBlock(raw, codexMcpSection(toolID))
	if toolID == "context-mode" {
		next = util.RemoveBlock(next, "mcp_servers.context-mode")
	}
	return next
}

// parseCodexMcpBlock extracts the command and args fields from a TOML block.
func parseCodexMcpBlock(text string) (string, []string) {
	var command string
	var args []string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		key, value, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = stripCodexTomlComment(strings.TrimSpace(value))
		switch key {
		case "command":
			if s, err := strconv.Unquote(value); err == nil {
				command = s
			}
		case "args":
			if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
				_ = json.Unmarshal([]byte(value), &args)
			}
		}
	}
	return command, args
}

// stripCodexTomlComment drops a trailing # comment, respecting quoted strings.
func stripCodexTomlComment(value string) string {
	inBasic, inLiteral, escaped := false, false, false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if inBasic {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inBasic = false
			}
			continue
		}
		if inLiteral {
			if c == '\'' {
				inLiteral = false
			}
			continue
		}
		switch c {
		case '"':
			inBasic = true
		case '\'':
			inLiteral = true
		case '#':
			return strings.TrimRight(value[:i], " \t")
		}
	}
	return value
}

// --- opencode ---

func openCodeMcpConfig() *util.OrderedMap {
	raw, ok := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if !ok {
		return nil
	}
	return util.TryParseJsonc(raw)
}

// --- grok ---

// grokMcpBounded reports whether the tool uses tokless's bounded MCP spawn.
func grokMcpBounded(toolID string) bool {
	switch toolID {
	case "codegraph":
		return GrokMcpHas("codegraph")
	case "context-mode":
		return GrokContextModeMcpHas()
	case "projectmem":
		return GrokProjectmemMcpHas()
	}
	return false
}

// grokMcpPresent reports whether the tool has an enabled MCP block, regardless of shape.
func grokMcpPresent(toolID string) bool {
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false
	}
	_, _, enabled, ok := grokMcpFields(raw, toolID)
	return ok && enabled && !grokMcpDisabled(raw, toolID)
}

// --- adapter instances ---

var claudeMcpAdapter = agentMcpAdapter{
	id: "claude",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file := ConfigureClaudeMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		return core.McpReply{Changed: RemoveClaudeMcp(req.ToolID), File: util.ClaudeCodePaths().GlobalJSON}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			return claudeMcpPresent(req.ToolID)
		}
		return claudeMcpHas(req.ToolID, req.Command)
	},
}

var opencodeMcpAdapter = agentMcpAdapter{
	id: "opencode",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file := ConfigureOpenCodeMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		return core.McpReply{Changed: RemoveOpenCodeMcp(req.ToolID), File: util.OpenCodePathsResolved().Config}
	},
	has: func(req core.McpRequest) bool {
		cfg := openCodeMcpConfig()
		if cfg == nil {
			return false
		}
		if req.AnyShape {
			return OpenCodeMcpHas(cfg, req.ToolID)
		}
		return OpenCodeMcpMatches(cfg, req.ToolID, req.Command)
	},
}

var codexMcpAdapter = agentMcpAdapter{
	id: "codex",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file := ConfigureCodexMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		p := util.CodexPathsResolved()
		raw, ok := util.ReadFileSafe(p.Config)
		if !ok {
			return core.McpReply{File: p.Config}
		}
		next := removeCodexMcpBlocks(raw, req.ToolID)
		if next == raw {
			return core.McpReply{File: p.Config}
		}
		err := util.WriteFile(p.Config, next)
		return core.McpReply{Changed: err == nil, File: p.Config, Err: err}
	},
	has: func(req core.McpRequest) bool {
		raw, ok := util.ReadFileSafe(util.CodexPathsResolved().Config)
		if ok && !req.AnyShape && req.ToolID == "context-mode" && strings.Contains(raw, "[mcp_servers.context-mode]") {
			return false
		}
		if req.AnyShape {
			return codexMcpPresent(req.ToolID)
		}
		return codexMcpHas(req.ToolID, req.Command)
	},
}

var cursorMcpAdapter = agentMcpAdapter{
	id: "cursor",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file := ConfigureCursorMcp(req.ToolID)
		if ConfigureCursorMcpPermissions(req.ToolID) {
			changed = true
		}
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		removed := RemoveCursorMcp(req.ToolID)
		if RemoveCursorMcpPermissions(req.ToolID) {
			removed = true
		}
		return core.McpReply{Changed: removed, File: CursorMcpConfigPath()}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			return CursorMcpHas(req.ToolID)
		}
		return CursorMcpHas(req.ToolID) && HasCursorMcpPermissions(req.ToolID)
	},
}

var antigravityMcpAdapter = agentMcpAdapter{
	id: "antigravity",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file := ConfigureAntigravityMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		RemoveAntigravityMcp(req.ToolID)
		return core.McpReply{Changed: true}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			return AntigravityMcpHas(req.ToolID)
		}
		return AntigravityMcpBounded(req.ToolID)
	},
}

var copilotMcpAdapter = agentMcpAdapter{
	id: "copilot",
	configure: func(req core.McpRequest) core.McpReply {
		if req.Ide {
			changed, file, err := ConfigureCopilotIdeMcpSafe(req.ToolID)
			return core.McpReply{Changed: changed, File: file, Err: err}
		}
		cliChanged, file, cliErr := ConfigureCopilotMcpSafe(req.ToolID)
		ideChanged, ideFile, ideErr := ConfigureCopilotIdeMcpSafe(req.ToolID)
		if file == "" {
			file = ideFile
		}
		return core.McpReply{Changed: cliChanged || ideChanged, File: file, Err: errors.Join(cliErr, ideErr)}
	},
	remove: func(req core.McpRequest) core.McpReply {
		if req.Ide {
			_, err := RemoveCopilotIdeMcpSafe(req.ToolID)
			return core.McpReply{Changed: err == nil, Err: err}
		}
		cliRemoved, cliErr := RemoveCopilotMcpSafe(req.ToolID)
		ideRemoved, ideErr := RemoveCopilotIdeMcpSafe(req.ToolID)
		return core.McpReply{Changed: cliRemoved || ideRemoved, Err: errors.Join(cliErr, ideErr)}
	},
	has: func(req core.McpRequest) bool {
		if req.Ide {
			return CopilotIdeMcpHas(req.ToolID)
		}
		if req.AnyShape {
			return CopilotMcpHas(req.ToolID) && CopilotIdeMcpHas(req.ToolID)
		}
		return CopilotMcpBounded(req.ToolID, false) && CopilotMcpBounded(req.ToolID, true)
	},
}

var droidMcpAdapter = agentMcpAdapter{
	id: "droid",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file := ConfigureDroidMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		return core.McpReply{Changed: RemoveDroidMcp(req.ToolID), File: droidMcpFile()}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			return DroidMcpHas(req.ToolID)
		}
		return DroidMcpBounded(req.ToolID)
	},
}

var grokMcpAdapter = agentMcpAdapter{
	id: "grok",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file, err := ConfigureGrokMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file, Err: err}
	},
	remove: func(req core.McpRequest) core.McpReply {
		changed, err := RemoveGrokMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: grokConfigFile(), Err: err}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			return grokMcpPresent(req.ToolID)
		}
		return grokMcpBounded(req.ToolID)
	},
}

var piMcpAdapter = agentMcpAdapter{
	id: "pi",
	configure: func(req core.McpRequest) core.McpReply {
		if !PiRemoveMcpAdapter() {
			return core.McpReply{File: piMcpFile(), Err: errors.New("failed to remove pi-mcp-adapter")}
		}
		changed, file := ConfigurePiMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		return core.McpReply{Changed: RemovePiMcp(req.ToolID), File: piMcpFile()}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			return PiMcpHas(req.ToolID)
		}
		return PiMcpBounded(req.ToolID)
	},
}

var ompMcpAdapter = agentMcpAdapter{
	id: "omp",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file := ConfigureOmpMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		return core.McpReply{Changed: RemoveOmpMcp(req.ToolID), File: ompMcpFile()}
	},
	has: func(req core.McpRequest) bool {
		return OmpMcpHas(req.ToolID)
	},
}

var kiloMcpAdapter = agentMcpAdapter{
	id: "kilo",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file, err := ConfigureKiloMcpSafe(req.ToolID, req.Command)
		return core.McpReply{Changed: changed, File: file, Err: err}
	},
	remove: func(req core.McpRequest) core.McpReply {
		return core.McpReply{Changed: RemoveKiloMcp(req.ToolID), File: util.KiloPathsResolved().Config}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			return KiloMcpConfigured(req.ToolID)
		}
		return KiloMcpMatches(req.ToolID, req.Command)
	},
}

var clineMcpAdapter = agentMcpAdapter{
	id: "cline",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file, err := ConfigureClineMcpSafe(req.ToolID, req.Command)
		return core.McpReply{Changed: changed, File: file, Err: err}
	},
	remove: func(req core.McpRequest) core.McpReply {
		return core.McpReply{Changed: RemoveClineMcp(req.ToolID), File: util.ClinePathsResolved().McpConfig}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			return ClineMcpConfigured(req.ToolID)
		}
		return ClineMcpMatches(req.ToolID, req.Command)
	},
}

var ampMcpAdapter = agentMcpAdapter{
	id: "amp",
	configure: func(req core.McpRequest) core.McpReply {
		changed, file := ConfigureAmpMcp(req.ToolID)
		return core.McpReply{Changed: changed, File: file}
	},
	remove: func(req core.McpRequest) core.McpReply {
		return core.McpReply{Changed: removeAmpMcp(req.ToolID), File: util.AmpPathsResolved().Config}
	},
	has: func(req core.McpRequest) bool {
		if req.AnyShape {
			raw, _ := util.ReadFileSafe(util.AmpPathsResolved().Config)
			cfg := util.TryParseJsonc(raw)
			if cfg == nil {
				return false
			}
			m, ok := mapChild(cfg, "amp.mcpServers")
			if !ok {
				return false
			}
			_, ok = m.Get(req.ToolID)
			return ok
		}
		return ampMcpHas(req.ToolID, req.Command)
	},
}

var kimiMcpAdapter = jsonMcpAdapter("kimi", func() string { return util.KimiPathsResolved().McpConfig }, "mcpServers")
var zcodeMcpAdapter = jsonMcpAdapter("zcode", func() string { return util.ZcodePathsResolved().McpConfig }, "mcp.servers")
var dshMcpAdapterValue = dshMcpAdapter()

// agentMcpAdapters maps every agent ID to its MCP adapter, independent of
// registry state so tool dispatch works before agents.Register runs.
var agentMcpAdapters = map[string]core.McpAdapter{
	"amp":         ampMcpAdapter,
	"kimi":        kimiMcpAdapter,
	"dsh":         dshMcpAdapterValue,
	"zcode":       zcodeMcpAdapter,
	"claude":      claudeMcpAdapter,
	"opencode":    opencodeMcpAdapter,
	"codex":       codexMcpAdapter,
	"cursor":      cursorMcpAdapter,
	"antigravity": antigravityMcpAdapter,
	"copilot":     copilotMcpAdapter,
	"droid":       droidMcpAdapter,
	"grok":        grokMcpAdapter,
	"pi":          piMcpAdapter,
	"omp":         ompMcpAdapter,
	"kilo":        kiloMcpAdapter,
	"cline":       clineMcpAdapter,
}

// McpAdapterFor returns the MCP adapter for an agent ID, or nil when unknown.
func McpAdapterFor(id string) core.McpAdapter { return agentMcpAdapters[id] }

func init() {
	amp.Mcp = ampMcpAdapter
	kimi.Mcp = kimiMcpAdapter
	dsh.Mcp = dshMcpAdapterValue
	zcode.Mcp = zcodeMcpAdapter
	claude.Mcp = claudeMcpAdapter
	opencode.Mcp = opencodeMcpAdapter
	codex.Mcp = codexMcpAdapter
	cursor.Mcp = cursorMcpAdapter
	antigravity.Mcp = antigravityMcpAdapter
	copilot.Mcp = copilotMcpAdapter
	droid.Mcp = droidMcpAdapter
	grok.Mcp = grokMcpAdapter
	pi.Mcp = piMcpAdapter
	omp.Mcp = ompMcpAdapter
	kilo.Mcp = kiloMcpAdapter
	cline.Mcp = clineMcpAdapter
}
