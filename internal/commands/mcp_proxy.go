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
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/tools"
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
		return runBoundedMcpProxy(cmd, input, output, allowed, mcpInitializeInstructions(tool), afterStart)
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

var projectmemTools = agents.ProjectmemMcpToolNames

var mcpToolPolicies = map[string][]string{
	"context-mode": contextModeTools,
	"projectmem":   projectmemTools,
}

// mcpInitializeInstructions returns the instructions override for a bounded proxy tool.
func mcpInitializeInstructions(tool string) func() string {
	switch tool {
	case "projectmem":
		return func() string { return tools.ProjectmemMcpInstructions }
	case "context-mode":
		return func() string { return "" }
	default:
		return nil
	}
}

// runBoundedMcpProxy forwards MCP traffic unchanged except an explicit tool allowlist.
func runBoundedMcpProxy(cmd *exec.Cmd, input io.Reader, output io.Writer, allowed []string, instructions func() string, afterStart func()) int {
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
	if afterStart != nil {
		afterStart()
	}
	listIDs := &mcpRequestIDs{}
	lockedOutput := &mcpLockedWriter{writer: output}
	inputDone := make(chan error, 1)
	go func() {
		defer stdin.Close()
		inputDone <- scanMcpInput(input, stdin, lockedOutput, listIDs, allowed)
	}()
	outputErr := scanMcpOutput(stdout, lockedOutput, listIDs, allowed, instructions)
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
	mu      sync.RWMutex
	initIDs map[string]bool
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
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("[")) {
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
		if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(line, &request) != nil {
			if rawID := mcpResponseID(line); rawID != nil {
				if err := writeMCPMessage(output, framing, mcpParseErrorResponse(rawID)); err != nil {
					return err
				}
			}
			continue
		}
		if id, ok := canonicalMCPID(request.ID); ok {
			if request.Method == "initialize" {
				listIDs.mu.Lock()
				if listIDs.initIDs == nil {
					listIDs.initIDs = map[string]bool{}
				}
				listIDs.initIDs[id] = true
				listIDs.mu.Unlock()
			}
		}
		if request.Method == "tools/call" && !isAllowedMcpTool(request.Params.Name, allowed) {
			if _, ok := canonicalMCPID(request.ID); ok {
				if err := writeMCPMessage(output, framing, mcpToolDeniedResponse(request.ID, request.Params.Name)); err != nil {
					return err
				}
			}
			continue
		}
		if err := writeMCPMessage(upstream, framing, line); err != nil {
			return err
		}
	}
}

// isValidMCPMessageObject returns true only for a single well-formed JSON object.
func isValidMCPMessageObject(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var obj map[string]json.RawMessage
	return json.Unmarshal(line, &obj) == nil
}

func scanMcpOutput(upstream io.Reader, output io.Writer, listIDs *mcpRequestIDs, allowed []string, instructions func() string) error {
	reader := bufio.NewReader(upstream)
	for {
		line, framing, err := readMCPMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if !isValidMCPMessageObject(line) {
			continue
		}
		if filterMcpTools(line) {
			line = filterMcpToolsResponse(line, allowed)
		}
		if instructions != nil {
			if id, ok := canonicalMCPID(mcpResponseID(line)); ok && mcpInitIDMatches(listIDs, id) {
				if rewritten, ok := rewriteMcpInstructions(line, instructions()); ok {
					line = rewritten
				}
			}
		}
		if err := writeMCPMessage(output, framing, line); err != nil {
			return err
		}
	}
}

func mcpResponseID(line []byte) json.RawMessage {
	var response struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(line, &response) != nil {
		return nil
	}
	return response.ID
}

func mcpInitIDMatches(listIDs *mcpRequestIDs, id string) bool {
	listIDs.mu.RLock()
	defer listIDs.mu.RUnlock()
	return listIDs.initIDs[id]
}

