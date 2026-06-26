package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// McpInstructions probes an MCP server via the JSON-RPC initialize handshake
// and returns the result.instructions field.
func McpInstructions(spawn McpSpawn) (string, bool) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return "", false
	}
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"tokless","version":"0"}}}` + "\n"
	cmd := exec.Command(spawn.Command, spawn.Args...)
	cmd.Stdin = strings.NewReader(initReq)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", false
	}
	if err := cmd.Start(); err != nil {
		return "", false
	}
	defer cmd.Process.Kill()

	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 0, 64*1024)
		tmp := make([]byte, 4096)
		for {
			n, err := stdout.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				if idx := strings.IndexByte(string(buf), '\n'); idx >= 0 {
					ch <- result{data: buf[:idx], err: nil}
					return
				}
			}
			if err != nil {
				ch <- result{data: buf, err: err}
				return
			}
		}
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case r := <-ch:
		cmd.Wait()
		if len(r.data) == 0 {
			return "", false
		}
var resp struct {
				Result struct {
					Instructions string `json:"instructions"`
				} `json:"result"`
			}
			if err := json.Unmarshal(r.data, &resp); err != nil {
				return "", false
			}
			if resp.Result.Instructions == "" {
				return "", false
			}
			return resp.Result.Instructions, true
	case <-timer.C:
		return "", false
	}
}

const (
	CodegraphMarkerStart = "<!-- CODEGRAPH_START -->"
	CodegraphMarkerEnd   = "<!-- CODEGRAPH_END -->"
)

const CodegraphAgentBlock = "## CodeGraph\n\n" +
	"In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:\n\n" +
	"- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.\n" +
	"- **Shell** (always works): `codegraph explore \"<symbol names or question>\"` prints the same output.\n\n" +
	"If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision."

func InstructionSourceFor(agentID, toolID string, spawn McpSpawn, packageConfigDir string) (string, bool) {
	if toolID == "codegraph" {
		return CodegraphAgentBlock, true
	}
	if instr, ok := McpInstructions(spawn); ok {
		return instr, true
	}
	if packageConfigDir != "" {
		for _, name := range instructionFileNames(agentID) {
			candidate := filepath.Join(packageConfigDir, agentID, name)
			if data, err := os.ReadFile(candidate); err == nil && len(data) > 0 {
				return string(data), true
			}
		}
	}
	return "", false
}

func instructionFileNames(agentID string) []string {
	switch agentID {
	case "claude":
		return []string{"CLAUDE.md", "AGENTS.md"}
	case "antigravity":
		return []string{"GEMINI.md", "AGENTS.md"}
	default:
		return []string{"AGENTS.md"}
	}
}

func markerStart(agentID, toolID string) string {
	if toolID == "codegraph" {
		return CodegraphMarkerStart
	}
	return "<!-- TOKLESS:" + agentID + ":" + toolID + ":START -->"
}
func markerEnd(agentID, toolID string) string {
	if toolID == "codegraph" {
		return CodegraphMarkerEnd
	}
	return "<!-- TOKLESS:" + agentID + ":" + toolID + ":END -->"
}

// MergeInstructionsFile merges a marked instruction block into the file at path.
func MergeInstructionsFile(path, agentID, toolID, content string) (changed bool) {
	start := markerStart(agentID, toolID)
	end := markerEnd(agentID, toolID)
	block := start + "\n" + content + "\n" + end

	raw, _ := os.ReadFile(path)
	s := string(raw)

	si := strings.Index(s, start)
	ei := strings.Index(s, end)

	if si >= 0 && ei > si {
		innerStart := si + len(start)
		innerEnd := ei
		existingInner := s[innerStart:innerEnd]
		existingTrim := strings.Trim(existingInner, "\n")
		newTrim := strings.Trim(content, "\n")
		if hashInner(existingTrim) == hashInner(newTrim) {
			return false
		}
		next := s[:si] + block + s[ei+len(end):]
		return writeFileIfChanged(path, next)
	}

	if si >= 0 || ei >= 0 {
		if len(s) > 0 && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		if len(s) > 0 && !strings.HasSuffix(s, "\n\n") {
			s += "\n"
		}
		next := s + block + "\n"
		return writeFileIfChanged(path, next)
	}

	if len(s) > 0 && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if len(s) > 0 && !strings.HasSuffix(s, "\n\n") {
		s += "\n"
	}
	next := s + block + "\n"
	return writeFileIfChanged(path, next)
}

// RemoveInstructionsFileBlock removes the marked block (markers + inner content) from the file.
func RemoveInstructionsFileBlock(path, agentID, toolID string) bool {
	start := markerStart(agentID, toolID)
	end := markerEnd(agentID, toolID)
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := string(raw)
	si := strings.Index(s, start)
	ei := strings.Index(s, end)
	if si < 0 || ei <= si {
		return false
	}
	removeStart := si
	removeEnd := ei + len(end)
	if removeEnd < len(s) && s[removeEnd] == '\n' {
		removeEnd++
	}
	if removeStart > 0 && s[removeStart-1] == '\n' {
		removeStart--
	}
	next := s[:removeStart] + s[removeEnd:]
	return writeFileIfChanged(path, next)
}

func hashInner(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func writeFileIfChanged(path, content string) bool {
	raw, _ := os.ReadFile(path)
	if string(raw) == content {
		return false
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(content), 0o644)
	return true
}
