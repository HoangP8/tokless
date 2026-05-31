package commands_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/commands"
	"github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

func TestMain(m *testing.M) {
	agents.Register()
	tools.Register()
	os.Exit(m.Run())
}

func TestInitSandboxWiring(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	tempdir := t.TempDir()

	err := os.MkdirAll(filepath.Join(tempdir, ".claude"), 0755)
	if err != nil {
		t.Fatalf("failed to create .claude: %v", err)
	}
	err = os.MkdirAll(filepath.Join(tempdir, ".config", "opencode"), 0755)
	if err != nil {
		t.Fatalf("failed to create opencode: %v", err)
	}
	err = os.MkdirAll(filepath.Join(tempdir, ".codex"), 0755)
	if err != nil {
		t.Fatalf("failed to create .codex: %v", err)
	}

	util.SetHomeOverride(tempdir)
	t.Setenv("HOME", tempdir)
	defer util.SetHomeOverride("")

	code := commands.RunInit(commands.InitOptions{
		Agents: []string{"claude", "opencode", "codex"},
	})
	if code != 0 {
		t.Errorf("RunInit returned non-zero code: %d", code)
	}

	// 1. <home>/.claude.json contains "codegraph" and "context-mode"
	claudePath := filepath.Join(tempdir, ".claude.json")
	claudeData, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("failed to read .claude.json: %v", err)
	}
	claudeStr := string(claudeData)
	if !strings.Contains(claudeStr, "codegraph") {
		t.Errorf(".claude.json doesn't contain 'codegraph', got: %s", claudeStr)
	}
	if !strings.Contains(claudeStr, "context-mode") {
		t.Errorf(".claude.json doesn't contain 'context-mode', got: %s", claudeStr)
	}

	// 2. <home>/.config/opencode/opencode.jsonc contains "context-mode" and "codegraph"
	opencodePath := filepath.Join(tempdir, ".config", "opencode", "opencode.jsonc")
	opencodeData, err := os.ReadFile(opencodePath)
	if err != nil {
		t.Fatalf("failed to read opencode.jsonc: %v", err)
	}
	opencodeStr := string(opencodeData)
	if !strings.Contains(opencodeStr, "context-mode") {
		t.Errorf("opencode.jsonc doesn't contain 'context-mode', got: %s", opencodeStr)
	}
	if !strings.Contains(opencodeStr, "codegraph") {
		t.Errorf("opencode.jsonc doesn't contain 'codegraph', got: %s", opencodeStr)
	}

	// 3. <home>/.codex/config.toml contains "[mcp_servers.codegraph]", "[mcp_servers.context-mode]", and "[features]"
	codexConfigPath := filepath.Join(tempdir, ".codex", "config.toml")
	codexConfigData, err := os.ReadFile(codexConfigPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}
	codexConfigStr := string(codexConfigData)
	if !strings.Contains(codexConfigStr, "[mcp_servers.codegraph]") {
		t.Errorf("config.toml doesn't contain '[mcp_servers.codegraph]', got: %s", codexConfigStr)
	}
	if !strings.Contains(codexConfigStr, "[mcp_servers.context-mode]") {
		t.Errorf("config.toml doesn't contain '[mcp_servers.context-mode]', got: %s", codexConfigStr)
	}
	if !strings.Contains(codexConfigStr, "[features]") {
		t.Errorf("config.toml doesn't contain '[features]', got: %s", codexConfigStr)
	}

	// 4. <home>/.codex/hooks.json contains "context-mode hook codex pretooluse"
	codexHooksPath := filepath.Join(tempdir, ".codex", "hooks.json")
	codexHooksData, err := os.ReadFile(codexHooksPath)
	if err != nil {
		t.Fatalf("failed to read hooks.json: %v", err)
	}
	codexHooksStr := string(codexHooksData)
	if !strings.Contains(strings.ToLower(codexHooksStr), "context-mode hook codex pretooluse") {
		t.Errorf("hooks.json doesn't contain 'context-mode hook codex pretooluse', got: %s", codexHooksStr)
	}
}

func getSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func TestInitIdempotent(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	tempdir := t.TempDir()

	err := os.MkdirAll(filepath.Join(tempdir, ".claude"), 0755)
	if err != nil {
		t.Fatalf("failed to create .claude: %v", err)
	}
	err = os.MkdirAll(filepath.Join(tempdir, ".config", "opencode"), 0755)
	if err != nil {
		t.Fatalf("failed to create opencode: %v", err)
	}
	err = os.MkdirAll(filepath.Join(tempdir, ".codex"), 0755)
	if err != nil {
		t.Fatalf("failed to create .codex: %v", err)
	}

	util.SetHomeOverride(tempdir)
	t.Setenv("HOME", tempdir)
	defer util.SetHomeOverride("")

	// First Run
	code := commands.RunInit(commands.InitOptions{
		Agents: []string{"claude", "opencode", "codex"},
	})
	if code != 0 {
		t.Fatalf("First RunInit returned non-zero code: %d", code)
	}

	// Read and hash
	paths := []string{
		filepath.Join(tempdir, ".claude.json"),
		filepath.Join(tempdir, ".config", "opencode", "opencode.jsonc"),
		filepath.Join(tempdir, ".codex", "config.toml"),
		filepath.Join(tempdir, ".codex", "hooks.json"),
	}

	hashes1 := make([]string, len(paths))
	for i, p := range paths {
		h, err := getSHA256(p)
		if err != nil {
			t.Fatalf("failed to hash %s: %v", p, err)
		}
		hashes1[i] = h
	}

	// Second Run
	code = commands.RunInit(commands.InitOptions{
		Agents: []string{"claude", "opencode", "codex"},
	})
	if code != 0 {
		t.Fatalf("Second RunInit returned non-zero code: %d", code)
	}

	// Re-hash and compare
	for i, p := range paths {
		h, err := getSHA256(p)
		if err != nil {
			t.Fatalf("failed to re-hash %s: %v", p, err)
		}
		if h != hashes1[i] {
			content1, _ := os.ReadFile(p)
			t.Errorf("file %s changed after second run! Hash 1: %s, Hash 2: %s\nContent:\n%s", p, hashes1[i], h, string(content1))
		}
	}
}
