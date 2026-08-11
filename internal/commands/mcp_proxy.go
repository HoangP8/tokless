package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// runMcpProxy spawns the MCP server as a child and proxies stdio.
func runMcpProxy(agent, path string, argv, env []string, tool string) int {
	return runMcpProxyAfterStart(agent, path, argv, env, tool, nil)
}

func runMcpProxyIO(agent, path string, argv, env []string, input io.Reader, output, stderr io.Writer, tool string) int {
	return runMcpProxyIOAfterStart(agent, path, argv, env, input, output, stderr, tool, nil)
}

func runMcpProxyAfterStart(agent, path string, argv, env []string, tool string, afterStart func()) int {
	return runMcpProxyIOAfterStart(agent, path, argv, env, os.Stdin, os.Stdout, os.Stderr, tool, afterStart)
}

func runMcpProxyIOAfterStart(agent, path string, argv, env []string, input io.Reader, output, stderr io.Writer, tool string, afterStart func()) int {
	exe, args := resolveMcpCommand(path, argv)
	cmd := exec.Command(exe, args...)
	cmd.Env = mcpChildEnv(env)
	cmd.Stderr = stderr
	if allowed := mcpToolPolicies[tool]; len(allowed) > 0 {
		if afterStart != nil {
		}
		return runBoundedMcpProxy(cmd, input, output, allowed)
	}
	cmd.Stdin = input
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cmd.Stdout = output
		_ = cmd.Start()
		return waitExit(cmd)
	}
	if err := cmd.Start(); err != nil {
		return 1
	}
	if afterStart != nil {
		afterStart()
	}
	io.Copy(output, stdout)
	return waitExit(cmd)
}

var contextModeTools = []string{
	"ctx_batch_execute",
	"ctx_execute",
	"ctx_execute_file",
	"ctx_index",
	"ctx_search",
	"ctx_fetch_and_index",
}

var mcpToolPolicies = map[string][]string{
	"context-mode": contextModeTools,
	"headroom":     {"headroom_compress", "headroom_retrieve"},
}

// runBoundedMcpProxy forwards MCP traffic unchanged except an explicit tool allowlist.
func runBoundedMcpProxy(cmd *exec.Cmd, input io.Reader, output io.Writer, allowed []string) int {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 1
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 1
	}
	if err := cmd.Start(); err != nil {
		return 1
	}
	listIDs := &mcpRequestIDs{ids: map[string]bool{}}
	lockedOutput := &mcpLockedWriter{writer: output}
	inputDone := make(chan error, 1)
	go func() {
		defer stdin.Close()
		inputDone <- scanMcpInput(input, stdin, lockedOutput, listIDs, allowed)
	}()
	outputErr := scanMcpOutput(stdout, lockedOutput, listIDs, allowed)
	_ = stdin.Close() // Upstream EOF must not wait for a client that keeps stdin open.
	code := waitExit(cmd)
	if outputErr != nil || code != 0 {
		return 1
	}
	select {
	case err := <-inputDone:
		if err != nil {
			return 1
		}
	default:
	}
	return 0
}

type mcpRequestIDs struct {
	mu  sync.RWMutex
	ids map[string]bool
}

type mcpLockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *mcpLockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

func (w *mcpLockedWriter) writeMCPMessage(framing mcpFraming, message []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return writeMCPMessageUnlocked(w.writer, framing, message)
}

const maxMCPMessageSize = 64 << 20

type mcpFraming int

const (
	mcpNDJSON mcpFraming = iota
	mcpContentLength
)

