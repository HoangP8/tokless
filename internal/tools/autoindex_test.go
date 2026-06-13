package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestClaudeAutoIndexMergeIdempotentUnwire(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	defer os.Unsetenv("HOME")
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")
	cp := util.ClaudeCodePaths()
	os.MkdirAll(cp.Dir, 0o755)
	os.WriteFile(cp.Settings, []byte(`{"model":"sonnet","hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"echo user"}]}]}}`), 0o644)

	wireClaudeAutoIndex()
	b, _ := os.ReadFile(cp.Settings)
	s := string(b)
	if !strings.Contains(s, "echo user") {
		t.Fatal("existing user hook lost")
	}
	if !strings.Contains(s, autoIndexCmd) {
		t.Fatal("auto-index hook not added")
	}
	if !strings.Contains(s, "sonnet") {
		t.Fatal("unrelated settings lost")
	}

	// idempotent
	wireClaudeAutoIndex()
	b2, _ := os.ReadFile(cp.Settings)
	if strings.Count(string(b2), autoIndexCmd) != 1 {
		t.Fatalf("not idempotent: %d occurrences", strings.Count(string(b2), autoIndexCmd))
	}

	// unwire removes ours, keeps user hook
	unwireClaudeAutoIndex()
	b3, _ := os.ReadFile(cp.Settings)
	if strings.Contains(string(b3), autoIndexCmd) {
		t.Fatal("auto-index not removed on unwire")
	}
	if !strings.Contains(string(b3), "echo user") {
		t.Fatal("unwire clobbered user hook")
	}
}

func TestGeminiAutoIndexMergeIdempotentUnwire(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	defer os.Unsetenv("HOME")
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")
	p := geminiSettingsPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"theme":"dark","hooks":{"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"echo user"}]}]}}`), 0o644)

	wireGeminiAutoIndex()
	s, _ := os.ReadFile(p)
	if !strings.Contains(string(s), "echo user") {
		t.Fatal("existing user hook lost")
	}
	if !strings.Contains(string(s), autoIndexCmd) {
		t.Fatal("auto-index hook not added to gemini settings")
	}
	if !strings.Contains(string(s), "dark") {
		t.Fatal("unrelated gemini settings lost")
	}

	wireGeminiAutoIndex()
	s2, _ := os.ReadFile(p)
	if strings.Count(string(s2), autoIndexCmd) != 1 {
		t.Fatalf("gemini auto-index not idempotent: %d", strings.Count(string(s2), autoIndexCmd))
	}

	unwireGeminiAutoIndex()
	s3, _ := os.ReadFile(p)
	if strings.Contains(string(s3), autoIndexCmd) {
		t.Fatal("gemini auto-index not removed on unwire")
	}
	if !strings.Contains(string(s3), "echo user") {
		t.Fatal("unwire clobbered user gemini hook")
	}
}

func TestCodexAutoIndexMergeWithContextMode(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("CODEX_HOME", dir)
	defer os.Unsetenv("CODEX_HOME")
	hp := filepath.Join(dir, "hooks.json")
	os.WriteFile(hp, []byte(`{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"context-mode hook codex sessionstart"}]}]}}`), 0o644)

	wireCodexAutoIndex()
	b, _ := os.ReadFile(hp)
	s := string(b)
	if !strings.Contains(s, "context-mode hook codex") {
		t.Fatal("context-mode hook clobbered")
	}
	if !strings.Contains(s, autoIndexCmd) {
		t.Fatal("auto-index not added")
	}
	wireCodexAutoIndex()
	b2, _ := os.ReadFile(hp)
	if strings.Count(string(b2), autoIndexCmd) != 1 {
		t.Fatal("codex auto-index not idempotent")
	}
	unwireCodexAutoIndex()
	b3, _ := os.ReadFile(hp)
	if strings.Contains(string(b3), autoIndexCmd) {
		t.Fatal("auto-index not removed")
	}
	if !strings.Contains(string(b3), "context-mode hook codex") {
		t.Fatal("unwire clobbered context-mode hook")
	}
}
