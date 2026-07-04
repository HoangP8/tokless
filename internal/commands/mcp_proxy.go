package commands

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// runMcpProxy spawns the MCP server as a child and proxies stdio.
func runMcpProxy(agent, path string, argv, env []string) int {
	exe, args := resolveMcpCommand(path, argv)
	if agent == "antigravity" {
		return runAgyMcpBridge(exe, args, env)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cmd.Stdout = os.Stdout
		_ = cmd.Start()
		return waitExit(cmd)
	}
	if err := cmd.Start(); err != nil {
		return 1
	}
	proxyMcpStdout(stdout, os.Stdout)
	return waitExit(cmd)
}

// --- antigravity MCP bridge (custom server, one-shot codegraph) ---

func runAgyMcpBridge(exe string, baseArgs []string, env []string) int {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		req := scanner.Bytes()
		resp := handleAgyMcpMessage(req, exe, baseArgs, env)
		if resp != nil {
			os.Stdout.Write(resp)
			os.Stdout.Write([]byte("\n"))
		}
	}
	return 0
}

func handleAgyMcpMessage(req []byte, exe string, baseArgs []string, env []string) []byte {
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *int            `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(req, &msg); err != nil || msg.JSONRPC != "2.0" {
		return nil
	}
	switch msg.Method {
	case "initialize":
		return agyInit(msg.ID)
	case "notifications/initialized":
		return nil
	case "tools/list":
		return agyToolsList(msg.ID)
	case "tools/call":
		return agyToolsCall(msg.ID, msg.Params, exe, baseArgs, env)
	}
	if msg.ID != nil {
		return jsonLine(map[string]interface{}{"jsonrpc": "2.0", "id": *msg.ID, "result": map[string]interface{}{}})
	}
	return nil
}

func agyInit(id *int) []byte {
	if id == nil {
		return nil
	}
	return jsonLine(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      *id,
		"result": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "codegraph", "version": "1.2.0"},
		},
	})
}

var agyToolDef = map[string]interface{}{
	"name":        "codegraph_explore",
	"description": "PRIMARY TOOL — call FIRST for almost any question OR before an edit: how does X work, architecture, a bug, where/what is X, surveying an area, or the symbols you are about to change. Returns the verbatim source of the relevant symbols grouped by file in ONE capped call (Read-equivalent — treat the shown source as already Read; do NOT re-open those files), plus the call path among them. Query can be a natural-language question OR a bag of symbol/file names. Usually the ONLY call you need — more accurate context, in far fewer tokens and round-trips than a search/Read/Grep loop.",
	"inputSchema": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Symbol names, file names, or short code terms to explore (e.g., \"AuthService loginUser session-manager\", \"GraphTraverser BFS impact traversal.ts\"). For a flow question, name the symbols spanning the flow (e.g. \"mutateElement renderScene\"). A natural-language question works too — no prior codegraph_search needed.",
			},
			"maxFiles": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of files to include source code from (default: 12)",
				"default":     12,
			},
			"projectPath": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the project to query (or any directory inside it) — codegraph uses the nearest .codegraph/ index at or above that path. Omit to use this session's default project.",
			},
		},
		"required": []string{"query"},
	},
}

func agyToolsList(id *int) []byte {
	if id == nil {
		return nil
	}
	return jsonLine(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      *id,
		"result":  map[string]interface{}{"tools": []interface{}{agyToolDef}},
	})
}

func agyToolsCall(id *int, params json.RawMessage, exe string, baseArgs []string, env []string) []byte {
	if id == nil {
		return nil
	}
	var call struct {
		Name      string `json:"name"`
		Arguments struct {
			Query       string  `json:"query"`
			MaxFiles    float64 `json:"maxFiles"`
			ProjectPath string  `json:"projectPath"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil || call.Name != "codegraph_explore" || call.Arguments.Query == "" {
		return agyError(id, -32602, "Invalid params")
	}

	mf := int(call.Arguments.MaxFiles)
	text, err := runCodegraphQuery(exe, baseArgs, env, call.Arguments.Query, mf, call.Arguments.ProjectPath)
	if err != nil {
		return agyError(id, -32603, err.Error())
	}
	return jsonLine(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      *id,
		"result": map[string]interface{}{
			"content": []interface{}{map[string]interface{}{"type": "text", "text": text}},
		},
	})
}

func runCodegraphQuery(exe string, baseArgs []string, env []string, query string, maxFiles int, projectPath string) (string, error) {
	args := map[string]interface{}{"query": query}
	if maxFiles > 0 {
		args["maxFiles"] = maxFiles
	}
	if projectPath != "" {
		args["projectPath"] = projectPath
	}
	tc, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]interface{}{"name": "codegraph_explore", "arguments": args},
	})

	cmd := exec.Command(exe, baseArgs...)
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", err
	}

	stdin.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":0,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\",\"clientInfo\":{\"name\":\"tokless\"},\"capabilities\":{\"tools\":{}}}}\n"))
	stdin.Write([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n"))
	stdin.Write(tc)
	stdin.Write([]byte("\n"))
	time.Sleep(500 * time.Millisecond)
	stdin.Close()

	var out strings.Builder
	buf := make([]byte, 64*1024)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}
	cmd.Wait()

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var probe struct{ ID int `json:"id"` }
		if json.Unmarshal([]byte(lines[i]), &probe) == nil && probe.ID == 1 {
			var resp struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(lines[i]), &resp) == nil && len(resp.Result.Content) > 0 {
				return resp.Result.Content[0].Text, nil
			}
		}
	}
	return out.String(), nil
}

func agyError(id *int, code int, message string) []byte {
	return jsonLine(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      *id,
		"error":   map[string]interface{}{"code": code, "message": message},
	})
}

func jsonLine(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// --- non-antigravity pass-through ---

func resolveMcpCommand(path string, argv []string) (string, []string) {
	if isNodeShebangScript(path) {
		if nodePath, err := exec.LookPath("node"); err == nil {
			return nodePath, append([]string{path}, argv[1:]...)
		}
	}
	return path, argv[1:]
}

func isNodeShebangScript(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 32)
	n, _ := f.Read(buf)
	return strings.HasPrefix(string(buf[:n]), "#!/usr/bin/env node")
}

func waitExit(cmd *exec.Cmd) int {
	err := cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}

func proxyMcpStdout(src io.Reader, dst io.Writer) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		dst.Write(line)
		dst.Write([]byte("\n"))
	}
}
