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

// --- in-place BYOK routing ---

const grokUserConfig = `[models]
default = "my-model"

[model_providers.myprov]
base_url = "https://provider.example/v1"
api_key = "sk-user-key"
api_backend = "chat_completions"

[model.my-model]
name = "My Model"
model = "deepseek-v4-flash"
model_provider = "myprov"
`

func grokSeedConfig(t *testing.T, content string) {
	t.Helper()
	setGrokTestHome(t)
	if err := util.WriteFile(grokConfigFile(), content); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureGrokProxyRewritesUserProviderInPlace(t *testing.T) {
	grokSeedConfig(t, grokUserConfig)

	changed, _ := ConfigureGrokProxy()
	if !changed {
		t.Fatal("configure did not change config")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	for _, want := range []string{
		`base_url = "` + ProxyEndpointFor("grok") + `"`,
		`x-headroom-base-url = "https://provider.example/v1"`,
		`x-headroom-original-path = "/chat/completions"`,
		`api_key = "sk-user-key"`,
		`api_backend = "chat_completions"`,
		`[model_providers.myprov]`,
		`model_provider = "myprov"`,
		`default = "my-model"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %q after wire:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "tokless") {
		t.Fatalf("tokless must never appear:\n%s", raw)
	}

	// Idempotent refresh.
	if changed2, _ := ConfigureGrokProxy(); changed2 {
		raw2, _ := util.ReadFileSafe(grokConfigFile())
		t.Fatalf("second run must be a no-op:\n%s", raw2)
	}
	if !GrokProxyWired() {
		t.Fatal("wired false after configure")
	}
}

func TestConfigureGrokProxyRefreshesStaleHeader(t *testing.T) {
	grokSeedConfig(t, grokUserConfig)
	if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
		t.Fatal("not wired")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	stale := strings.Replace(raw, "https://provider.example/v1", "https://provider.example/api", 1)
	if err := util.WriteFile(grokConfigFile(), stale); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureGrokProxy(); !changed {
		t.Fatal("stale header was not refreshed")
	}
	raw, _ = util.ReadFileSafe(grokConfigFile())
	if !strings.Contains(raw, `x-headroom-base-url = "https://provider.example/v1"`) {
		t.Fatalf("full upstream path not restored:\n%s", raw)
	}
}

func TestGrokProxyWiredRequiresManagedHeaders(t *testing.T) {
	for _, field := range []string{"x-headroom-base-url", "x-headroom-original-path"} {
		t.Run(field, func(t *testing.T) {
			grokSeedConfig(t, grokUserConfig)
			if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
				t.Fatal("not wired")
			}
			raw, _ := util.ReadFileSafe(grokConfigFile())
			line := "\t" + field + " = "
			if !strings.Contains(raw, line) {
				line = field + " = "
			}
			start := strings.Index(raw, line)
			if start < 0 {
				t.Fatalf("managed header %q missing before removal:\n%s", field, raw)
			}
			end := strings.IndexByte(raw[start:], '\n')
			if end < 0 {
				end = len(raw) - start
			}
			raw = raw[:start] + raw[start+end+1:]
			if err := util.WriteFile(grokConfigFile(), raw); err != nil {
				t.Fatal(err)
			}
			if GrokProxyWired() {
				t.Fatalf("wired with %q removed", field)
			}
		})
	}
}

func TestGrokProxyPreservesUserOwnedOriginalPathHeader(t *testing.T) {
	cfg := `[model_providers.custom]
base_url = "https://up.example/v1"
api_key = "sk-c"
extra_headers = { x-headroom-original-path = "/custom/chat/completions", x-tag = "t" }
`
	grokSeedConfig(t, cfg)
	if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
		t.Fatal("not wired")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if !strings.Contains(raw, `x-headroom-original-path = "/chat/completions"`) {
		t.Fatalf("managed original path missing:\n%s", raw)
	}
	if !RemoveGrokProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(grokConfigFile())
	if raw != cfg {
		t.Fatalf("user-owned original path not restored:\n%s", raw)
	}
}

func TestConfigureGrokProxySupportsQuotedProviderAndKeys(t *testing.T) {
	cfg := `[model_providers."my.provider"]
"base_url" = 'https://provider.example/v1' # keep
"api_key" = 'key#fragment'
`
	grokSeedConfig(t, cfg)
	if changed, _ := ConfigureGrokProxy(); !changed || !GrokProxyWired() {
		t.Fatal("quoted provider was not wired")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if !strings.Contains(raw, `"base_url" = "`+ProxyEndpointFor("grok")+`"`) ||
		!strings.Contains(raw, `"api_key" = 'key#fragment'`) {
		t.Fatalf("quoted provider fields changed incorrectly:\n%s", raw)
	}
}

func TestGrokProxySupportsQuotedExtraHeadersKey(t *testing.T) {
	cfg := `[model_providers.custom]
base_url = "https://up.example/v1"
api_key = "sk-c"
"extra_headers" = { x-tag = "keep" }
`
	grokSeedConfig(t, cfg)
	if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
		t.Fatal("quoted extra_headers key was not wired")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if strings.Count(raw, `"extra_headers"`) != 1 || !strings.Contains(raw, `x-headroom-base-url = "https://up.example/v1"`) {
		t.Fatalf("quoted extra_headers key was duplicated or not routed:\n%s", raw)
	}
	if !RemoveGrokProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(grokConfigFile())
	if raw != cfg {
		t.Fatalf("quoted extra_headers key not restored:\n%s", raw)
	}
}

func TestConfigureGrokProxyRestoresSingleQuotedBaseLine(t *testing.T) {
	cfg := `[model_providers.'my.provider']
base_url = 'https://provider.example/v1' # keep
api_key = 'key'

[model.m]
model = "m"
model_provider = "my.provider"
`
	grokSeedConfig(t, cfg)
	if _, _ = ConfigureGrokProxy(); !RemoveGrokProxy() {
		t.Fatal("quoted provider round trip failed")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if raw != cfg {
		t.Fatalf("base_url syntax not restored: got=%q want=%q", raw, cfg)
	}
}

func TestRemoveGrokProxyRestoresByteExact(t *testing.T) {
	grokSeedConfig(t, grokUserConfig)

	if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
		t.Fatal("not wired")
	}
	if !RemoveGrokProxy() {
		t.Fatal("remove reported no change")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if raw != grokUserConfig {
		t.Fatalf("restore not byte-exact:\n%s", raw)
	}
	if GrokProxyWired() {
		t.Fatal("still wired after remove")
	}
}

func TestWirePreservesCustomHeadersAndChildTable(t *testing.T) {
	cfg := `[model_providers.custom]
base_url = "https://up.example/v1"
api_key = "sk-c"
extra_headers = { x-client-tag = "keepme" }

[model_providers.custom.extra_headers_doc]
x-note = "doc"

[model.m]
name = "M"
model = "m1"
model_provider = "custom"
`
	grokSeedConfig(t, cfg)
	if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
		t.Fatal("not wired")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if !strings.Contains(raw, `x-headroom-base-url = "https://up.example/v1"`) ||
		!strings.Contains(raw, `x-client-tag = "keepme"`) ||
		!strings.Contains(raw, `[model_providers.custom.extra_headers_doc]`) {
		t.Fatalf("header merge broken:\n%s", raw)
	}
	if !RemoveGrokProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(grokConfigFile())
	if raw != cfg {
		t.Fatalf("restore not byte-exact with child tables:\n%s", raw)
	}
}

func TestConfigureRoutesAllProvidersAndPreservesUserNames(t *testing.T) {
	cfg := `[models]
default = "tokless"

[model_providers.a]
base_url = "https://a.example/v1"
api_key = "ka"

[model_providers.b]
base_url = "https://b.example/v1"
api_key = "kb"

[model.tokless]
name = "old"
model = "old"
model_provider = "tokless"
`
	grokSeedConfig(t, cfg)
	changed, _ := ConfigureGrokProxy()
	if !changed {
		t.Fatal("no change")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if !strings.Contains(raw, "[model.tokless]") || !strings.Contains(raw, `default = "tokless"`) {
		t.Fatalf("user-owned tokless names changed:\n%s", raw)
	}
	for _, id := range []string{"a", "b"} {
		if got := util.TomlBlockField(raw, "model_providers."+id, "base_url"); got != ProxyEndpointFor("grok") {
			t.Fatalf("provider %s base_url = %q", id, got)
		}
	}
	d := detectGrokProxy(ProxyCapabilities()["grok"])
	if d.State != ProxyStateManaged || !strings.Contains(d.Detail, "2 provider(s)") {
		t.Fatalf("detection = %s %s", d.State, d.Detail)
	}
}

func TestUserOwnedHeaderValueSurvivesRoundTrip(t *testing.T) {
	cfg := `[model_providers.custom]
base_url = "https://up.example/v1"
api_key = "sk-c"
extra_headers = { x-headroom-base-url = "https://my-own.example" , x-tag = "t" }

[model.m]
name = "M"
model = "m1"
model_provider = "custom"
`
	grokSeedConfig(t, cfg)
	if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
		t.Fatal("not wired")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if !strings.Contains(raw, `x-headroom-base-url = "https://up.example/v1"`) {
		t.Fatalf("routing header missing:\n%s", raw)
	}
	if !strings.Contains(raw, `x-tag = "t"`) {
		t.Fatal("user tag lost")
	}
	if !RemoveGrokProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(grokConfigFile())
	if raw != cfg {
		t.Fatalf("user-owned header not restored:\n%s", raw)
	}
}

func TestEmptyUserOwnedHeaderSurvivesRoundTrip(t *testing.T) {
	cfg := `[model_providers.custom]
base_url = "https://up.example/v1"
api_key = "sk-c"
extra_headers = { x-headroom-base-url = "" }
`
	grokSeedConfig(t, cfg)
	if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
		t.Fatal("not wired")
	}
	if !RemoveGrokProxy() {
		t.Fatal("remove failed")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if raw != cfg {
		t.Fatalf("empty user-owned header not restored:\n%s", raw)
	}
}

func TestIndentedChildHeaderKeysHandled(t *testing.T) {
	cfg := "[model_providers.custom]\nbase_url = \"https://up.example/v1\"\napi_key = \"sk-c\"\n\n[model_providers.custom.extra_headers]\n  x-note = \"doc\"\n"
	grokSeedConfig(t, cfg)
	if _, _ = ConfigureGrokProxy(); !GrokProxyWired() {
		t.Fatal("not wired")
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	if strings.Count(raw, "x-headroom-base-url") != 1 {
		t.Fatalf("expected exactly one routing header:\n%s", raw)
	}
	if !RemoveGrokProxy() {
		t.Fatal("remove failed")
	}
	raw, _ = util.ReadFileSafe(grokConfigFile())
	if strings.Contains(raw, headroomBaseURLHeader) || !strings.Contains(raw, `x-note = "doc"`) {
		t.Fatalf("indented child restore broken:\n%s", raw)
	}
}

func TestDetectStatesOAuthOnlyAndStale(t *testing.T) {
	grokSeedConfig(t, "[models]\ndefault = \"grok-4.6\"\n")
	if ProxyAgentApplicable("grok") {
		t.Fatal("oauth-only config must not be applicable")
	}
	d := detectGrokProxy(ProxyCapabilities()["grok"])
	if d.State != ProxyStateUnconfigured {
		t.Fatalf("state = %s (%s)", d.State, d.Detail)
	}

	// Stale: stash present, user hand-restored the route.
	grokSeedConfig(t, grokUserConfig)
	_, _ = ConfigureGrokProxy()
	stash := loadGrokStash()
	if len(stash) != 1 {
		t.Fatalf("stash entries = %d", len(stash))
	}
	raw, _ := util.ReadFileSafe(grokConfigFile())
	next := strings.Replace(raw, ProxyEndpointFor("grok"), "https://provider.example/v1", 1)
	if err := util.WriteFile(grokConfigFile(), next); err != nil {
		t.Fatal(err)
	}
	if GrokProxyWired() {
		t.Fatal("hand-restored route must read unwired")
	}
	d = detectGrokProxy(ProxyCapabilities()["grok"])
	if d.State != ProxyStateUnconfigured || !strings.Contains(d.Detail, "not routed") {
		t.Fatalf("opted-out state = %s (%s)", d.State, d.Detail)
	}
}

func TestRemoveBlockSparesArrayTables(t *testing.T) {
	setGrokTestHome(t)
	src := "[model.tokless]\nname = \"x\"\n\n[[items]]\nname = \"keep\"\n"
	if err := util.WriteFile(grokConfigFile(), src); err != nil {
		t.Fatal(err)
	}
	next := util.RemoveBlock(src, "model.tokless")
	if !strings.Contains(next, "[[items]]") || !strings.Contains(next, `name = "keep"`) {
		t.Fatalf("array table consumed by block removal:\n%q", next)
	}
}

func TestGrokProxyCapabilityIsAdditiveProvider(t *testing.T) {
	cap := ProxyCapabilities()["grok"]
	if cap.WireKind != ProxyWireAdditiveProvider {
		t.Fatalf("Grok wire kind = %s, want additive-provider/model", cap.WireKind)
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

func TestConfigureGrokMcpRejectsHeadroom(t *testing.T) {
	setGrokTestHome(t)
	if changed, _, err := ConfigureGrokMcp("headroom"); err == nil || changed {
		t.Fatalf("ConfigureGrokMcp(headroom) = (%v, %v), want unchanged error", changed, err)
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

func TestDetectGrokProxyUnreadableState(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits meaningless as root")
	}
	setGrokTestHome(t)
	if err := util.WriteFile(grokConfigFile(), "[models]\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(grokConfigFile(), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(grokConfigFile(), 0o644) })
	if got := DetectProxy("grok").State; got != ProxyStateUnreadable {
		t.Fatalf("state = %q, want unreadable", got)
	}
}