func scanMcpInput(input io.Reader, upstream, output io.Writer, listIDs *mcpRequestIDs, allowed []string) error {
	reader := bufio.NewReader(input)
	for {
		line, framing, err := readMCPMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("[")) {
			if err := writeMCPMessage(output, framing, mcpInvalidRequestResponse()); err != nil {
				return err
			}
			continue
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(line, &request) == nil && request.Method == "tools/list" {
			if id, ok := canonicalMCPID(request.ID); ok {
				listIDs.mu.Lock()
				listIDs.ids[id] = true
				listIDs.mu.Unlock()
			}
		}
		if request.Method == "tools/call" && !isAllowedMcpTool(request.Params.Name, allowed) {
			if err := writeMCPMessage(output, framing, mcpToolDeniedResponse(request.ID, request.Params.Name)); err != nil {
				return err
			}
			continue
		}
		if err := writeMCPMessage(upstream, framing, line); err != nil {
			return err
		}
	}
}

func scanMcpOutput(upstream io.Reader, output io.Writer, listIDs *mcpRequestIDs, allowed []string) error {
	reader := bufio.NewReader(upstream)
	for {
		line, framing, err := readMCPMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if filterMcpTools(line, listIDs) {
			line = filterMcpToolsResponse(line, allowed)
		}
		if err := writeMCPMessage(output, framing, line); err != nil {
			return err
		}
	}
}

func filterMcpTools(line []byte, listIDs *mcpRequestIDs) bool {
	var response map[string]json.RawMessage
	if json.Unmarshal(line, &response) != nil {
		return false
	}
	if _, hasMethod := response["method"]; hasMethod {
		return false
	}
	if _, hasResult := response["result"]; !hasResult {
		if _, hasError := response["error"]; !hasError {
			return false
		}
	}
	id, valid := canonicalMCPID(response["id"])
	if !valid {
		return false
	}
	listIDs.mu.Lock()
	ok := listIDs.ids[id]
	if ok {
		delete(listIDs.ids, id)
	}
	listIDs.mu.Unlock()
	return ok
}

func canonicalMCPID(id json.RawMessage) (string, bool) {
	if len(id) == 0 || bytes.Equal(bytes.TrimSpace(id), []byte("null")) || !json.Valid(id) {
		return "", false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	switch value := value.(type) {
	case string:
		return "string:" + value, true
	case json.Number:
		return canonicalMCPNumber(value.String())
	default:
		return "", false
	}
}

// canonicalMCPNumber returns a decimal coefficient and base-10 exponent.
func canonicalMCPNumber(number string) (string, bool) {
	sign := ""
	if number[0] == '-' {
		sign, number = "-", number[1:]
	}
	exponent := 0
	if index := strings.IndexAny(number, "eE"); index >= 0 {
		parsed, err := strconv.Atoi(number[index+1:])
		if err != nil {
			return "", false
		}
		exponent, number = parsed, number[:index]
	}
	if index := strings.IndexByte(number, '.'); index >= 0 {
		exponent -= len(number) - index - 1
		number = number[:index] + number[index+1:]
	}
	number = strings.TrimLeft(number, "0")
	if number == "" {
		return "number:0", true
	}
	for strings.HasSuffix(number, "0") {
		number = number[:len(number)-1]
		exponent++
	}
	return "number:" + sign + number + "e" + strconv.Itoa(exponent), true
}

func isAllowedMcpTool(name string, allowedTools []string) bool {
	for _, allowed := range allowedTools {
		if name == allowed {
			return true
		}
	}
	return false
}

func mcpToolDeniedResponse(id json.RawMessage, name string) []byte {
	responseID := json.RawMessage("null")
	if _, ok := canonicalMCPID(id); ok {
		responseID = id
	}
	response, _ := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{JSONRPC: "2.0", ID: responseID, Error: struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{Code: -32601, Message: fmt.Sprintf("tool %q is not available", name)}})
	return response
}

