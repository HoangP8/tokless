package util

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// McpSpawn is the command shape written into an agent's MCP config entry.
type McpSpawn struct {
	Command string
	Args    []string
}

var pkgForBin = map[string]string{
	"context-mode": "context-mode",
	"codegraph":    "@colbymchenry/codegraph",
}

// PickMcpSpawn prefers a real binary on PATH, else falls back to npx --no-install.
func PickMcpSpawn(bin string, extraArgs ...string) McpSpawn {
	if extraArgs == nil {
		extraArgs = []string{}
	}
	if bin == "codegraph" {
		if spawn, ok := PickCodegraphSpawn(extraArgs...); ok {
			return spawn
		}
		return McpSpawn{}
	} else if p := Which(bin); p != "" {
		return wrapCmdShim(McpSpawn{Command: spawnCommand(bin, p), Args: extraArgs})
	}
	pkg, ok := pkgForBin[bin]
	if !ok {
		pkg = bin
	}
	args := append([]string{"--no-install", pkg}, extraArgs...)
	cmd := "npx"
	if p := Which("npx"); p != "" {
		cmd = spawnCommand("npx", p)
	}
	return wrapCmdShim(McpSpawn{Command: cmd, Args: args})
}

// PickCodegraphSpawn returns only a spawn that serves the expected MCP tool.
func PickCodegraphSpawn(extraArgs ...string) (McpSpawn, bool) {
	if p := ResolveCodegraphBin(); p != "" {
		return wrapCmdShim(McpSpawn{Command: spawnCommand("codegraph", p), Args: extraArgs}), true
	}
	return McpSpawn{}, false
}

// spawnCommand picks what goes into the config.
func spawnCommand(bin, resolved string) string {
	if resolved != "" {
		return resolved
	}
	return bin
}

func wrapCmdShim(s McpSpawn) McpSpawn {
	if !IsWin {
		return s
	}
	p := s.Command
	if !filepath.IsAbs(p) {
		p = Which(s.Command)
	}
	ext := strings.ToLower(filepath.Ext(p))
	if ext != ".cmd" && ext != ".bat" {
		return s
	}
	return McpSpawn{Command: "cmd", Args: append([]string{"/c", p}, s.Args...)}
}

var semverLike = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

type codegraphHealthEntry struct {
	size int64
	mod  int64
	ok   bool
}

var (
	codegraphHealthMu sync.Mutex
	codegraphHealth   = map[string]codegraphHealthEntry{}
)

func codegraphProbeCommand(p string) (string, []string) {
	if IsWin {
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".cmd" || ext == ".bat" {
			return "cmd", []string{"/c", p}
		}
	}
	return p, nil
}

func CodegraphBinaryHealthy(p string) bool {
	info, err := os.Stat(p)
	if err == nil {
		codegraphHealthMu.Lock()
		if cached, ok := codegraphHealth[p]; ok && cached.size == info.Size() && cached.mod == info.ModTime().UnixNano() {
			codegraphHealthMu.Unlock()
			return cached.ok
		}
		codegraphHealthMu.Unlock()
	}
	cmd, args := codegraphProbeCommand(p)
	ok := CodegraphSpawnHealthy(cmd, args)
	if err == nil {
		codegraphHealthMu.Lock()
		codegraphHealth[p] = codegraphHealthEntry{size: info.Size(), mod: info.ModTime().UnixNano(), ok: ok}
		codegraphHealthMu.Unlock()
	}
	return ok
}

func CodegraphSpawnHealthy(command string, prefixArgs []string) bool {
	if command == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	version := Run(command, append(append([]string{}, prefixArgs...), "--version"), RunOptions{Capture: true, Ctx: ctx})
	if version.Code != 0 || !semverLike.MatchString(version.Stdout) {
		return false
	}
	return codegraphMcpToolsHealthy(command, prefixArgs)
}

func codegraphMcpToolsHealthy(command string, prefixArgs []string) bool {
	return codegraphMcpToolsHealthyMode(command, prefixArgs, false) || codegraphMcpToolsHealthyMode(command, prefixArgs, true)
}

func codegraphMcpToolsHealthyMode(command string, prefixArgs []string, framed bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	args := append(append([]string{}, prefixArgs...), "serve", "--mcp")
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return false
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	requests := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "tokless", "version": "0"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
	}
	for _, req := range requests {
		if err := writeMcpProbeRequest(stdin, req, framed); err != nil {
			return false
		}
	}

	rd := bufio.NewReader(stdout)
	for {
		msg, err := readMcpProbeResponse(rd, framed)
		if err != nil {
			return false
		}
		var resp struct {
			ID     int `json:"id"`
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if json.Unmarshal(msg, &resp) != nil || resp.ID != 2 {
			continue
		}
		for _, tool := range resp.Result.Tools {
			if tool.Name == "codegraph_explore" {
				return true
			}
		}
		return false
	}
	return false
}

func writeMcpProbeRequest(w io.Writer, req map[string]any, framed bool) error {
	b, _ := json.Marshal(req)
	if framed {
		_, err := w.Write(append([]byte("Content-Length: "+strconv.Itoa(len(b))+"\r\n\r\n"), b...))
		return err
	}
	_, err := w.Write(append(b, '\n'))
	return err
}

func readMcpProbeResponse(rd *bufio.Reader, framed bool) ([]byte, error) {
	if !framed {
		return rd.ReadBytes('\n')
	}
	length := 0
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		k, v, ok := strings.Cut(trimmed, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			length, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if length <= 0 {
		return nil, io.ErrUnexpectedEOF
	}
	buf := make([]byte, length)
	_, err := io.ReadFull(rd, buf)
	return buf, err
}

// WrapAutoIndex routes an MCP launch through `tokless run-mcp --agent <id>` so
// the per-project index is built/checked before the real server starts.
func WrapAutoIndex(agent string, s McpSpawn) McpSpawn {
	self, err := os.Executable()
	if err != nil {
		return s
	}
	args := append([]string{"run-mcp", "--agent", agent, s.Command}, s.Args...)
	return McpSpawn{Command: self, Args: args}
}
