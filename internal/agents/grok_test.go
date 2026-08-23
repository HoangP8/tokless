package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func setGrokTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("GROK_HOME", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	return home
}

func TestGrokProxyHealsPoisonedProfile(t *testing.T) {
	setGrokTestHome(t)
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	t.Setenv("TOKLESS_OPENCODE_GO_KEY", "test-key")

	poisoned := "[model.dsv4-flash]\nmodel = \"deepseek-v4-flash\"\nbase_url = \"https://opencode.ai/zen/go/v1\"\nname = \"original\"\napi_key = \"k\"\n\n[model.headroom]\nmodel = \"deepseek-v4-flash\"\nbase_url = \"\"\napi_backend = \"chat_completions\"\nstream_tool_calls = false\napi_key = \"test-key\"\n"
	file := filepath.Join(dir, "config.toml")
	if err := util.WriteFile(file, poisoned); err != nil {
		t.Fatal(err)
	}

	changed, gotFile := ConfigureGrokProxy()
	if !changed || gotFile != file {
		t.Fatalf("ConfigureGrokProxy = (%v, %q), want (true, %q)", changed, gotFile, file)
	}
	raw, _ := util.ReadFileSafe(file)
	if !strings.Contains(raw, `base_url = "`+ProxyEndpointFor("grok")+`"`) {
		t.Fatalf("headroom profile not healed to managed endpoint:\n%s", raw)
	}
	if !strings.Contains(raw, "[model.dsv4-flash]") || !strings.Contains(raw, "https://opencode.ai/zen/go/v1") {
		t.Fatalf("unrelated profiles must be preserved:\n%s", raw)
	}
	if strings.Contains(raw, `base_url = ""`) {
		t.Fatalf("empty base_url remains after heal:\n%s", raw)
	}
	if !GrokProxyWired() {
		t.Fatal("GrokProxyWired = false after heal")
	}
}

func TestGrokProxyHealsOnlyPoisonedProfile(t *testing.T) {
	setGrokTestHome(t)
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	t.Setenv("TOKLESS_OPENCODE_GO_KEY", "test-key")

	foreign := "[model.headroom]\nmodel = \"deepseek-v4-flash\"\nbase_url = \"https://foreign.example/v1\"\napi_backend = \"chat_completions\"\nstream_tool_calls = false\napi_key = \"test-key\"\n"
	file := filepath.Join(dir, "config.toml")
	if err := util.WriteFile(file, foreign); err != nil {
		t.Fatal(err)
	}

	changed, _ := ConfigureGrokProxy()
	if changed {
		t.Fatal("ConfigureGrokProxy must refuse to overwrite foreign headroom profile")
	}
}

func TestGrokConfigUsesGrokHome(t *testing.T) {
	setGrokTestHome(t)
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)

	if got, want := grokConfigFile(), filepath.Join(dir, "config.toml"); got != want {
		t.Fatalf("grok config = %q, want %q", got, want)
	}
	if changed, file, err := ConfigureGrokMcp("codegraph"); err != nil || !changed || file != filepath.Join(dir, "config.toml") {
		t.Fatalf("ConfigureGrokMcp = %v, %q", changed, file)
	}
	if !GrokMcpHas("codegraph") {
		t.Fatal("codegraph MCP missing")
	}
}

func TestGrokContextModeMcpHasChecksOnlyContextModeBlock(t *testing.T) {
	setGrokTestHome(t)
	if err := util.WriteFile(grokConfigFile(), `[mcp_servers.context-mode]
command = "other"
# command = "tokless"
# args = ["run-mcp", "--context-mode", "context-mode"]

[mcp_servers.unrelated]
command = "tokless"
args = ["run-mcp", "--context-mode", "context-mode"]
`); err != nil {
		t.Fatal(err)
	}
	if GrokContextModeMcpHas() {
		t.Fatal("unrelated MCP block must not validate context-mode")
	}

	if changed, _, err := ConfigureGrokMcp("context-mode"); err != nil || !changed {
		t.Fatal("context-mode MCP was not configured")
	}
	if !GrokContextModeMcpHas() {
		raw, _ := util.ReadFileSafe(grokConfigFile())
		t.Fatalf("context-mode MCP missing:\n%s", raw)
	}
}

func TestGrokCodegraphMcpHasChecksOnlyCodegraphBlock(t *testing.T) {
	setGrokTestHome(t)
	if err := util.WriteFile(grokConfigFile(), `# [mcp_servers.codegraph]
# command = "tokless"
# args = ["run-mcp", "--agent", "grok", "codegraph", "serve", "--mcp"]
[mcp_servers.codegraph]
command = "other"

[mcp_servers.unrelated]
command = "tokless"
args = ["run-mcp", "--agent", "grok", "codegraph", "serve", "--mcp"]
`); err != nil {
		t.Fatal(err)
	}
	if GrokMcpHas("codegraph") {
		t.Fatal("comments and unrelated MCP block must not validate codegraph")
	}

	if changed, _, err := ConfigureGrokMcp("codegraph"); err != nil || !changed {
		t.Fatal("codegraph MCP was not configured")
	}
	if !GrokMcpHas("codegraph") {
		t.Fatal("codegraph MCP missing")
	}
}