func rewriteMcpInstructions(line []byte, instructions string) ([]byte, bool) {
	var response map[string]json.RawMessage
	if json.Unmarshal(line, &response) != nil {
		return nil, false
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(response["result"], &result) != nil {
		return nil, false
	}
	// Only initialize results carry protocolVersion; guards null results and id reuse.
	if _, ok := result["protocolVersion"]; !ok {
		return nil, false
	}
	encoded, err := json.Marshal(instructions)
	if err != nil {
		return nil, false
	}
	result["instructions"] = encoded
	response["result"], _ = json.Marshal(result)
	out, err := json.Marshal(response)
	if err != nil {
		return nil, false
	}
	return out, true
}

func filterMcpTools(line []byte) bool {
	var response map[string]json.RawMessage
	if json.Unmarshal(line, &response) != nil {
		return false
	}
	if _, hasResult := response["result"]; hasResult {
		var result map[string]json.RawMessage
		if json.Unmarshal(response["result"], &result) != nil {
			return false
		}
		if _, hasTools := result["tools"]; hasTools {
			return true
		}
	}
	return false
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

func mcpParseErrorResponse(id json.RawMessage) []byte {
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
	}{Code: -32700, Message: "Parse error"}})
	return response
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
	allowedSet := make(map[string]bool, len(allowedTools))
	for _, name := range allowedTools {
		allowedSet[name] = true
	}
	hidden := buildHiddenToolNames(byName, allowedSet, staticToolUniverse(allowedTools))
	filtered := make([]json.RawMessage, 0, len(allowedTools))
	for _, name := range allowedTools {
		if tool, ok := byName[name]; ok {
			if len(hidden) > 0 {
				tool = scrubHiddenToolNames(tool, name, hidden)
			}
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

// staticToolUniverses lists the full known upstream tool names for each bounded
// proxy. It supplements names derived from the current response.
var staticToolUniverses = []struct {
	tool  string
	names []string
}{
	{"projectmem", []string{
		"add_decision", "add_note", "current_project", "get_context",
		"get_global_gotchas", "get_instructions", "get_issue", "get_plan",
		"get_project_map", "get_score", "get_summary", "list_projects",
		"log_issue", "precheck_file", "record_attempt", "record_fix",
		"search_events",
	}},
	{"context-mode", []string{
		"ctx_execute", "ctx_execute_file", "ctx_batch_execute", "ctx_search",
		"ctx_index", "ctx_fetch_and_index", "ctx_stats", "ctx_doctor",
		"ctx_upgrade", "ctx_insight", "ctx_purge",
	}},
}

// staticToolUniverse returns the static name set for the bounded proxy whose
// allowlist contains all of allowed, or nil when no proxy matches.
func staticToolUniverse(allowed []string) []string {
	if len(allowed) == 0 {
		return nil
	}
	for _, universe := range staticToolUniverses {
		if stringSubset(allowed, universe.names) {
			return universe.names
		}
	}
	return nil
}

func stringSubset(subset, superset []string) bool {
	set := make(map[string]bool, len(superset))
	for _, name := range superset {
		set[name] = true
	}
	for _, name := range subset {
		if !set[name] {
			return false
		}
	}
	return true
}

// buildHiddenToolNames returns the set of names present upstream or in the
// static universe that are NOT in the allowlist, sorted longest-first so that
// prefix names don't shadow longer names during regex alternation.
func buildHiddenToolNames(byName map[string]json.RawMessage, allowed map[string]bool, universe []string) []string {
	set := make(map[string]bool, len(byName)+len(universe))
	for name := range byName {
		if !allowed[name] {
			set[name] = true
		}
	}
	for _, name := range universe {
		if !allowed[name] {
			set[name] = true
		}
	}
	hidden := make([]string, 0, len(set))
	for name := range set {
		hidden = append(hidden, name)
	}
	sort.Slice(hidden, func(i, j int) bool { return len(hidden[i]) > len(hidden[j]) })
	return hidden
}

// scrubHiddenToolNames deletes occurrences of hidden tool names from all
// free-text string fields and object keys of the kept tool, leaving the tool's
// own "name" field intact.
func scrubHiddenToolNames(tool json.RawMessage, ownName string, hidden []string) json.RawMessage {
	var node any
	if err := json.Unmarshal(tool, &node); err != nil {
		return tool
	}
	node = scrubJSONStrings(node, hiddenToolRegexp(hidden), ownName)
	out, err := json.Marshal(node)
	if err != nil {
		return tool
	}
	return out
}

// scrubJSONStrings walks the decoded JSON tree and replaces hidden tool name
// occurrences in all string values and object keys, except the value of a
// "name" key matching ownName.
func scrubJSONStrings(node any, re *regexp.Regexp, ownName string) any {
	switch v := node.(type) {
	case string:
		return re.ReplaceAllString(v, "the memory system")
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, val := range v {
			if key == "name" {
				if s, ok := val.(string); ok && s == ownName {
					out[key] = val
					continue
				}
			}
			out[re.ReplaceAllString(key, "")] = scrubJSONStrings(val, re, ownName)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = scrubJSONStrings(val, re, ownName)
		}
		return out
	default:
		return node
	}
}

// hiddenToolRegexp builds a single regex matching any hidden name as a whole word.
func hiddenToolRegexp(hidden []string) *regexp.Regexp {
	parts := make([]string, len(hidden))
	for i, name := range hidden {
		parts[i] = `\b` + regexp.QuoteMeta(name) + `\b`
	}
	return regexp.MustCompile(strings.Join(parts, "|"))
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
	prefix := string(buf[:n])
	return strings.HasPrefix(prefix, "#!/usr/bin/env node") ||
		strings.HasPrefix(prefix, "#!/usr/bin/env -S node") ||
		strings.HasPrefix(prefix, "#!/usr/bin/node")
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
