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
	err = os.MkdirAll(filepath.Join(tempdir, ".gemini", "antigravity-ide"), 0755)
	if err != nil {
		t.Fatalf("failed to create .gemini/antigravity-ide: %v", err)
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

	// 3. <home>/.codex/config.toml contains "[mcp_servers.codegraph]", "[mcp_servers.context_mode]", and no context-mode hooks.
	codexConfigPath := filepath.Join(tempdir, ".codex", "config.toml")
	codexConfigData, err := os.ReadFile(codexConfigPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}
	codexConfigStr := string(codexConfigData)
	if !strings.Contains(codexConfigStr, "[mcp_servers.codegraph]") {
		t.Errorf("config.toml doesn't contain '[mcp_servers.codegraph]', got: %s", codexConfigStr)
	}
	if !strings.Contains(codexConfigStr, "[mcp_servers.context_mode]") {
		t.Errorf("config.toml doesn't contain '[mcp_servers.context_mode]', got: %s", codexConfigStr)
	}
	if strings.Contains(codexConfigStr, "[mcp_servers.context-mode]") {
		t.Errorf("config.toml still contains legacy '[mcp_servers.context-mode]', got: %s", codexConfigStr)
	}
	if !strings.Contains(codexConfigStr, "hooks = true") {
		t.Errorf("context-mode should enable Codex hooks like upstream config, got: %s", codexConfigStr)
	}
	if !strings.Contains(codexConfigStr, `approval_policy = "on-request"`) {
		t.Errorf("config.toml doesn't set approval_policy=on-request, got: %s", codexConfigStr)
	}

	// 4. <home>/.codex/hooks.json contains tokless runtime hooks plus one
	// minimal context-mode PreToolUse redirect hook. MCP + AGENTS.md stay base.
	codexHooksPath := filepath.Join(tempdir, ".codex", "hooks.json")
	codexHooksData, err := os.ReadFile(codexHooksPath)
	if err != nil {
		t.Fatalf("failed to read hooks.json: %v", err)
	}
	codexHooksStr := string(codexHooksData)
	if !strings.Contains(codexHooksStr, "context-mode hook codex pretooluse") {
		t.Errorf("hooks.json missing minimal context-mode hook, got: %s", codexHooksStr)
	}
	if !strings.Contains(codexHooksStr, "local_shell|shell|shell_command|exec_command|Bash|Shell|apply_patch|Edit|Write|grep_files|ctx_execute|ctx_execute_file|ctx_batch_execute|ctx_fetch_and_index|ctx_search|ctx_index|mcp__") {
		t.Errorf("Codex PreToolUse matcher should match upstream context-mode config, got: %s", codexHooksStr)
	}
	for _, bad := range []string{"SessionStart", "PreCompact", "PostToolUse", "UserPromptSubmit", "context-mode hook codex sessionstart"} {
		if strings.Contains(codexHooksStr, bad) {
			t.Errorf("hooks.json should not contain legacy context-mode hook %q, got: %s", bad, codexHooksStr)
		}
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
	if !strings.Contains(codexHooksStr, "codex-perm codex") {
		t.Errorf("hooks.json missing PermissionRequest hook (codex-perm codex), got: %s", codexHooksStr)
	}
	if !strings.Contains(codexHooksStr, "PermissionRequest") {
		t.Errorf("hooks.json missing PermissionRequest event key, got: %s", codexHooksStr)
	}
	// default.rules allowlist
	rulesPath := filepath.Join(tempdir, ".codex", "rules", "default.rules")
	rulesData, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("failed to read default.rules: %v", err)
	}
	rulesStr := string(rulesData)
	if !strings.Contains(rulesStr, "tokless-managed codex allowlist") {
		t.Errorf("default.rules missing tokless marker, got: %s", rulesStr)
	}
	if !strings.Contains(rulesStr, `prefix_rule(pattern = ["rtk"], decision = "allow")`) {
		t.Errorf("default.rules missing rtk prefix_rule, got: %s", rulesStr)
	}

	// 5. <home>/.gemini/config/mcp_config.json contains both MCP tools.
	agyMcpPath := filepath.Join(tempdir, ".gemini", "config", "mcp_config.json")
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
	if !strings.Contains(string(claudeSettings), "mcp__codegraph__.*") {
		t.Errorf("claude settings.json doesn't auto-approve codegraph MCP, got: %s", string(claudeSettings))
	}

	// 6. Antigravity: rtk/codegraph hooks, but no context-mode PreToolUse hook.
	hooksContent, _ := os.ReadFile(filepath.Join(tempdir, ".gemini", "config", "hooks.json"))
	if !strings.Contains(string(hooksContent), "rtk-hook agy") {
		t.Errorf("antigravity hooks.json does not invoke `rtk-hook agy`, got: %s", string(hooksContent))
	}
	for _, bad := range []string{"context-mode hook antigravity-cli pretooluse", "context-mode-hook agy", "context-mode hook gemini", "beforetool", "run_command|view_file|grep_search|web_fetch|read_url_content"} {
		if strings.Contains(string(hooksContent), bad) {
			t.Errorf("antigravity hooks.json should not contain context-mode hook artifact %q, got: %s", bad, string(hooksContent))
		}
	}
	if !strings.Contains(string(hooksContent), "tokless-codegraph-index") {
		t.Errorf("antigravity hooks.json missing codegraph-index hook group, got: %s", string(hooksContent))
	}
	if !strings.Contains(string(hooksContent), "agy-hook codegraph-index") {
		t.Errorf("antigravity hooks.json missing agy-hook codegraph-index command, got: %s", string(hooksContent))
	}
	if util.Exists(filepath.Join(tempdir, ".gemini", "config", "tokless", "context-mode-routing.md")) {
		t.Errorf("antigravity context-mode should not write intermediate routing file")
	}
	geminiMd, err := os.ReadFile(filepath.Join(tempdir, ".gemini", "GEMINI.md"))
	if err != nil {
		t.Fatalf("failed to read antigravity GEMINI.md: %v", err)
	}
	if !strings.Contains(string(geminiMd), "## Context Tools (context-mode)") {
		t.Errorf("antigravity GEMINI.md missing context-mode routing section, got: %s", string(geminiMd))
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
	if _, err := os.Stat(filepath.Join(proj, "GEMINI.md")); err == nil {
		t.Errorf("project-local antigravity GEMINI.md routing file should NOT be written")
	}

	// 7. Antigravity codegraph MCP launches directly (not via tokless run-mcp proxy).
	if strings.Contains(agyMcpStr, "run-mcp") {
		t.Errorf("antigravity mcp_config.json codegraph entry should NOT be wrapped with run-mcp, got: %s", agyMcpStr)
	}

	// 8. Antigravity uses one canonical MCP config, not per-surface duplicates.
	agyIdeMcpPath := filepath.Join(tempdir, ".gemini", "antigravity-ide", "mcp_config.json")
	if util.Exists(agyIdeMcpPath) {
		t.Errorf("antigravity-ide mcp_config.json should not be created")
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

func TestInitPiWiring(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	tempdir := t.TempDir()

	// --- seed existing user config ---
	piDir := filepath.Join(tempdir, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0755); err != nil {
		t.Fatalf("mkdir .pi/agent: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(piDir, "extensions"), 0755); err != nil {
		t.Fatalf("mkdir extensions: %v", err)
	}

	// user settings.json with pre-existing packages
	userSettings := `{"packages":["user-custom-pkg","another-user-pkg"],"theme":"dark"}`
	if err := os.WriteFile(filepath.Join(piDir, "settings.json"), []byte(userSettings), 0644); err != nil {
		t.Fatalf("write user settings.json: %v", err)
	}

	// user extensions (must survive init)
	userExt := `// my custom extension
export default async function(pi) { console.log("user ext") }`
	if err := os.WriteFile(filepath.Join(piDir, "extensions", "my-custom.ts"), []byte(userExt), 0644); err != nil {
		t.Fatalf("write user extension: %v", err)
	}

	// user AGENTS.md (must survive init)
	userAgentsMd := "# My Project\n\nCustom instructions for my team.\n"
	if err := os.WriteFile(filepath.Join(piDir, "AGENTS.md"), []byte(userAgentsMd), 0644); err != nil {
		t.Fatalf("write user AGENTS.md: %v", err)
	}

	util.SetHomeOverride(tempdir)
	t.Setenv("HOME", tempdir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempdir, ".config"))
	t.Setenv("PI_CODING_AGENT_DIR", "") // must not inherit outer env
	defer util.SetHomeOverride("")

	// --- run init for pi ---
	code := commands.RunInit(commands.InitOptions{
		Agents: []string{"pi"},
	})
	if code != 0 {
		t.Fatalf("RunInit returned non-zero code: %d", code)
	}

	// --- verify settings.json packages ---
	settingsRaw, err := os.ReadFile(filepath.Join(piDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	settings := string(settingsRaw)

	// Only MCP bridge remains as a pi package; tools use skills/MCP.
	if !strings.Contains(settings, "npm:pi-mcp-adapter") {
		t.Errorf("settings.json missing pi-mcp-adapter, got:\n%s", settings)
	}
	// Must NOT install tools via old pi packages.
	for _, bad := range []string{
		"npm:context-mode", "npm:pi-caveman", "npm:pi-ponytail",
		"git:github.com/JuliusBrussee/caveman", "git:github.com/DietrichGebert/ponytail",
		"@vndv/pi-codegraph",
	} {
		if strings.Contains(settings, bad) {
			t.Errorf("settings.json still has old pi package %q, got:\n%s", bad, settings)
		}
	}
	// MCP: codegraph + context-mode
	mcpRaw, _ := os.ReadFile(filepath.Join(piDir, "mcp.json"))
	mcpStr := string(mcpRaw)
	for _, want := range []string{"codegraph", "context-mode"} {
		if !strings.Contains(mcpStr, want) {
			t.Errorf("mcp.json missing %q, got:\n%s", want, mcpStr)
		}
	}
	// Skills from source (skills CLI stubs in TOKLESS_TEST)
	for _, skill := range []string{"caveman", "ponytail"} {
		sk := filepath.Join(piDir, "skills", skill, "SKILL.md")
		if !util.Exists(sk) {
			t.Errorf("missing pi skill %s", sk)
		}
	}
	// user's pre-existing packages preserved
	for _, pkg := range []string{"user-custom-pkg", "another-user-pkg"} {
		if !strings.Contains(settings, pkg) {
			t.Errorf("settings.json lost user package %q, got:\n%s", pkg, settings)
		}
	}
	// user's theme field preserved
	if !strings.Contains(settings, `"theme"`) || !strings.Contains(settings, `"dark"`) {
		t.Errorf("settings.json lost user theme field, got:\n%s", settings)
	}

	// RTK: rtk init --agent pi → extensions/rtk.ts
	if !util.Exists(filepath.Join(piDir, "extensions", "rtk.ts")) {
		t.Error("RTK extension rtk.ts not created")
	}
	// codegraph auto-index extension
	if !util.Exists(filepath.Join(piDir, "extensions", "codegraph-index.ts")) {
		t.Error("codegraph-index.ts not created for pi")
	}

	// --- verify user extensions preserved ---
	userExtPath := filepath.Join(piDir, "extensions", "my-custom.ts")
	if !util.Exists(userExtPath) {
		t.Error("user extension my-custom.ts was deleted by init")
	} else {
		survived, _ := os.ReadFile(userExtPath)
		if string(survived) != userExt {
			t.Errorf("user extension my-custom.ts content changed, got:\n%s", string(survived))
		}
	}

	// --- verify AGENTS.md ---
	agentsMd, err := os.ReadFile(filepath.Join(piDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	agentsMdStr := string(agentsMd)

	// user content preserved
	if !strings.Contains(agentsMdStr, "Custom instructions for my team.") {
		t.Errorf("AGENTS.md lost user content, got:\n%s", agentsMdStr)
	}
	// tokless managed sections present (owner markers)
	for _, owner := range []string{"caveman", "codegraph", "context-mode", "ponytail"} {
		if !strings.Contains(agentsMdStr, owner) {
			t.Errorf("AGENTS.md missing owner %q, got:\n%s", owner, agentsMdStr)
		}
	}

	// --- verify idempotency: re-run should not corrupt ---
	code2 := commands.RunInit(commands.InitOptions{
		Agents: []string{"pi"},
	})
	if code2 != 0 {
		t.Fatalf("second RunInit returned non-zero code: %d", code2)
	}

	settingsRaw2, err := os.ReadFile(filepath.Join(piDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json after 2nd run: %v", err)
	}
	for _, pkg := range []string{"user-custom-pkg", "another-user-pkg", "npm:pi-mcp-adapter"} {
		if !strings.Contains(string(settingsRaw2), pkg) {
			t.Errorf("settings.json after 2nd run missing package %q", pkg)
		}
	}
	for _, skill := range []string{"caveman", "ponytail"} {
		if !util.Exists(filepath.Join(piDir, "skills", skill, "SKILL.md")) {
			t.Errorf("skill %s missing after 2nd run", skill)
		}
	}
	if !util.Exists(userExtPath) {
		t.Error("user extension deleted after 2nd run")
	}
	agentsMd2, _ := os.ReadFile(filepath.Join(piDir, "AGENTS.md"))
	if !strings.Contains(string(agentsMd2), "Custom instructions for my team.") {
		t.Error("AGENTS.md user content lost after 2nd run")
	}

	// --- verify all pi tools report correct state after wiring ---
	toolIDs := []string{"rtk", "caveman", "codegraph", "context-mode", "ponytail"}
	for _, id := range toolIDs {
		tm := core.GetTool(id)
		if tm == nil {
			t.Fatalf("tool %q not registered", id)
		}
		verifyFn, ok := tm.VerifyFor["pi"]
		if !ok {
			t.Errorf("tool %q has no VerifyFor pi", id)
			continue
		}
		result := verifyFn()
		if result == nil {
			t.Errorf("tool %q VerifyFor pi returned nil", id)
		} else if !*result {
			t.Errorf("tool %q VerifyFor pi returned false after wiring", id)
		}
	}
}

func TestPiUnwire(t *testing.T) {
	t.Setenv("TOKLESS_TEST", "1")
	tempdir := t.TempDir()

	piDir := filepath.Join(tempdir, ".pi", "agent")
	if err := os.MkdirAll(filepath.Join(piDir, "extensions"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settings := `{"packages":["npm:pi-mcp-adapter"],"theme":"light"}`
	if err := os.WriteFile(filepath.Join(piDir, "settings.json"), []byte(settings), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	for _, skill := range []string{"caveman", "ponytail"} {
		d := filepath.Join(piDir, "skills", skill)
		_ = os.MkdirAll(d, 0755)
		_ = os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("# "+skill+"\n"), 0644)
	}
	mcp := `{"mcpServers":{"codegraph":{"command":"codegraph"},"context-mode":{"command":"context-mode"}}}`
	if err := os.WriteFile(filepath.Join(piDir, "mcp.json"), []byte(mcp), 0644); err != nil {
		t.Fatalf("write mcp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piDir, "extensions", "rtk.ts"), []byte("// rtk"), 0644); err != nil {
		t.Fatalf("write rtk: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piDir, "extensions", "codegraph-index.ts"), []byte("// cg"), 0644); err != nil {
		t.Fatalf("write codegraph-index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piDir, "AGENTS.md"), []byte("# Tokless\n\n<!-- caveman -->\n<!-- rtk -->\n\nUser content.\n"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	util.SetHomeOverride(tempdir)
	t.Setenv("HOME", tempdir)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	defer util.SetHomeOverride("")

	// unwrap pi tools
	opts := core.RunOpts{}
	unwrapPi := func(toolID string) {
		t.Helper()
		tool := core.GetTool(toolID)
		if tool == nil {
			t.Fatalf("tool %q not registered", toolID)
		}
		fn, ok := tool.UnwireFor["pi"]
		if !ok {
			t.Fatalf("tool %q has no UnwireFor pi", toolID)
		}
		ok, err := fn(opts)
		if err != nil {
			t.Fatalf("unwrap %s for pi: %v", toolID, err)
		}
		if !ok {
			t.Errorf("unwrap %s for pi returned false", toolID)
		}
	}

	unwrapPi("rtk")
	unwrapPi("caveman")
	unwrapPi("context-mode")
	unwrapPi("ponytail")
	unwrapPi("codegraph")

	if util.Exists(filepath.Join(piDir, "extensions", "rtk.ts")) {
		t.Error("RTK extension should be removed after unwire")
	}
	if util.Exists(filepath.Join(piDir, "extensions", "codegraph-index.ts")) {
		t.Error("codegraph-index.ts should be removed after unwire")
	}

	for _, skill := range []string{"caveman", "ponytail"} {
		if util.Exists(filepath.Join(piDir, "skills", skill, "SKILL.md")) {
			t.Errorf("skill %s still present after unwire", skill)
		}
	}
	mcpRaw, _ := os.ReadFile(filepath.Join(piDir, "mcp.json"))
	mcpStr := string(mcpRaw)
	for _, bad := range []string{"codegraph", "context-mode"} {
		if strings.Contains(mcpStr, bad) {
			t.Errorf("mcp.json still contains %q after unwire", bad)
		}
	}
	settingsRaw, _ := os.ReadFile(filepath.Join(piDir, "settings.json"))
	settingsStr := string(settingsRaw)
	if !strings.Contains(settingsStr, `"theme"`) || !strings.Contains(settingsStr, `"light"`) {
		t.Errorf("settings.json lost user theme after unwire, got:\n%s", settingsStr)
	}
}