func TestConfigureGrokMcpEnablesOnlyTargetServer(t *testing.T) {
	setGrokTestHome(t)
	if err := os.MkdirAll(filepath.Dir(grokConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `disabled_mcp_servers = ["codegraph", "other",]
unrelated = "keep"

[mcp_servers.codegraph]
command = "wrong"
enabled = false

[mcp_servers.other]
command = "other"
`
	if err := os.WriteFile(grokConfigFile(), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, _, err := ConfigureGrokMcp("codegraph"); err != nil || !changed {
		t.Fatalf("ConfigureGrokMcp = %v, %v", changed, err)
	}
	got, err := os.ReadFile(grokConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if grokMcpDisabled(string(got), "codegraph") || !strings.Contains(string(got), `disabled_mcp_servers = ["other"]`) ||
		!strings.Contains(string(got), "unrelated = \"keep\"") || !GrokMcpHas("codegraph") {
		t.Fatalf("unexpected Grok config:\n%s", got)
	}
}

func TestGrokMcpHasSupportsMultilineArgs(t *testing.T) {
	setGrokTestHome(t)
	if err := os.MkdirAll(filepath.Dir(grokConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grokConfigFile(), []byte(`[mcp_servers.codegraph]
command = "tokless"
args = [
  "run-mcp",
  "--agent",
  "grok",
  "codegraph",
  "serve",
  "--mcp",
]
enabled = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !GrokMcpHas("codegraph") {
		t.Fatal("multiline Grok args did not validate")
	}
}

func TestGrokMcpHasAcceptsCodegraphNpxFallback(t *testing.T) {
	setGrokTestHome(t)
	if err := os.MkdirAll(filepath.Dir(grokConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grokConfigFile(), []byte(`[mcp_servers.codegraph]
command = "tokless"
args = ["run-mcp", "--agent", "grok", "/opt/node/bin/npx", "--no-install", "@colbymchenry/codegraph", "serve", "--mcp"]
enabled = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !GrokMcpHas("codegraph") {
		t.Fatal("npx CodeGraph fallback did not validate")
	}
}

func TestGrokMcpArgsAcceptWindowsCmdWrapper(t *testing.T) {
	if !grokCodegraphArgs([]string{"run-mcp", "--agent", "grok", "cmd", "/c", `C:\\node\\npx.CMD`, "--no-install", "@colbymchenry/codegraph", "serve", "--mcp"}) {
		t.Fatal("wrapped npx CodeGraph args did not validate")
	}
	if !grokCodegraphArgs([]string{"run-mcp", "--agent", "grok", "cmd", "/c", `C:\\bin\\codegraph.cmd`, "serve", "--mcp"}) {
		t.Fatal("wrapped direct CodeGraph args did not validate")
	}
	if !grokContextModeArgs([]string{"run-mcp", "--context-mode", "cmd", "/c", `C:\\bin\\context-mode.CMD`}) {
		t.Fatal("wrapped Context Mode args did not validate")
	}
	if !grokContextModeArgs([]string{"run-mcp", "--context-mode", "/opt/node/bin/npx", "--no-install", "context-mode"}) {
		t.Fatal("npx Context Mode args did not validate")
	}
	if !grokContextModeArgs([]string{"run-mcp", "--context-mode", "cmd", "/c", `C:\\node\\npx.CMD`, "--no-install", "context-mode"}) {
		t.Fatal("wrapped npx Context Mode args did not validate")
	}
}

func TestGrokRtkHookInstallRemoveHas(t *testing.T) {
	setGrokTestHome(t)

	if err := InstallGrokRtkHook(); err != nil {
		t.Fatal(err)
	}
	raw, ok := util.ReadFileSafe(grokRtkHookPath())
	if !ok {
		t.Fatal("hook file missing after install")
	}
	if !strings.Contains(raw, `"PreToolUse"`) || !strings.Contains(raw, "rtk-hook grok") {
		t.Fatalf("managed hook content missing:\n%s", raw)
	}
	if !HasGrokRtkHook() {
		t.Fatal("HasGrokRtkHook = false after install")
	}

	foreign := filepath.Join(grokDir(), "hooks", "user-hooks.json")
	if err := util.WriteFile(foreign, `{"hooks":{}}`); err != nil {
		t.Fatal(err)
	}
	RemoveGrokRtkHook()
	if _, ok := util.ReadFileSafe(grokRtkHookPath()); ok {
		t.Fatal("managed hook file not removed")
	}
	if _, ok := util.ReadFileSafe(foreign); !ok {
		t.Fatal("foreign hooks must survive removal")
	}
	if HasGrokRtkHook() {
		t.Fatal("HasGrokRtkHook = true after removal")
	}
}

func TestGrokCodegraphSessionHookInstallHas(t *testing.T) {
	setGrokTestHome(t)
	if err := InstallGrokCodegraphSessionHook(); err != nil {
		t.Fatal(err)
	}
	raw, ok := util.ReadFileSafe(grokCodegraphSessionHookPath())
	if !ok {
		t.Fatal("session hook file missing after install")
	}
	if !strings.Contains(raw, `"SessionStart"`) || !strings.Contains(raw, "grok-hook session-start") {
		t.Fatalf("managed session hook content missing:\n%s", raw)
	}
	if !HasGrokCodegraphSessionHook() {
		t.Fatal("HasGrokCodegraphSessionHook = false after install")
	}
	RemoveGrokCodegraphSessionHook()
	if HasGrokCodegraphSessionHook() {
		t.Fatal("HasGrokCodegraphSessionHook = true after removal")
	}
}

func TestConfigureGrokMcpRejectsHeadroom(t *testing.T) {
	setGrokTestHome(t)
	if changed, _, err := ConfigureGrokMcp("headroom"); err == nil || changed {
		t.Fatalf("ConfigureGrokMcp(headroom) = (%v, %v), want unchanged error", changed, err)
	}
}

func TestGrokInstructionsUseGlobalAgentsMarkdown(t *testing.T) {
	setGrokTestHome(t)
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)

	want := filepath.Join(dir, "rules", "AGENTS.md")
	if got := GrokInstructionsFile(); got != want {
		t.Fatalf("GrokInstructionsFile = %q, want %q", got, want)
	}
}
