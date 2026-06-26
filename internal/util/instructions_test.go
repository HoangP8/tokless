package util

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeInstructionsFileAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	changed := MergeInstructionsFile(path, "codex", "codegraph", "Use codegraph_explore first.")
	if !changed {
		t.Fatal("expected changed=true for new file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, CodegraphMarkerStart) {
		t.Errorf("missing START marker, got: %s", s)
	}
	if !strings.Contains(s, CodegraphMarkerEnd) {
		t.Errorf("missing END marker, got: %s", s)
	}
	if !strings.Contains(s, "Use codegraph_explore first.") {
		t.Errorf("missing instruction content, got: %s", s)
	}
}

func TestMergeInstructionsFileReplaceInner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	// Write file with user content + existing block.
	initial := "# My Rules\n\n" + CodegraphMarkerStart + "\nold content\n" + CodegraphMarkerEnd + "\n\n# More Rules\n"
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	changed := MergeInstructionsFile(path, "codex", "codegraph", "new content")
	if !changed {
		t.Fatal("expected changed=true for different content")
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, "# My Rules") {
		t.Errorf("user content before block lost, got: %s", s)
	}
	if !strings.Contains(s, "# More Rules") {
		t.Errorf("user content after block lost, got: %s", s)
	}
	if !strings.Contains(s, "new content") {
		t.Errorf("new content not present, got: %s", s)
	}
	if strings.Contains(s, "old content") {
		t.Errorf("old content still present, got: %s", s)
	}
}

func TestMergeInstructionsFileIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	initial := "# My Rules\n\n" + CodegraphMarkerStart + "\nnew content\n" + CodegraphMarkerEnd + "\n"
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	changed := MergeInstructionsFile(path, "codex", "codegraph", "new content")
	if changed {
		t.Fatal("expected changed=false for identical content")
	}
}

func TestMergeInstructionsFileMalformedStartOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	initial := "# My Rules\n\n" + CodegraphMarkerStart + "\nbroken\n"
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	changed := MergeInstructionsFile(path, "codex", "codegraph", "fresh content")
	if !changed {
		t.Fatal("expected changed=true for malformed markers")
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, "# My Rules") {
		t.Errorf("user content lost, got: %s", s)
	}
	if !strings.Contains(s, "fresh content") {
		t.Errorf("fresh content not appended, got: %s", s)
	}
	// Malformed fragment should still be there (not cleaned up).
	if !strings.Contains(s, "broken") {
		t.Errorf("malformed fragment removed (should be left for user), got: %s", s)
	}
}

func TestMergeInstructionsFileMalformedEndOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	initial := "# My Rules\n\n" + CodegraphMarkerEnd + "\nbroken\n"
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	changed := MergeInstructionsFile(path, "codex", "codegraph", "fresh content")
	if !changed {
		t.Fatal("expected changed=true for malformed markers")
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, "# My Rules") {
		t.Errorf("user content lost, got: %s", s)
	}
	if !strings.Contains(s, "fresh content") {
		t.Errorf("fresh content not appended, got: %s", s)
	}
}

func TestRemoveInstructionsFileBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	initial := "# My Rules\n\n" + CodegraphMarkerStart + "\ninner\n" + CodegraphMarkerEnd + "\n\n# More Rules\n"
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	removed := RemoveInstructionsFileBlock(path, "codex", "codegraph")
	if !removed {
		t.Fatal("expected removed=true")
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if strings.Contains(s, CodegraphMarkerStart) || strings.Contains(s, CodegraphMarkerEnd) {
		t.Errorf("markers still present, got: %s", s)
	}
	if strings.Contains(s, "inner") {
		t.Errorf("inner content still present, got: %s", s)
	}
	if !strings.Contains(s, "# My Rules") {
		t.Errorf("user content before block lost, got: %s", s)
	}
	if !strings.Contains(s, "# More Rules") {
		t.Errorf("user content after block lost, got: %s", s)
	}
}

func TestRemoveInstructionsFileBlockNoMarkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	initial := "# My Rules\n"
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	removed := RemoveInstructionsFileBlock(path, "codex", "codegraph")
	if removed {
		t.Fatal("expected removed=false when no markers present")
	}
}

func TestMcpInstructionsParse(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"test","version":"1.0"},"instructions":"Use this tool first."}}`
	var parsed struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Result.Instructions != "Use this tool first." {
		t.Errorf("expected 'Use this tool first.', got: %s", parsed.Result.Instructions)
	}
}

func TestMcpInstructionsParseMissing(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"test","version":"1.0"}}}`
	var parsed struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Result.Instructions != "" {
		t.Errorf("expected empty instructions, got: %s", parsed.Result.Instructions)
	}
}

func TestCodegraphMarkerMatchesUpstream(t *testing.T) {
	if CodegraphMarkerStart != "<!-- CODEGRAPH_START -->" {
		t.Errorf("CodegraphMarkerStart drifted: %q", CodegraphMarkerStart)
	}
	if CodegraphMarkerEnd != "<!-- CODEGRAPH_END -->" {
		t.Errorf("CodegraphMarkerEnd drifted: %q", CodegraphMarkerEnd)
	}
}