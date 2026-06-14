package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestClaudeAutoIndexUnwireKeepsUserHook(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	defer os.Unsetenv("HOME")
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")
	cp := util.ClaudeCodePaths()
	os.MkdirAll(cp.Dir, 0o755)
	os.WriteFile(cp.Settings, []byte(`{"model":"sonnet","hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"echo user"}]},{"matcher":"startup","hooks":[{"type":"command","command":"tokless index --auto --agent claude"}]}]}}`), 0o644)

	unwireClaudeAutoIndex()
	b, _ := os.ReadFile(cp.Settings)
	s := string(b)
	if strings.Contains(s, autoIndexCmd) {
		t.Fatal("legacy auto-index hook not removed on unwire")
	}
	if !strings.Contains(s, "echo user") {
		t.Fatal("unwire clobbered user hook")
	}
	if !strings.Contains(s, "sonnet") {
		t.Fatal("unrelated settings lost")
	}
}

func TestGeminiAutoIndexUnwireKeepsUserHook(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	defer os.Unsetenv("HOME")
	util.SetHomeOverride(dir)
	defer util.SetHomeOverride("")
	p := geminiSettingsPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"theme":"dark","hooks":{"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"echo user"}]},{"matcher":"","hooks":[{"type":"command","command":"tokless index --auto --agent antigravity"}]}]}}`), 0o644)

	unwireGeminiAutoIndex()
	s, _ := os.ReadFile(p)
	if strings.Contains(string(s), autoIndexCmd) {
		t.Fatal("legacy gemini auto-index hook not removed on unwire")
	}
	if !strings.Contains(string(s), "echo user") {
		t.Fatal("unwire clobbered user gemini hook")
	}
	if !strings.Contains(string(s), "dark") {
		t.Fatal("unrelated gemini settings lost")
	}
}

func TestCodexAutoIndexUnwireKeepsContextMode(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("CODEX_HOME", dir)
	defer os.Unsetenv("CODEX_HOME")
	hp := filepath.Join(dir, "hooks.json")
	os.WriteFile(hp, []byte(`{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"context-mode hook codex sessionstart"}]},{"matcher":"startup","hooks":[{"type":"command","command":"tokless index --auto --agent codex"}]}]}}`), 0o644)

	unwireCodexAutoIndex()
	b, _ := os.ReadFile(hp)
	s := string(b)
	if strings.Contains(s, autoIndexCmd) {
		t.Fatal("legacy codex auto-index hook not removed on unwire")
	}
	if !strings.Contains(s, "context-mode hook codex") {
		t.Fatal("unwire clobbered context-mode hook")
	}
}