func mcpInvalidRequestResponse() []byte {
	return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"JSON-RPC batches are not supported"}}`)
}

func readMCPMessage(reader *bufio.Reader) ([]byte, mcpFraming, error) {
	line, err := readMCPLine(reader)
	if err != nil {
		return nil, mcpNDJSON, err
	}
	if !strings.EqualFold(strings.SplitN(string(line), ":", 2)[0], "Content-Length") {
		return line, mcpNDJSON, nil
	}
	parts := strings.SplitN(string(line), ":", 2)
	if len(parts) != 2 {
		return nil, mcpContentLength, fmt.Errorf("invalid Content-Length header")
	}
	length, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || length < 0 || length > maxMCPMessageSize {
		return nil, mcpContentLength, fmt.Errorf("invalid Content-Length")
	}
	for {
		header, err := readMCPLine(reader)
		if err != nil {
			return nil, mcpContentLength, err
		}
		if len(header) == 0 {
			break
		}
	}
	message := make([]byte, length)
	if _, err := io.ReadFull(reader, message); err != nil {
		return nil, mcpContentLength, err
	}
	return message, mcpContentLength, nil
}

func readMCPLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > maxMCPMessageSize+1 {
			return nil, fmt.Errorf("MCP message exceeds %d bytes", maxMCPMessageSize)
		}
		line = append(line, part...)
		if err == nil {
			break
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
	line = bytes.TrimSuffix(line, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	if len(line) > maxMCPMessageSize {
		return nil, fmt.Errorf("MCP message exceeds %d bytes", maxMCPMessageSize)
	}
	return line, nil
}

func writeMCPMessage(writer io.Writer, framing mcpFraming, message []byte) error {
	if len(message) > maxMCPMessageSize {
		return fmt.Errorf("MCP message exceeds %d bytes", maxMCPMessageSize)
	}
	if writer, ok := writer.(*mcpLockedWriter); ok {
		return writer.writeMCPMessage(framing, message)
	}
	return writeMCPMessageUnlocked(writer, framing, message)
}

func writeMCPMessageUnlocked(writer io.Writer, framing mcpFraming, message []byte) error {
	if framing == mcpContentLength {
		_, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(message))
		if err != nil {
			return err
		}
		_, err = writer.Write(message)
		return err
	}
	_, err := writer.Write(append(append([]byte(nil), message...), '\n'))
	return err
}

func filterMcpToolsResponse(line []byte, allowedTools []string) []byte {
	var response map[string]json.RawMessage
	if json.Unmarshal(line, &response) != nil {
		return line
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(response["result"], &result) != nil {
		return line
	}
	var tools []json.RawMessage
	if json.Unmarshal(result["tools"], &tools) != nil {
		return line
	}
	byName := make(map[string]json.RawMessage, len(tools))
	for _, tool := range tools {
		var item struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(tool, &item) == nil {
			byName[item.Name] = tool
		}
	}
	filtered := make([]json.RawMessage, 0, len(allowedTools))
	for _, name := range allowedTools {
		if tool, ok := byName[name]; ok {
			filtered = append(filtered, tool)
		}
	}
	result["tools"], _ = json.Marshal(filtered)
	response["result"], _ = json.Marshal(result)
	filteredResponse, err := json.Marshal(response)
	if err != nil {
		return line
	}
	return filteredResponse
}

func mcpChildEnv(env []string) []string {
	return env
}

// --- non-antigravity pass-through ---

func resolveMcpCommand(path string, argv []string) (string, []string) {
	if isNodeShebangScript(path) {
		if nodePath, err := exec.LookPath("node"); err == nil {
			return nodePath, append([]string{path}, argv[1:]...)
		}
	}
	return path, normalizedCmdBatchArgs(path, argv[1:], runtime.GOOS == "windows")
}

func normalizedCmdBatchArgs(command string, args []string, windows bool) []string {
	out := append([]string(nil), args...)
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/")))
	if !windows || (base != "cmd" && base != "cmd.exe") || len(out) < 2 || !strings.EqualFold(out[0], "/c") {
		return out
	}
	ext := strings.ToLower(filepath.Ext(strings.ReplaceAll(out[1], "\\", "/")))
	if ext == ".cmd" || ext == ".bat" {
		out[1] = strings.ReplaceAll(out[1], "/", "\\")
	}
	return out
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
