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
	"github.com/HoangP8/tokless/internal/core"
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
	userHook := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/usr/bin/user-guard.py","timeout":20}]}]}}`
	if err := os.WriteFile(filepath.Join(tempdir, ".codex", "hooks.json"), []byte(userHook), 0644); err != nil {
		t.Fatalf("failed to seed user hooks.json: %v", err)
	}
	err = os.MkdirAll(filepath.Join(tempdir, ".gemini", "antigravity"), 0755)
	if err != nil {
		t.Fatalf("failed to create .gemini/antigravity: %v", err)
	}

	util.SetHomeOverride(tempdir)
	t.Setenv("HOME", tempdir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempdir, ".config"))
	defer util.SetHomeOverride("")

	// Antigravity wiring is partly project-scoped — run from a sandbox project dir.
	proj := filepath.Join(tempdir, "proj")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatalf("failed to create proj: %v", err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(proj); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(oldWd)

	code := commands.RunInit(commands.InitOptions{
		Agents: []string{"claude", "opencode", "codex", "antigravity"},
	})
	if code != 0 {
		t.Errorf("RunInit returned non-zero code: %d", code)
	}

	indexCode := commands.RunIndex(commands.InitOptions{
		Agents: []string{"claude", "opencode", "codex", "antigravity"},
	}, false)
	if indexCode != 0 {
		t.Errorf("RunIndex returned non-zero code: %d", indexCode)
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
	if !strings.Contains(codexConfigStr, `approval_policy = "never"`) {
		t.Errorf("config.toml doesn't auto-approve (approval_policy=never), got: %s", codexConfigStr)
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
	if !strings.Contains(codexHooksStr, "rtk-hook codex") {
		t.Errorf("hooks.json doesn't contain the rtk hook 'rtk-hook codex', got: %s", codexHooksStr)
	}
	if !strings.Contains(codexConfigStr, "[hooks.state") || !strings.Contains(codexConfigStr, "trusted_hash") {
		t.Errorf("config.toml doesn't pre-seed rtk hook trust ([hooks.state]/trusted_hash), got: %s", codexConfigStr)
	}
	if util.Exists(filepath.Join(tempdir, ".codex", "RTK.md")) {
		t.Errorf("codex RTK.md instruction should NOT be written (hook handles rewriting)")
	}
	if !strings.Contains(codexHooksStr, "/usr/bin/user-guard.py") {
		t.Errorf("user's pre-existing hook was overwritten — must be preserved, got: %s", codexHooksStr)
	}

	// 5. <home>/.gemini/antigravity/mcp_config.json contains both MCP tools
	agyMcpPath := filepath.Join(tempdir, ".gemini", "antigravity", "mcp_config.json")
	agyMcpData, err := os.ReadFile(agyMcpPath)
	if err != nil {
		t.Fatalf("failed to read antigravity mcp_config.json: %v", err)
	}
	agyMcpStr := string(agyMcpData)
	if !strings.Contains(agyMcpStr, "codegraph") {
		t.Errorf("antigravity mcp_config.json doesn't contain 'codegraph', got: %s", agyMcpStr)
	}
	if !strings.Contains(agyMcpStr, "context-mode") {
		t.Errorf("antigravity mcp_config.json doesn't contain 'context-mode', got: %s", agyMcpStr)
	}
	if !strings.Contains(agyMcpStr, "trust") {
		t.Errorf("antigravity mcp_config.json doesn't auto-approve (trust), got: %s", agyMcpStr)
	}

	// Claude auto-approves tokless MCP tools via permissions.allow in settings.json.
	claudeSettings, err := os.ReadFile(filepath.Join(tempdir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("failed to read claude settings.json: %v", err)
	}
	if !strings.Contains(string(claudeSettings), "mcp__codegraph__*") {
		t.Errorf("claude settings.json doesn't auto-approve codegraph MCP, got: %s", string(claudeSettings))
	}

	// 6. Antigravity PreToolUse hook is installed + project-scoped context-mode routing GEMINI.md
	hooksContent, _ := os.ReadFile(filepath.Join(tempdir, ".gemini", "config", "hooks.json"))
	if !strings.Contains(string(hooksContent), "rtk-hook agy") {
		t.Errorf("antigravity hooks.json does not invoke `rtk-hook agy`, got: %s", string(hooksContent))
	}
	if util.Exists(filepath.Join(tempdir, ".gemini", "config", "tokless", "rtk-rewrite.sh")) ||
		util.Exists(filepath.Join(tempdir, ".gemini", "config", "tokless-rtk-rewrite.sh")) {
		t.Errorf("no rtk rewrite wrapper script should be installed")
	}
	// rtk uses the native hook now, not an instruction rule.
	if _, err := os.Stat(filepath.Join(proj, ".agents", "rules", "antigravity-rtk-rules.md")); err == nil {
		t.Errorf("antigravity rtk instruction rule should NOT be written")
	}
	if _, err := os.Stat(filepath.Join(proj, ".agents", "rules", "antigravity-codegraph-rules.md")); err == nil {
		t.Errorf("fabricated antigravity-codegraph-rules.md should not be written")
	}
	if _, err := os.Stat(filepath.Join(proj, "GEMINI.md")); err != nil {
		t.Errorf("antigravity GEMINI.md routing file not written: %v", err)
	}

	// 7. Antigravity codegraph MCP launch is wrapped through `tokless run-mcp`
	// so opening a project in the IDE auto-indexes (IDE has no startup hook).
	if !strings.Contains(agyMcpStr, "run-mcp") {
		t.Errorf("antigravity mcp_config.json codegraph entry not wrapped with run-mcp, got: %s", agyMcpStr)
	}
}

func TestAutoIndexRtkIndependentOfCodegraph(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	tempdir := t.TempDir()
	for _, d := range []string{".claude", filepath.Join(".config", "opencode"), ".codex", filepath.Join(".gemini", "antigravity")} {
		if err := os.MkdirAll(filepath.Join(tempdir, d), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	util.SetHomeOverride(tempdir)
	t.Setenv("HOME", tempdir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempdir, ".config"))
	defer util.SetHomeOverride("")

	proj := filepath.Join(tempdir, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".git"), 0755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(proj, ".codegraph"), 0755); err != nil {
		t.Fatalf("mkdir .codegraph: %v", err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(proj); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(oldWd)

	if code := commands.RunInit(commands.InitOptions{Agents: []string{"claude", "antigravity"}}); code != 0 {
		t.Fatalf("RunInit returned non-zero code: %d", code)
	}
	commands.RunIndex(commands.InitOptions{}, true)

	if !util.Exists(filepath.Join(tempdir, ".gemini", "config", "hooks.json")) {
		t.Errorf("antigravity rtk PreToolUse hook (hooks.json) not installed")
	}
	if _, err := os.Stat(filepath.Join(proj, ".agents", "rules", "antigravity-rtk-rules.md")); err == nil {
		t.Errorf("antigravity rtk instruction rule should NOT be written")
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
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempdir, ".config"))
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

func TestCavemanNotTrackable(t *testing.T) {
	caveman := core.GetTool("caveman")
	if caveman == nil {
		t.Fatalf("expected tool 'caveman' to be registered, but it was nil")
	}
	if !caveman.NotTrackable {
		t.Errorf("expected tool 'caveman' to have NotTrackable set to true, but got false")
	}

	trackableTools := map[string]bool{
		"rtk":          true,
		"codegraph":    true,
		"context-mode": true,
	}

	for _, tool := range core.ListTools() {
		if trackableTools[tool.ID] {
			if tool.NotTrackable {
				t.Errorf("expected tool %q to have NotTrackable set to false, but got true", tool.ID)
			}
		}
	}
}
