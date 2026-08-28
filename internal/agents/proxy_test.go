package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

const proxyTestURL = "http://127.0.0.1:8787"

func pinToklessProxyEnv(t *testing.T) {
	t.Setenv("TOKLESS_PROXY_PROVIDER", "")
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "")
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "")
	t.Setenv("TOKLESS_HEADROOM_OPENAI_URL", "")
	t.Setenv("TOKLESS_PROXY_MODEL", "")
	t.Setenv("TOKLESS_PROXY_KEY", "")
}

func claudeProxyTestHome(t *testing.T) {
	t.Helper()
	pinToklessProxyEnv(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func TestClaudeProxyLifecycle(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings

	changed, file := ConfigureClaudeProxy()
	if !changed || file != settings {
		t.Fatalf("ConfigureClaudeProxy = %v, %q", changed, file)
	}
	if !ClaudeProxyWired() {
		t.Fatal("expected wired after configure")
	}
	if changed, _ := ConfigureClaudeProxy(); changed {
		t.Fatal("second configure should be a no-op")
	}
	raw, ok := util.ReadFileSafe(settings)
	if !ok || !strings.Contains(raw, `"ANTHROPIC_BASE_URL": "http://127.0.0.1:8787"`) {
		t.Fatalf("settings missing proxy env:\n%s", raw)
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected removal")
	}
	if ClaudeProxyWired() {
		t.Fatal("expected unwired after remove")
	}
	if RemoveClaudeProxy() {
		t.Fatal("second remove should be a no-op")
	}
}

func TestClaudeProxyConfigurePreservesOtherEnvKeys(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_API_KEY":"sk-test","OTHER":"keep"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected configure to write")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"sk-test"`) || !strings.Contains(raw, `"keep"`) {
		t.Fatalf("other env keys lost:\n%s", raw)
	}
}

func TestClaudeProxyPreservesUserValueThroughTakeover(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{
  "env": {
    "ANTHROPIC_API_KEY": "sk-test",
    "ANTHROPIC_BASE_URL": "http://user.example:9999"
  }
}
`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("foreign endpoint should be taken over, originals kept in stash")
	}
	stash, ok := loadClaudeBYOKStash()
	if !ok || stash.BaseURL != "http://user.example:9999" {
		t.Fatalf("user upstream not stashed: %+v ok=%v", stash, ok)
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"sk-test"`) {
		t.Fatalf("user env keys were clobbered:\n%s", raw)
	}
	if ClaudeProxyWired() != true {
		t.Fatal("takeover should report wired")
	}
	if !RemoveClaudeProxy() {
		t.Fatal("restore failed")
	}
	raw, _ = util.ReadFileSafe(settings)
	if !strings.Contains(raw, "http://user.example:9999") || !strings.Contains(raw, "sk-test") {
		t.Fatalf("user env was not restored:\n%s", raw)
	}
}

func TestClaudeProxyConfigureDoesNotPinModelOnRewire(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8787"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); changed {
		t.Fatal("already-wired Claude config should be a no-op")
	}
	raw, _ := util.ReadFileSafe(settings)
	if strings.Contains(raw, "ANTHROPIC_MODEL") {
		t.Fatalf("OAuth config must not receive model pin:\n%s", raw)
	}
	if !strings.Contains(raw, "ANTHROPIC_BASE_URL") || !strings.Contains(raw, "http://127.0.0.1:8787") {
		t.Fatalf("proxy endpoint missing:\n%s", raw)
	}
}

func TestClaudeProxyDoesNotRemovePreexistingProxyEndpoint(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8787"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); changed {
		t.Fatal("preexisting proxy endpoint should not be claimed")
	}
	if RemoveClaudeProxy() {
		t.Fatal("preexisting proxy endpoint should not be removed")
	}
	raw, _ := util.ReadFileSafe(settings)
	if raw != seed {
		t.Fatalf("preexisting endpoint changed:\n%s", raw)
	}
}

func TestClaudeProxyRefusesUnparseableConfig(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env": {"ANTHROPIC_API_KEY": "sk-test", `
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); changed {
		t.Fatal("configure must refuse an unparseable existing settings.json")
	}
	raw, _ := util.ReadFileSafe(settings)
	if raw != seed {
		t.Fatalf("unparseable config was clobbered:\n%s", raw)
	}
}

func TestDetectProxyReadOnlyStates(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8787"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("claude"); got.State != ProxyStateManaged {
		t.Fatalf("claude managed state = %s", got.State)
	}
	raw, _ := util.ReadFileSafe(settings)
	if raw != seed {
		t.Fatal("detection changed claude config")
	}

	if err := util.WriteFile(settings, `{"env":{"ANTHROPIC_BASE_URL":"http://user.example"}}`); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("claude"); got.State != ProxyStateForeignBYOK {
		t.Fatalf("claude foreign state = %s", got.State)
	}
	if err := util.WriteFile(settings, `{`); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("claude"); got.State != ProxyStateUnreadable {
		t.Fatalf("claude unreadable state = %s", got.State)
	}
	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("claude"); got.State != ProxyStateAbsent {
		t.Fatalf("claude absent state = %s", got.State)
	}
}

func TestDetectProxyUnreadableConfigIsNotAbsentAndDoesNotMutate(t *testing.T) {
	claudeProxyTestHome(t)
	path := util.ClaudeCodePaths().Settings
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("claude"); got.State != ProxyStateUnreadable {
		t.Fatalf("directory at config path = %s, want %s", got.State, ProxyStateUnreadable)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("detector mutated unreadable config path: %v", err)
	}
}

func TestDetectProxyConservativeStates(t *testing.T) {
	setTestHome(t)
	if got := DetectProxy("grok"); got.State != ProxyStateAbsent {
		t.Fatalf("grok absent state = %s", got.State)
	}
	if got := DetectProxy("grok"); got.State != ProxyStateAbsent {
		t.Fatalf("grok env must not affect config detection = %+v", got)
	}
	if got := DetectProxy("cline"); got.State != ProxyStateAbsent {
		t.Fatalf("cline state = %s", got.State)
	}
	if got := DetectProxy("antigravity"); got.State != ProxyStateAbsent || got.Capability.WireKind != ProxyWireManagedRoute ||
		got.Capability.Protocol != ProxyProtocolGeminiNative {
		t.Fatalf("antigravity = %+v", got)
	}
	if got := DetectProxy("arbitrary"); got.State != ProxyStateUnknown || got.Capability.ID != "arbitrary" {
		t.Fatalf("unsupported = %+v", got)
	}
}

func TestDetectOmpMalformedIsUnknown(t *testing.T) {
	ompProxyTestHome(t)
	if err := util.WriteFile(ompModelsFile(), "providers:\n  headroom:\n    baseUrl: [\n    apiKey: TOKLESS_OPENCODE_GO_KEY\n    api: openai-completions\n    discovery:\n      type: openai-models-list\n"); err != nil {
		t.Fatal(err)
	}
	got := DetectProxy("omp")
	if got.State != ProxyStateConflict {
		t.Fatalf("malformed OMP state = %s", got.State)
	}
	if strings.Contains(got.Detail, "http://") || strings.Contains(got.Detail, "user.example") {
		t.Fatalf("OMP detail contains endpoint: %q", got.Detail)
	}
	if got.State == ProxyStateForeignBYOK || got.State == ProxyStateUnconfigured {
		t.Fatal("malformed OMP was classified as a route")
	}
}

func TestDetectOmpManagedEndpointTextRemainsUnknown(t *testing.T) {
	ompProxyTestHome(t)
	raw := `providers:
  headroom:
    baseUrl: http://127.0.0.1:8787/v1
    apiKey: TOKLESS_OPENCODE_GO_KEY
    api: openai-completions
    discovery:
      type: openai-models-list
    broken: [
`
	if err := util.WriteFile(ompModelsFile(), raw); err != nil {
		t.Fatal(err)
	}
	got := DetectProxy("omp")
	if got.State != ProxyStateManaged {
		t.Fatalf("OMP malformed state = %s", got.State)
	}
	if strings.Contains(got.Detail, "http://") || strings.Contains(got.Detail, "127.0.0.1") {
		t.Fatalf("OMP detail contains endpoint: %q", got.Detail)
	}
}

func TestDetectCodexReservedConflict(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	if err := util.WriteFile(path, `model_provider = "headroom"
openai_base_url = "http://user.example"
[model_providers.headroom]
base_url = "http://user.example"
`); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("codex"); got.State != ProxyStateConflict {
		t.Fatalf("codex existing state = %s", got.State)
	}
	if err := util.WriteFile(path, `# model_provider = "headroom"
# [model_providers.headroom]
`); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("codex"); got.State == ProxyStateForeignBYOK {
		t.Fatal("codex comment substring classified as foreign")
	}
}

func TestDetectCodexRequiresExactManagedProviderBlock(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	endpoint := proxyTestURL + "/v1"
	cases := []struct {
		name  string
		block string
		state ProxyConfigState
	}{
		{name: "foreign block", block: `
[model_providers.headroom]
name = "Foreign proxy"
base_url = "` + endpoint + `"
wire_api = "responses"
experimental_bearer_token = "tokless"
supports_websockets = false
`, state: ProxyStateConflict},
		{name: "exact block", block: `
[model_providers.headroom]
wire_api = "responses" # text [model_providers.headroom] name = "wrong"
name = "Headroom persistent proxy"
base_url = "` + endpoint + `#not-a-comment"
`, state: ProxyStateConflict},
		{name: "exact desired block", block: `model_provider = "headroom"
openai_base_url = "` + endpoint + `"

` + codexProxySeed(false) + codexDesiredBlock(endpoint), state: ProxyStateManaged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `model_provider = "headroom"
openai_base_url = "` + endpoint + `"
` + tc.block
			if tc.name == "exact desired block" {
				raw = tc.block
			}
			if err := util.WriteFile(path, raw); err != nil {
				t.Fatal(err)
			}
			if got := DetectProxy("codex"); got.State != tc.state {
				t.Fatalf("codex state = %s, want %s", got.State, tc.state)
			}
		})
	}
}

func TestDetectCodexManagedEndpointTextRemainsUnknown(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	endpoint := ProxyEndpointFor("codex")
	raw := `malformed = [
model_provider = "headroom"
openai_base_url = "` + endpoint + `"
`
	if err := util.WriteFile(path, raw); err != nil {
		t.Fatal(err)
	}
	got := DetectProxy("codex")
	if got.State != ProxyStateUnconfigured {
		t.Fatalf("codex managed endpoint text state = %s", got.State)
	}
}

func TestDetectCodexIgnoresCommentedRootRoute(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	endpoint := ProxyEndpointFor("codex")
	raw := `# model_provider = "headroom"
# openai_base_url = "` + endpoint + `"
[model_providers.headroom]
name = "Headroom persistent proxy"
base_url = "` + endpoint + `"
supports_websockets = true
`
	if err := util.WriteFile(path, raw); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("codex"); got.State != ProxyStateConflict {
		t.Fatalf("codex state = %s, want %s", got.State, ProxyStateConflict)
	}
}

func codexProxyTestHome(t *testing.T) {
	t.Helper()
	pinToklessProxyEnv(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("CODEX_HOME", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func codexProxySeed(foreignBlock bool) string {
	if foreignBlock {
		return `[model_providers.headroom]
base_url = "http://user.example:9999/v1"

[features]
use_system_prompt_optimizer = true
`
	}
	return `[features]
use_system_prompt_optimizer = true
`
}

func TestCodexProxyIsManual(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	if err := util.WriteFile(path, `model_provider = "headroom"
openai_base_url = "`+ProxyEndpointFor("codex")+`"

`+codexProxySeed(false)+codexDesiredBlock(ProxyEndpointFor("codex"))); err != nil {
		t.Fatal(err)
	}
	if !ConfigureProxyAgent("codex") && !ProxyAgentWired("codex") {
		t.Fatal("codex proxy must be wired via managed route")
	}
	if got := DetectProxy("codex"); got.State != ProxyStateManaged || got.Capability.WireKind != ProxyWireManagedRoute {
		t.Fatalf("codex detection = %+v", got)
	}
	if !RemoveProxyAgent("codex") {
		t.Fatal("codex proxy remove failed")
	}
}

func codexDesiredBlock(endpoint string) string {
	return `
[model_providers.headroom]
name = "Headroom persistent proxy"
base_url = "` + endpoint + `"
wire_api = "responses"
supports_websockets = false
`
}

func TestConfigureCodexProxyUsesChatGPTOAuthWhenLoggedIn(t *testing.T) {
	codexProxyTestHome(t)
	if err := util.EnsureDir(util.CodexPathsResolved().Dir); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(filepath.Join(util.CodexPathsResolved().Dir, "auth.json"), `{"auth_mode":"chatgpt"}`); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureCodexProxy()
	if !changed {
		t.Fatal("Codex proxy configuration was not written")
	}
	raw, ok := util.ReadFileSafe(util.CodexPathsResolved().Config)
	if !ok {
		t.Fatal("Codex proxy configuration missing")
	}
	for _, want := range []string{
		`model_provider = "headroom"`,
		`openai_base_url = "` + ProxyEndpointFor("codex") + `"`,
		`requires_openai_auth = true`,
		`supports_websockets = false`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("Codex proxy config missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "experimental_bearer_token") || strings.Contains(raw, "env_key") {
		t.Fatalf("Codex config must preserve native OAuth auth:\n%s", raw)
	}
	if info, err := os.Stat(util.CodexPathsResolved().Config); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Codex config mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestCodexProxyRefusesSimilarForeignProvider(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	foreign := `model_provider = "headroom"
openai_base_url = "` + ProxyEndpointFor("codex") + `"

[model_providers.headroom]
name = "Headroom persistent proxy"
base_url = "` + ProxyEndpointFor("codex") + `"
user_option = true
`
	if err := util.WriteFile(path, foreign); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCodexProxy(); changed {
		t.Fatal("configure rewrote a similar foreign provider")
	}
	if RemoveCodexProxy() {
		t.Fatal("remove deleted a similar foreign provider")
	}
	raw, _ := util.ReadFileSafe(path)
	if raw != foreign {
		t.Fatalf("foreign provider changed:\n%s", raw)
	}
}

func TestConfigureCodexProxyMigratesHistoricalWebsocketFlavor(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	if err := util.WriteFile(filepath.Join(util.CodexPathsResolved().Dir, "auth.json"), `{"auth_mode":"chatgpt"}`); err != nil {
		t.Fatal(err)
	}
	historical := strings.Replace(codexProxySection(ProxyEndpointFor("codex"), nil), "supports_websockets = false", "supports_websockets = true", 1)
	if err := util.WriteFile(path, historical); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureCodexProxy()
	if !changed {
		t.Fatal("historical managed flavor was not migrated")
	}
	migrated, _ := util.ReadFileSafe(path)
	if !strings.Contains(migrated, "supports_websockets = false") || strings.Contains(migrated, "supports_websockets = true") {
		t.Fatalf("migration did not rewrite the managed block:\n%s", migrated)
	}
	if !RemoveCodexProxy() {
		t.Fatal("remove rejected a historical managed flavor")
	}
}

func TestCodexProxyRefusesEditedMarkedProvider(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	foreign := strings.Replace(codexProxySection(ProxyEndpointFor("codex"), nil), "supports_websockets = false", "supports_websockets = false\nuser_option = true", 1)
	if err := util.WriteFile(path, foreign); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCodexProxy(); changed {
		t.Fatal("configure rewrote an edited marked provider")
	}
	if RemoveCodexProxy() {
		t.Fatal("remove deleted an edited marked provider")
	}
}

func TestCodexProxyPreservesTUIRootExtras(t *testing.T) {
	codexProxyTestHome(t)
	if err := util.WriteFile(filepath.Join(util.CodexPathsResolved().Dir, "auth.json"), `{"auth_mode":"chatgpt"}`); err != nil {
		t.Fatal(err)
	}
	path := util.CodexPathsResolved().Config
	base := codexProxySection(ProxyEndpointFor("codex"), nil)
	withExtra := strings.Replace(base, `openai_base_url = "`+ProxyEndpointFor("codex")+`"`, "openai_base_url = \""+ProxyEndpointFor("codex")+"\"\nmodel_reasoning_effort = \"medium\"", 1)
	if err := util.WriteFile(path, withExtra); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureCodexProxy()
	if changed {
		t.Fatal("canonical-plus-extras should need no rewrite")
	}
	raw, _ := util.ReadFileSafe(path)
	if !strings.Contains(raw, "model_reasoning_effort = \"medium\"") || !strings.Contains(raw, codexMarkerStart) {
		t.Fatalf("extras lost on no-op configure:\n%s", raw)
	}
	if !RemoveCodexProxy() {
		t.Fatal("remove failed on canonical-plus-extras")
	}
	raw, _ = util.ReadFileSafe(path)
	if !strings.Contains(raw, "model_reasoning_effort = \"medium\"") {
		t.Fatalf("extras lost on removal:\n%s", raw)
	}
	if strings.Contains(raw, codexMarkerStart) {
		t.Fatalf("managed block survived removal:\n%s", raw)
	}
}

func TestConfigureCodexProxyMigratesHistoricalKeepingExtras(t *testing.T) {
	codexProxyTestHome(t)
	if err := util.WriteFile(filepath.Join(util.CodexPathsResolved().Dir, "auth.json"), `{"auth_mode":"chatgpt"}`); err != nil {
		t.Fatal(err)
	}
	path := util.CodexPathsResolved().Config
	historical := strings.Replace(codexProxySection(ProxyEndpointFor("codex"), nil), "supports_websockets = false", "supports_websockets = true", 1)
	withExtra := strings.Replace(historical, `openai_base_url = "`+ProxyEndpointFor("codex")+`"`, "openai_base_url = \""+ProxyEndpointFor("codex")+"\"\nnotify_favorite = \"x\"", 1)
	if err := util.WriteFile(path, withExtra); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureCodexProxy()
	if !changed {
		t.Fatal("historical flavor was not migrated")
	}
	raw, _ := util.ReadFileSafe(path)
	if strings.Contains(raw, "supports_websockets = true") || !strings.Contains(raw, "notify_favorite = \"x\"") {
		t.Fatalf("migration lost extras or kept old ws value:\n%s", raw)
	}
}

func TestCodexProxyRefusesUserEditedManagedKeys(t *testing.T) {
	codexProxyTestHome(t)
	if err := util.WriteFile(filepath.Join(util.CodexPathsResolved().Dir, "auth.json"), `{"auth_mode":"chatgpt"}`); err != nil {
		t.Fatal(err)
	}
	path := util.CodexPathsResolved().Config
	oauthBase := codexProxySection(ProxyEndpointFor("codex"), nil)
	byokBase := codexProxySection(ProxyEndpointFor("codex"), &openCodeBYOK{})
	cases := []struct {
		name        string
		base        string
		needle      string
		replacement string
	}{
		{"user flips oauth off", oauthBase, "requires_openai_auth = true", "requires_openai_auth = false"},
		{"user swaps env_key", byokBase, `env_key = "TOKLESS_CODEX_API_KEY"`, `env_key = "MY_USER_KEY"`},
		{"comment on managed line", oauthBase, "supports_websockets = false", "supports_websockets = false # user choice"},
	}
	for _, tc := range cases {
		name, needle, replacement := tc.name, tc.needle, tc.replacement
		foreign := strings.Replace(tc.base, needle, replacement, 1)
		if foreign == tc.base {
			t.Fatalf("%s: needle %q not found", name, needle)
		}
		if err := util.WriteFile(path, foreign); err != nil {
			t.Fatal(err)
		}
		before, _ := util.ReadFileSafe(path)
		if changed, _ := ConfigureCodexProxy(); changed {
			t.Fatalf("%s: configure rewrote user-edited managed key", name)
		}
		if RemoveCodexProxy() {
			t.Fatalf("%s: remove deleted user-edited marked provider", name)
		}
		after, _ := util.ReadFileSafe(path)
		if after != before {
			t.Fatalf("%s: bytes changed", name)
		}
	}
}

func TestConfigureCodexProxyRotatesManagedBearer(t *testing.T) {
	codexProxyTestHome(t)
	path := util.CodexPathsResolved().Config
	if err := util.WriteFile(path, codexProxySectionWithBearer(ProxyEndpointFor("codex"))); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureCodexProxy(); !changed {
		t.Fatal("configure did not migrate managed bearer")
	}
	raw, _ := util.ReadFileSafe(path)
	if strings.Contains(raw, "experimental_bearer_token") || !strings.Contains(raw, "supports_websockets = false") || !strings.Contains(raw, codexMarkerStart) {
		t.Fatalf("managed config was not migrated to native API-key auth:\n%s", raw)
	}
}

func openCodeProxySeed(foreign bool) string {
	if foreign {
		return `{"$schema": "https://opencode.ai/config.json",
  "provider": {
    "headroom": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "User Proxy",
      "options": {"baseURL": "http://user.example:9999/v1"},
      "models": {}
    }
  }
}
`
	}
	return `{"$schema": "https://opencode.ai/config.json", "theme": "dark"}
`
}

func opencodeProxyTestHome(t *testing.T) {
	t.Helper()
	pinToklessProxyEnv(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.WriteFile(openCodeTransportPluginPath(), "export default async () => ({})\n"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodeProxyScenarios(t *testing.T) {
	byokSeed := `{"$schema": "https://opencode.ai/config.json", "theme": "dark", "provider": {"prov-a": {"npm": "@ai-sdk/openai-compatible", "options": {"baseURL": "https://api.provider-a.test/v1", "apiKey": "prov-a-key"}, "models": {"m1": {"name": "M1"}}}}}`
	cases := []proxyScenario{
		{name: "wire installs transport plugin without rewriting BYOK", seed: byokSeed,
			wantChange: true, wantWired: true, wantContains: []string{
				`"prov-a"`,
				`"baseURL": "https://api.provider-a.test/v1"`,
				`"plugin"`,
				`"proxyUrl": "http://127.0.0.1:8787"`,
				`"theme": "dark"`,
			}},
		{name: "second inject idempotent", seed: byokSeed, preConfigure: true,
			wantChange: false, wantWired: true},
		{name: "no BYOK still routes native and future providers", seed: openCodeProxySeed(false),
			wantChange: true, wantWired: true},
		{name: "foreign headroom preserved", seed: openCodeProxySeed(true),
			wantChange: true, wantWired: true, keepContains: []string{"User Proxy", "user.example"}},
		{name: "refuses non-map provider", seed: `{"provider": "junk"}
`,
			wantChange: true, wantWired: true, keepContains: []string{`"junk"`}},
		{name: "remove preserves BYOK baseURL", seed: byokSeed, preConfigure: true,
			remove: true, wantRemoved: true, wantWired: false, wantContains: []string{"provider-a.test"}, wantAbsent: []string{"127.0.0.1:8787"}},
		{name: "remove leaves foreign provider", seed: openCodeProxySeed(true), preConfigure: true,
			remove: true, wantRemoved: true, wantWired: false, keepContains: []string{"User Proxy", "user.example"}},
		{name: "absent file receives plugin", seed: "",
			wantChange: true, wantWired: true},
	}
	runProxyScenarios(t, cases, opencodeProxyTestHome,
		ConfigureOpenCodeProxy, RemoveOpenCodeProxy, OpenCodeProxyWired, func() string { return util.OpenCodePathsResolved().Config })
}

func ompProxyTestHome(t *testing.T) {
	t.Helper()
	pinToklessProxyEnv(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("PI_CONFIG_DIR", "")
	t.Setenv("OMP_PROFILE", "")
	t.Setenv("PI_PROFILE", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func ompProxySeed(foreign bool) string {
	if foreign {
		return `providers:
  headroom:
    baseUrl: http://user.example:9999/v1
    apiKey: USER_KEY
    api: openai-completions
    discovery:
      type: openai-models-list
models:
  claude-sonnet:
    id: claude-sonnet
`
	}
	return `models:
  claude-sonnet:
    id: claude-sonnet
providers:
  headroom:
    baseUrl: http://127.0.0.1:8787/v1
    apiKey: TOKLESS_OPENCODE_GO_KEY
    api: openai-completions
    discovery:
      type: openai-models-list
`
}

func TestOmpProxyScenarios(t *testing.T) {
	ompProxyTestHome(t)
	models, config := ompModelsFile(), ompConfigFile()
	if err := util.WriteFile(models, ompProxySeed(false)); err != nil {
		t.Fatal(err)
	}
	if changed, file := ConfigureOmpProxy(); !changed || file != models {
		t.Fatalf("ConfigureOmpProxy = %v, %q", changed, file)
	}
	if !OmpProxyWired() {
		t.Fatal("expected wired after configure")
	}
	modelRaw := readProxyTestFile(t, models)
	for _, want := range []string{"  headroom:", "    baseUrl: http://127.0.0.1:8787/v1", "    apiKey: TOKLESS_OPENCODE_GO_KEY", "    api: openai-completions", "    discovery:", "      type: openai-models-list", "claude-sonnet"} {
		if !strings.Contains(modelRaw, want) {
			t.Fatalf("models.yml missing %q:\n%s", want, modelRaw)
		}
	}
	configRaw := readProxyTestFile(t, config)
	if !strings.Contains(configRaw, "modelRoles:") || !strings.Contains(configRaw, "  default: "+ompRoleTarget()) {
		t.Fatalf("config.yml missing managed default:\n%s", configRaw)
	}
	if changed, _ := ConfigureOmpProxy(); changed {
		t.Fatal("second configure should be a no-op")
	}
	if !RemoveOmpProxy() || OmpProxyWired() || RemoveOmpProxy() {
		t.Fatal("remove lifecycle failed")
	}

	ompProxyTestHome(t)
	if err := util.WriteFile(models, ompProxySeed(false)); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(config, "modelRoles:\n  default: user/model\n  fallback: keep\n"); err != nil {
		t.Fatal(err)
	}
	ConfigureOmpProxy()
	RemoveOmpProxy()
	configRaw = readProxyTestFile(t, config)
	if !strings.Contains(configRaw, "default: user/model") || !strings.Contains(configRaw, "fallback: keep") {
		t.Fatalf("prior role or sibling key not restored:\n%s", configRaw)
	}

	ompProxyTestHome(t)
	if err := util.WriteFile(models, ompProxySeed(true)); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); changed || OmpProxyWired() {
		t.Fatal("foreign headroom provider must block configure")
	}
	if got := readProxyTestFile(t, models); !strings.Contains(got, "user.example") || !strings.Contains(got, "claude-sonnet") {
		t.Fatalf("foreign provider config changed:\n%s", got)
	}
}

func TestPiProxyPreservesNativeProvider(t *testing.T) {
	piProxyTestHome(t)
	raw := `{"providers":{"qwen":{"api":"openai-completions","baseUrl":"https://dashscope.aliyuncs.com/compatible-mode/v1","apiKey":"user-key","headers":{"x-user":"keep"},"models":[{"id":"deepseek-v4-flash"}]}}}`
	if err := util.WriteFile(piModelsFile(), raw); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigurePiProxy(); !changed || !PiProxyWired() {
		t.Fatal("native Pi provider not wired")
	}
	got, _ := util.ReadFileSafe(piModelsFile())
	for _, want := range []string{"\"apiKey\": \"user-key\"", "\"id\": \"deepseek-v4-flash\"", "\"x-user\": \"keep\"", "\"baseUrl\": \"http://127.0.0.1:8787/v1\"", "\"x-headroom-base-url\": \"https://dashscope.aliyuncs.com/compatible-mode\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("Pi route missing %q:\n%s", want, got)
		}
	}
	if changed, _ := ConfigurePiProxy(); changed {
		t.Fatal("Pi native route not idempotent")
	}
	if !RemovePiProxy() || PiProxyWired() {
		t.Fatal("Pi native route not removed")
	}
	got, _ = util.ReadFileSafe(piModelsFile())
	if got != util.StringifyJSON(util.TryParseJsonc(raw)) {
		t.Fatalf("Pi provider not restored:\n%s", got)
	}
}

func TestOmpProxyPreservesNativeProviderAndRole(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  qwen:
    baseUrl: https://dashscope.aliyuncs.com/compatible-mode/v1
    apiKey: user-key
    api: openai-completions
    headers:
      x-user: keep
modelRoles:
  default: qwen/deepseek-v4-flash:high
`
	if err := util.WriteFile(ompModelsFile(), models); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed || !OmpProxyWired() {
		t.Fatal("native OMP provider not wired")
	}
	got, _ := util.ReadFileSafe(ompModelsFile())
	for _, want := range []string{"baseUrl: http://127.0.0.1:8787/v1", "apiKey: user-key", "x-user: keep", "x-headroom-base-url: https://dashscope.aliyuncs.com/compatible-mode"} {
		if !strings.Contains(got, want) {
			t.Fatalf("OMP route missing %q:\n%s", want, got)
		}
	}
	config, _ := util.ReadFileSafe(ompConfigFile())
	if config != "" {
		t.Fatalf("OMP changed default role:\n%s", config)
	}
	if !RemoveOmpProxy() || OmpProxyWired() {
		t.Fatal("OMP native route not removed")
	}
	got, _ = util.ReadFileSafe(ompModelsFile())
	if !strings.Contains(got, "baseUrl: https://dashscope.aliyuncs.com/compatible-mode/v1") || strings.Contains(got, "x-headroom-base-url") {
		t.Fatalf("OMP provider not restored:\n%s", got)
	}
}

func readProxyTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		t.Fatalf("cannot read %s", path)
	}
	return raw
}

func TestOmpProxyConfigureDoesNotInsertUnderOpenAISibling(t *testing.T) {
	ompProxyTestHome(t)
	raw := `providers:
  headroom:
    name: Headroom
  openai:
    baseUrl: https://api.openai.com/v1
models:
  claude-sonnet:
    id: claude-sonnet
`
	if err := util.WriteFile(ompModelsFile(), raw); err != nil {
		t.Fatal(err)
	}
	ConfigureOmpProxy()
	got, ok := util.ReadFileSafe(ompModelsFile())
	if !ok {
		t.Fatal("models.yml missing")
	}
	if !strings.Contains(got, "  headroom:\n    name: Headroom\n    apiKey: TOKLESS_OPENCODE_GO_KEY") {
		t.Fatalf("headroom provider not updated:\n%s", got)
	}
	if strings.Contains(got, "    baseUrl: "+proxyTestURL+"\nmodels:") {
		t.Fatalf("baseUrl inserted outside provider mapping:\n%s", got)
	}
	if !strings.Contains(got, "  openai:\n    baseUrl: https://api.openai.com/v1") {
		t.Fatalf("openai sibling changed:\n%s", got)
	}
}

func TestOmpProxyRemoveChainsModelTransforms(t *testing.T) {
	ompProxyTestHome(t)
	raw := `providers:
  headroom:
    baseUrl: ` + proxyTestURL + `/v1
    apiKey: TOKLESS_OPENCODE_GO_KEY
    api: openai-completions
    discovery:
      type: openai-models-list
  anthropic:
    baseUrl: ` + proxyTestURL + `
models:
  claude-sonnet:
    id: claude-sonnet
`
	if err := util.WriteFile(ompModelsFile(), raw); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("prereq configure did not change")
	}
	if !RemoveOmpProxy() {
		t.Fatal("remove did not report change")
	}
	got := readProxyTestFile(t, ompModelsFile())
	if strings.Contains(got, "  headroom:\n") {
		t.Fatalf("headroom provider remains:\n%s", got)
	}
	if strings.Contains(got, "    baseUrl: "+proxyTestURL+"\n") {
		t.Fatalf("legacy anthropic baseUrl remains:\n%s", got)
	}
}

func TestOmpProxyConfigureDoesNotDuplicateDiscovery(t *testing.T) {
	ompProxyTestHome(t)
	if err := util.WriteFile(ompModelsFile(), ompProxySeed(false)); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("first configure did not change")
	}
	if changed, _ := ConfigureOmpProxy(); changed {
		t.Fatal("second configure should be no-op")
	}
	got := readProxyTestFile(t, ompModelsFile())
	if strings.Count(got, "    discovery:\n") != 1 || strings.Count(got, "      type: openai-models-list\n") != 1 {
		t.Fatalf("discovery duplicated:\n%s", got)
	}
}

func TestOmpProxyRemoveDoesNotDeleteOpenAISiblingBaseURL(t *testing.T) {
	ompProxyTestHome(t)
	raw := `providers:
  anthropic:
    name: Anthropic
  openai:
    baseUrl: https://api.openai.com/v1
models:
  claude-sonnet:
    id: claude-sonnet
`
	if err := util.WriteFile(ompModelsFile(), raw); err != nil {
		t.Fatal(err)
	}
	if RemoveOmpProxy() {
		t.Fatal("remove must not delete unconfigured providers")
	}
	if OmpProxyWired() {
		t.Fatal("headroom without managed fields must not report wired")
	}
	got, ok := util.ReadFileSafe(ompModelsFile())
	if !ok {
		t.Fatal("models.yml missing")
	}
	if got != raw {
		t.Fatalf("openai sibling changed:\n%s", got)
	}
}

func TestOmpProxyIgnoresNestedAnthropicProvider(t *testing.T) {
	ompProxyTestHome(t)
	raw := `providers:
  openai:
    models:
      anthropic:
        baseUrl: http://nested.example:9999
models:
  claude-sonnet:
    id: claude-sonnet
`
	if err := util.WriteFile(ompModelsFile(), raw); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("expected direct anthropic provider insertion")
	}
	got, _ := util.ReadFileSafe(ompModelsFile())
	if !strings.Contains(got, "  headroom:\n    baseUrl: "+proxyTestURL+"/v1") {
		t.Fatalf("direct headroom provider missing:\n%s", got)
	}
	if !strings.Contains(got, "      anthropic:\n        baseUrl: http://nested.example:9999") {
		t.Fatalf("nested anthropic sibling changed:\n%s", got)
	}
}

func TestOmpProxyRemoveReportsWriteFailure(t *testing.T) {
	ompProxyTestHome(t)
	if err := util.WriteFile(ompModelsFile(), ompProxySeed(false)); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("prereq configure did not change")
	}
	oldWrite := ompWriteFile
	ompWriteFile = func(string, string) error { return os.ErrPermission }
	t.Cleanup(func() { ompWriteFile = oldWrite })
	if !RemoveOmpProxy() {
		t.Fatal("remove should restore role state even when model write fails")
	}
	if !OmpProxyWired() {
		t.Fatal("proxy wiring changed despite write failure")
	}
}

// TestOmpProxyHonorsPiCodingAgentDir verifies models.yml follows the relocated
// omp agent dir.
func TestOmpProxyHonorsPiCodingAgentDir(t *testing.T) {
	home := t.TempDir()
	relocated := filepath.Join(home, "custom-agent")
	util.SetHomeOverride(filepath.Join(home, "fake-home"))
	t.Setenv("PI_CODING_AGENT_DIR", relocated)
	t.Setenv("PI_CONFIG_DIR", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.WriteFile(ompModelsFile(), "models:\n  x: y\n"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(ompConfigFile(), "modelRoles:\n  default: "+ompRoleTarget()+"\n"); err != nil {
		t.Fatal(err)
	}
	changed, file := ConfigureOmpProxy()
	if !changed || file != filepath.Join(relocated, "models.yml") {
		t.Fatalf("ConfigureOmpProxy = %v, %q", changed, file)
	}
	if !strings.Contains(readProxyTestFile(t, ompModelsFile()), "  headroom:") {
		t.Fatal("expected headroom provider in relocated dir")
	}
}

// proxyScenario is one behavior check for a proxy writer trio. seed "" means
// "no config file present" unless absent is set; absent marks expectation that
// the file must not be created.
type proxyScenario struct {
	name         string
	seed         string
	absent       bool
	preConfigure bool
	remove       bool
	wantChange   bool
	wantRemoved  bool
	wantWired    bool
	wantContains []string
	wantAbsent   []string
	keepContains []string
}

func runProxyScenarios(t *testing.T, cases []proxyScenario,
	home func(t *testing.T),
	configure func() (bool, string),
	remove func() bool,
	wired func() bool,
	file func() string,
) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home(t)
			path := file()
			if c.seed != "" {
				if err := util.WriteFile(path, c.seed); err != nil {
					t.Fatal(err)
				}
			}
			if c.preConfigure {
				if changed, _ := configure(); !changed {
					t.Fatal("prereq configure did not change")
				}
			}
			if c.remove {
				if got := remove(); got != c.wantRemoved {
					t.Fatalf("remove = %v, want %v", got, c.wantRemoved)
				}
			} else {
				changed, _ := configure()
				if changed != c.wantChange {
					t.Fatalf("configure changed = %v, want %v", changed, c.wantChange)
				}
				if c.absent && util.Exists(path) {
					t.Fatalf("configure created absent file %s", path)
				}
			}
			if got := wired(); got != c.wantWired {
				t.Fatalf("wired = %v, want %v", got, c.wantWired)
			}
			if len(c.wantContains) > 0 {
				raw, ok := util.ReadFileSafe(path)
				if !ok {
					t.Fatalf("cannot read %s", path)
				}
				for _, want := range c.wantContains {
					if !strings.Contains(raw, want) {
						t.Fatalf("missing %q in:\n%s", want, raw)
					}
				}
			}
			if len(c.wantAbsent) > 0 {
				raw, ok := util.ReadFileSafe(path)
				if !ok {
					t.Fatalf("cannot read %s", path)
				}
				for _, absent := range c.wantAbsent {
					if strings.Contains(raw, absent) {
						t.Fatalf("unexpected %q in:\n%s", absent, raw)
					}
				}
			}
			if len(c.keepContains) > 0 {
				raw, _ := util.ReadFileSafe(path)
				for _, want := range c.keepContains {
					if !strings.Contains(raw, want) {
						t.Fatalf("foreign value %q lost:\n%s", want, raw)
					}
				}
			}
		})
	}
}

func TestAntigravityProxyLifecycle(t *testing.T) {
	setTestHome(t)
	envFile := antigravityEnvFile()

	changed, file := ConfigureAntigravityProxy()
	if !changed || file != envFile {
		t.Fatalf("ConfigureAntigravityProxy = %v, %q", changed, file)
	}
	if !AntigravityProxyWired() {
		t.Fatal("expected wired after configure")
	}
	if changed, _ := ConfigureAntigravityProxy(); changed {
		t.Fatal("second configure should be a no-op")
	}
	raw, ok := util.ReadFileSafe(envFile)
	if !ok || !strings.Contains(raw, "GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787") {
		t.Fatalf("env file missing proxy line:\n%s", raw)
	}
	if !strings.Contains(raw, "CLOUD_CODE_URL=http://127.0.0.1:8787") {
		t.Fatalf("env file missing CLOUD_CODE_URL line:\n%s", raw)
	}
	if !RemoveAntigravityProxy() {
		t.Fatal("expected removal")
	}
	if AntigravityProxyWired() {
		t.Fatal("expected unwired after remove")
	}
	if RemoveAntigravityProxy() {
		t.Fatal("second remove should be a no-op")
	}
}

func TestAntigravityProxyPreservesForeignEnv(t *testing.T) {
	setTestHome(t)
	envFile := antigravityEnvFile()
	seed := "# user env\nGOOGLE_API_KEY=secret\n"
	if err := util.WriteFile(envFile, seed); err != nil {
		t.Fatal(err)
	}
	if _, _ = ConfigureAntigravityProxy(); !AntigravityProxyWired() {
		t.Fatal("expected wired")
	}
	raw, _ := util.ReadFileSafe(envFile)
	if !strings.Contains(raw, "GOOGLE_API_KEY=secret") {
		t.Fatalf("foreign env lost:\n%s", raw)
	}
	if !RemoveAntigravityProxy() {
		t.Fatal("expected removal")
	}
	raw, _ = util.ReadFileSafe(envFile)
	if strings.Contains(raw, "GOOGLE_GEMINI_BASE_URL") {
		t.Fatalf("proxy line survived removal:\n%s", raw)
	}
	if !strings.Contains(raw, "GOOGLE_API_KEY=secret") {
		t.Fatalf("foreign env lost after removal:\n%s", raw)
	}
}

func TestAntigravityProxyDoesNotOverwriteForeignRoutes(t *testing.T) {
	setTestHome(t)
	t.Setenv(antigravityProxyEnvKey, "https://user.example/gemini")
	t.Setenv(antigravityCloudCodeKey, "https://user.example/cloud")
	envFile := antigravityEnvFile()
	seed := antigravityProxyEnvKey + "=https://user.example/gemini\n" + antigravityCloudCodeKey + "=https://user.example/cloud\n"
	if err := util.WriteFile(envFile, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureAntigravityProxy(); changed {
		t.Fatal("foreign routes must not be overwritten")
	}
	raw, _ := util.ReadFileSafe(envFile)
	if raw != seed || os.Getenv(antigravityProxyEnvKey) != "https://user.example/gemini" ||
		os.Getenv(antigravityCloudCodeKey) != "https://user.example/cloud" {
		t.Fatalf("foreign routes changed: file=%q env=%q/%q", raw, os.Getenv(antigravityProxyEnvKey), os.Getenv(antigravityCloudCodeKey))
	}
}

func TestDetectAntigravityManagedAndForeign(t *testing.T) {
	setTestHome(t)
	t.Setenv(antigravityCloudCodeKey, "")
	t.Setenv(antigravityProxyEnvKey, "")
	envFile := antigravityEnvFile()
	if got := DetectProxy("antigravity"); got.State != ProxyStateAbsent {
		t.Fatalf("absent = %+v", got)
	}
	if _, _ = ConfigureAntigravityProxy(); !AntigravityProxyWired() {
		t.Fatal("expected wired")
	}
	// Configure sets process env; clear to assert file-only managed detail.
	t.Setenv(antigravityCloudCodeKey, "")
	t.Setenv(antigravityProxyEnvKey, "")
	if got := DetectProxy("antigravity"); got.State != ProxyStateManaged {
		t.Fatalf("managed = %+v", got)
	} else if !strings.Contains(got.Detail, "shell/user env wired") {
		t.Fatalf("managed detail should note shell/user env: %+v", got)
	}
	t.Setenv(antigravityCloudCodeKey, "http://127.0.0.1:8787")
	t.Setenv(antigravityProxyEnvKey, "http://127.0.0.1:8787")
	if got := DetectProxy("antigravity"); got.State != ProxyStateManaged ||
		!strings.Contains(got.Detail, "session env routes") {
		t.Fatalf("managed with env = %+v", got)
	}
	if err := util.WriteFile(envFile, "GOOGLE_GEMINI_BASE_URL=http://user.example\n"); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("antigravity"); got.State != ProxyStateForeignBYOK {
		t.Fatalf("foreign = %+v", got)
	}
}

func TestOmpRoutePreservesOriginalLineBytes(t *testing.T) {
	ompProxyTestHome(t)
	original := `providers:
  qwen:
    baseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1"  # primary
    apiKey: user-key
    api: openai-completions
    headers:
      x-user-header: keep-me
`
	file := ompModelsFile()
	if err := util.WriteFile(file, original); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("expected first wire to change config")
	}
	raw, _ := util.ReadFileSafe(file)
	for _, want := range []string{`# primary`, `"`, `x-user-header: keep-me`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("rewritten config lost %q:\n%s", want, raw)
		}
	}
	if !RemoveOmpProxy() {
		t.Fatal("expected restore to change config")
	}
	raw, _ = util.ReadFileSafe(file)
	if raw != original {
		t.Fatalf("restore not byte-identical:\n got: %q\nwant: %q", raw, original)
	}
}

func TestOmpSkipsFlowStyleHeaders(t *testing.T) {
	ompProxyTestHome(t)
	original := `providers:
  qwen:
    baseUrl: https://dashscope.aliyuncs.com/compatible-mode/v1
    apiKey: k
    api: openai-completions
    headers: {x-user: keep}
`
	file := ompModelsFile()
	if err := util.WriteFile(file, original); err != nil {
		t.Fatal(err)
	}
	ConfigureOmpProxy()
	raw, _ := util.ReadFileSafe(file)
	block := `  qwen:
    baseUrl: https://dashscope.aliyuncs.com/compatible-mode/v1
    apiKey: k
    api: openai-completions
    headers: {x-user: keep}
`
	if !strings.Contains(raw, block) {
		t.Fatalf("flow-style provider block must stay byte-identical:\n%s", raw)
	}
}

func TestWiredRequiresEveryStashEntry(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  alpha:
    baseUrl: https://alpha.example/v1
    apiKey: a
    api: openai-completions
  beta:
    baseUrl: https://beta.example/v1
    apiKey: b
    api: openai-completions
`
	file := ompModelsFile()
	if err := util.WriteFile(file, models); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("expected wire")
	}
	if !OmpProxyWired() {
		t.Fatal("both routed should be wired")
	}
	raw, _ := util.ReadFileSafe(file)
	filtered := `providers:
  alpha:
    baseUrl: http://127.0.0.1:8787/v1
    apiKey: a
    api: openai-completions
    headers:
      x-headroom-base-url: https://alpha.example
`
	if err := util.WriteFile(file, filtered); err != nil {
		t.Fatal(err)
	}
	raw, _ = util.ReadFileSafe(file)
	_ = raw
	if OmpProxyWired() {
		t.Fatal("wired must be false when one stashed provider was deleted")
	}
	if changed, _ := ConfigureOmpProxy(); changed {
		t.Fatal("re-up must not rewrite when remaining providers already routed")
	}
	stash := loadProxyRouteStash("omp")
	if _, exists := stash["beta"]; exists {
		t.Fatal("deleted provider must be pruned from stash")
	}
	if !OmpProxyWired() {
		t.Fatal("after pruning, remaining route should be wired")
	}
}

func TestNoLegacyFallbackWhileStashExists(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  other:
    baseUrl: https://x.example/v1
    apiKey: k
    api: unsupported-api
`
	file := ompModelsFile()
	if err := util.WriteFile(file, models); err != nil {
		t.Fatal(err)
	}
	if err := saveProxyRouteStash("omp", map[string]proxyRouteStashEntry{
		"gone": {Provider: "gone", BaseURL: "https://gone.example/v1", Upstream: "https://gone.example"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = saveProxyRouteStash("omp", nil) })
	changed, _ := ConfigureOmpProxy()
	if changed {
		t.Fatal("must not inject legacy headroom provider while stash unresolved")
	}
	raw, _ := util.ReadFileSafe(file)
	if strings.Contains(raw, "headroom") || !strings.Contains(raw, "unsupported-api") {
		t.Fatalf("config must stay untouched:\n%s", raw)
	}
}

func TestPiWiredRequiresEveryStashEntry(t *testing.T) {
	piProxyTestHome(t)
	cfg := util.NewOrderedMap()
	providers := util.NewOrderedMap()
	for _, id := range []string{"alpha", "beta"} {
		p := piProxyProviderEntry("https://" + id + ".example/v1")
		p.Set("baseUrl", "http://127.0.0.1:8787/v1")
		headers := util.NewOrderedMap()
		headers.Set(headroomBaseURLHeader, "https://"+id+".example")
		p.Set("headers", headers)
		providers.Set(id, p)
	}
	cfg.Set("providers", providers)
	file := piModelsFile()
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}
	stash := map[string]proxyRouteStashEntry{}
	for _, id := range []string{"alpha", "beta"} {
		stash[id] = proxyRouteStashEntry{Provider: id, BaseURL: "https://" + id + ".example/v1", Upstream: "https://" + id + ".example"}
	}
	if err := saveProxyRouteStash("pi", stash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = saveProxyRouteStash("pi", nil) })
	if !PiProxyWired() {
		t.Fatal("all entries matched should be wired")
	}
	providers.Delete("beta")
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}
	if PiProxyWired() {
		t.Fatal("wired must be false after provider deletion")
	}
}

func TestSaveProxyRouteStashAtomicNoTempLeftover(t *testing.T) {
	ompProxyTestHome(t)
	if err := saveProxyRouteStash("omp", map[string]proxyRouteStashEntry{
		"x": {Provider: "x"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(proxyRouteStashPath("omp") + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp stash file must not survive successful write")
	}
}

func TestOmpSpliceTargetsValueNotComment(t *testing.T) {
	ompProxyTestHome(t)
	original := `providers:
  qwen:
    baseUrl: https://a.example/v1  # see https://a.example/v1/docs
    apiKey: k
    api: openai-completions
`
	file := ompModelsFile()
	if err := util.WriteFile(file, original); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("expected wire")
	}
	raw, _ := util.ReadFileSafe(file)
	if !strings.Contains(raw, "    baseUrl: http://127.0.0.1:8787/v1  # see https://a.example/v1/docs") {
		t.Fatalf("value splice hit wrong occurrence:\n%s", raw)
	}
	if !RemoveOmpProxy() {
		t.Fatal("expected restore")
	}
	raw, _ = util.ReadFileSafe(file)
	if raw != original {
		t.Fatalf("restore not byte-identical:\n got: %q\nwant: %q", raw, original)
	}
}

func TestStashPrunesWhenAllProvidersDeleted(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  solo:
    baseUrl: https://solo.example/v1
    apiKey: k
    api: openai-completions
`
	file := ompModelsFile()
	if err := util.WriteFile(file, models); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("expected wire")
	}
	if err := util.WriteFile(file, "providers: {}\n"); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); changed {
		t.Fatal("no-native run must not touch config")
	}
	stash := loadProxyRouteStash("omp")
	if len(stash) != 0 {
		t.Fatalf("all-deleted providers must be pruned, got %v", stash)
	}
}

func TestStashFileIsPrivate(t *testing.T) {
	ompProxyTestHome(t)
	if err := saveProxyRouteStash("omp", map[string]proxyRouteStashEntry{
		"x": {Provider: "x"},
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(proxyRouteStashPath("omp"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("stash mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestOmpQuotedBaseUrlRoutesClean(t *testing.T) {
	ompProxyTestHome(t)
	original := `providers:
  qwen:
    baseUrl: "https://a.example/v1"
    apiKey: k
    api: openai-completions
`
	file := ompModelsFile()
	if err := util.WriteFile(file, original); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("expected wire")
	}
	if !OmpProxyWired() {
		t.Fatal("quoted baseUrl must wire")
	}
	stash := loadProxyRouteStash("omp")
	if stash["qwen"].Upstream != "https://a.example" {
		t.Fatalf("upstream polluted by quotes: %q", stash["qwen"].Upstream)
	}
	raw, _ := util.ReadFileSafe(file)
	if !strings.Contains(raw, `x-headroom-base-url: https://a.example`) {
		t.Fatalf("header contains quote characters:\n%s", raw)
	}
	if !RemoveOmpProxy() {
		t.Fatal("expected restore")
	}
	if raw, _ = util.ReadFileSafe(file); raw != original {
		t.Fatalf("restore not byte-identical:\n got: %q\nwant: %q", raw, original)
	}
}

func TestOmpDuplicateProviderKeysRefused(t *testing.T) {
	ompProxyTestHome(t)
	dup := `providers:
  qwen:
    baseUrl: https://first.example/v1
    apiKey: a
    api: openai-completions
  qwen:
    baseUrl: https://second.example/v1
    apiKey: b
    api: openai-completions
`
	file := ompModelsFile()
	if err := util.WriteFile(file, dup); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); changed {
		t.Fatal("duplicate provider keys must refuse wiring")
	}
	if raw, _ := util.ReadFileSafe(file); raw != dup {
		t.Fatalf("ambiguous file must stay untouched:\n%s", raw)
	}
}

func TestWiredFalseWhenProviderAddedAfterUp(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  alpha:
    baseUrl: https://alpha.example/v1
    apiKey: a
    api: openai-completions
`
	file := ompModelsFile()
	if err := util.WriteFile(file, models); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("expected wire")
	}
	if !OmpProxyWired() {
		t.Fatal("should be wired")
	}
	grown := strings.Replace(models, "providers:\n", "providers:\n  beta:\n    baseUrl: https://beta.example/v1\n    apiKey: b\n    api: openai-completions\n", 1)
	if err := util.WriteFile(file, grown); err != nil {
		t.Fatal(err)
	}
	if OmpProxyWired() {
		t.Fatal("new unrouted provider must break managed claim")
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("re-up should wire the new provider")
	}
	if !OmpProxyWired() {
		t.Fatal("all providers routed now, should be wired")
	}
}

func TestStashPrunedWhenApiBecomesUnsupported(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  solo:
    baseUrl: https://solo.example/v1
    apiKey: k
    api: openai-completions
`
	file := ompModelsFile()
	if err := util.WriteFile(file, models); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("expected wire")
	}
	broken := strings.Replace(models, "api: openai-completions", "api: exotic-protocol", 1)
	if err := util.WriteFile(file, broken); err != nil {
		t.Fatal(err)
	}
	ConfigureOmpProxy()
	stash := loadProxyRouteStash("omp")
	if len(stash) != 0 {
		t.Fatalf("unsupported transport must release stash, got %v", stash)
	}
	if OmpProxyWired() {
		t.Fatal("must not claim managed after release")
	}
}

func TestOmpRewireLastProviderStable(t *testing.T) {
	ompProxyTestHome(t)
	original := `providers:
  solo:
    baseUrl: https://solo.example/v1
    apiKey: k
    api: openai-completions
`
	file := ompModelsFile()
	if err := util.WriteFile(file, original); err != nil {
		t.Fatal(err)
	}
	ConfigureOmpProxy()
	first, _ := util.ReadFileSafe(file)
	if strings.Contains(first, "\n\n") {
		t.Fatalf("fresh wire must not inject blank lines:\n%q", first)
	}
	if !RemoveOmpProxy() {
		t.Fatal("expected restore")
	}
	if raw, _ := util.ReadFileSafe(file); raw != original {
		t.Fatalf("restore not byte-identical:\n got: %q\nwant: %q", raw, original)
	}
	ConfigureOmpProxy()
	second, _ := util.ReadFileSafe(file)
	if second != first {
		t.Fatalf("re-wire not stable:\n first: %q\nsecond: %q", first, second)
	}
}

func TestPiDetectProxyNativeRoutesManaged(t *testing.T) {
	piProxyTestHome(t)
	providers := util.NewOrderedMap()
	must := piProxyProviderEntry("https://htmustc.id.vn/v1")
	must.Set("baseUrl", "http://127.0.0.1:8787/v1")
	headers := util.NewOrderedMap()
	headers.Set("x-headroom-base-url", "https://htmustc.id.vn")
	must.Set("headers", headers)
	providers.Set("mustc", must)
	cfg := util.NewOrderedMap()
	cfg.Set("providers", providers)
	if err := util.WriteFile(piModelsFile(), util.StringifyJSON(cfg)); err != nil {
		t.Fatal(err)
	}
	if err := saveProxyRouteStash("pi", map[string]proxyRouteStashEntry{
		"mustc": {Provider: "mustc", BaseURL: "https://htmustc.id.vn/v1", Upstream: "https://htmustc.id.vn", BaseKey: "baseUrl"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = saveProxyRouteStash("pi", nil) })
	if !PiProxyWired() {
		t.Fatal("native routes should be wired")
	}
	if d := DetectProxy("pi"); d.State != ProxyStateManaged {
		t.Fatalf("detect state = %q (%s), want managed", d.State, d.Detail)
	}
}

func TestOmpDetectProxyNativeRoutesManaged(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  qwen:
    baseUrl: http://127.0.0.1:8787/v1
    apiKey: user-key
    api: openai-completions
    headers:
      x-headroom-base-url: https://dashscope.aliyuncs.com/compatible-mode
`
	if err := util.WriteFile(ompModelsFile(), models); err != nil {
		t.Fatal(err)
	}
	if err := saveProxyRouteStash("omp", map[string]proxyRouteStashEntry{
		"qwen": {Provider: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Upstream: "https://dashscope.aliyuncs.com/compatible-mode", BaseKey: "baseUrl"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = saveProxyRouteStash("omp", nil) })
	if !OmpProxyWired() {
		t.Fatal("native routes should be wired")
	}
	if d := DetectProxy("omp"); d.State != ProxyStateManaged {
		t.Fatalf("detect state = %q (%s), want managed", d.State, d.Detail)
	}
}

func TestOmpProxyBlankLineInHeadersDoesNotDuplicateKey(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  qwen:
    baseUrl: https://dashscope.aliyuncs.com/compatible-mode/v1
    apiKey: user-key
    api: openai-completions
    headers:

      x-user: keep
`
	if err := util.WriteFile(ompModelsFile(), models); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); !changed {
		t.Fatal("native OMP provider not wired")
	}
	got, _ := util.ReadFileSafe(ompModelsFile())
	if n := strings.Count(got, "x-headroom-base-url"); n != 1 {
		t.Fatalf("expected exactly one x-headroom-base-url, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "x-user: keep") {
		t.Fatalf("user header lost:\n%s", got)
	}
	if !RemoveOmpProxy() || OmpProxyWired() {
		t.Fatal("OMP native route not removed")
	}
	got, _ = util.ReadFileSafe(ompModelsFile())
	if strings.Contains(got, "x-headroom-base-url") || !strings.Contains(got, "x-user: keep") {
		t.Fatalf("OMP provider not restored:\n%s", got)
	}
}

func TestOmpProxyAlreadyRoutedWithoutStashNotClaimed(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  qwen:
    baseUrl: http://127.0.0.1:8787/v1
    apiKey: user-key
    api: openai-completions
    headers:
      x-headroom-base-url: https://dashscope.aliyuncs.com/compatible-mode
`
	if err := util.WriteFile(ompModelsFile(), models); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOmpProxy(); changed {
		t.Fatal("pre-routed provider must not be rewritten")
	}
	if _, err := os.Stat(proxyRouteStashPath("omp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stash fabricated for pre-routed provider: %v", err)
	}
	if RemoveOmpProxy() {
		t.Fatal("remove must not touch routes without tokless stash")
	}
	got, _ := util.ReadFileSafe(ompModelsFile())
	if !strings.Contains(got, "baseUrl: http://127.0.0.1:8787/v1") {
		t.Fatalf("pre-routed provider modified:\n%s", got)
	}
}

func TestPiProxyAlreadyRoutedWithoutStashNotClaimed(t *testing.T) {
	piProxyTestHome(t)
	raw := `{"providers":{"qwen":{"baseUrl":"http://127.0.0.1:8787/v1","api":"openai-completions","apiKey":"user-key","headers":{"x-headroom-base-url":"https://dashscope.aliyuncs.com/compatible-mode"}}}}`
	if err := util.WriteFile(piModelsFile(), raw); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigurePiProxy(); changed {
		t.Fatal("pre-routed provider must not be rewritten")
	}
	if _, err := os.Stat(proxyRouteStashPath("pi")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stash fabricated for pre-routed provider: %v", err)
	}
	if RemovePiProxy() {
		t.Fatal("remove must not touch routes without tokless stash")
	}
	got, _ := util.ReadFileSafe(piModelsFile())
	if !strings.Contains(got, "http://127.0.0.1:8787/v1") {
		t.Fatalf("pre-routed provider modified:\n%s", got)
	}
}

func TestPiProxyNonStringHeaderSkipped(t *testing.T) {
	piProxyTestHome(t)
	raw := `{"providers":{` +
		`"nvidia":{"baseUrl":"https://integrate.api.nvidia.com/v1","api":"openai-completions","apiKey":"nv-key"},` +
		`"weird":{"baseUrl":"https://weird.example/v1","api":"openai-completions","apiKey":"k","headers":{"x-headroom-base-url":false}}}}`
	if err := util.WriteFile(piModelsFile(), raw); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigurePiProxy(); !changed {
		t.Fatal("routable provider not wired")
	}
	got, _ := util.ReadFileSafe(piModelsFile())
	if !strings.Contains(got, `"x-headroom-base-url": false`) {
		t.Fatalf("non-string header destroyed:\n%s", got)
	}
	if !strings.Contains(got, "https://weird.example/v1") {
		t.Fatalf("conflicting provider was rewritten:\n%s", got)
	}
	if !strings.Contains(got, "https://integrate.api.nvidia.com") {
		t.Fatalf("routable provider missing upstream header:\n%s", got)
	}
}

func TestOmpProxyDoesNotPersistStashOnConfigWriteFailure(t *testing.T) {
	ompProxyTestHome(t)
	models := `providers:
  qwen:
    baseUrl: https://dashscope.aliyuncs.com/compatible-mode/v1
    apiKey: user-key
    api: openai-completions
`
	if err := util.WriteFile(ompModelsFile(), models); err != nil {
		t.Fatal(err)
	}
	prev := ompWriteFile
	ompWriteFile = func(string, string) error { return errors.New("boom") }
	t.Cleanup(func() { ompWriteFile = prev })
	if changed, _ := ConfigureOmpProxy(); changed {
		t.Fatal("configure must fail when config write fails")
	}
	if stash := loadProxyRouteStash("omp"); len(stash) != 0 {
		t.Fatalf("stash persisted after config write failure: %+v", stash)
	}
	if _, err := os.Stat(proxyRouteStashPath("omp")); !os.IsNotExist(err) {
		t.Fatalf("stash file exists after config write failure: %v", err)
	}
}

func TestClaudeProxyTakesOverForeignBYOK(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{
  "env": {
    "ANTHROPIC_API_KEY": "sk-byok",
    "ANTHROPIC_BASE_URL": "https://api.qwencoder.test/api",
    "ANTHROPIC_CUSTOM_HEADERS": "X-Keep: yes"
  }
}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover to write")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"ANTHROPIC_BASE_URL": "http://127.0.0.1:8787"`) ||
		!strings.Contains(raw, "x-headroom-base-url: https://api.qwencoder.test/api") ||
		!strings.Contains(raw, "X-Keep: yes") {
		t.Fatalf("takeover state wrong:\n%s", raw)
	}
	if !strings.Contains(raw, `"ANTHROPIC_AUTH_TOKEN": "sk-byok"`) || strings.Contains(raw, `"ANTHROPIC_API_KEY"`) {
		t.Fatalf("api key must move to Bearer token while wired:\n%s", raw)
	}
	if !ClaudeProxyWired() {
		t.Fatal("expected wired after takeover")
	}
	stash, ok := loadClaudeBYOKStash()
	if !ok || stash.BaseURL != "https://api.qwencoder.test/api" || !stash.HadHeader || stash.Header != "X-Keep: yes" {
		t.Fatalf("stash wrong: %+v ok=%v", stash, ok)
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected restore")
	}
	raw, _ = util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"ANTHROPIC_BASE_URL": "https://api.qwencoder.test/api"`) ||
		!strings.Contains(raw, `"X-Keep: yes"`) || strings.Contains(raw, "x-headroom-base-url") ||
		!strings.Contains(raw, `"ANTHROPIC_API_KEY": "sk-byok"`) || strings.Contains(raw, "ANTHROPIC_AUTH_TOKEN") {
		t.Fatalf("restore state wrong:\n%s", raw)
	}
	if ClaudeProxyWired() {
		t.Fatal("expected unwired after restore")
	}
	if _, ok := loadClaudeBYOKStash(); ok {
		t.Fatal("stash should be cleared")
	}
}

func TestClaudeProxyRestoresBothCredentialKeys(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_API_KEY":"api-key","ANTHROPIC_AUTH_TOKEN":"auth-token","ANTHROPIC_BASE_URL":"https://gw.test/v1"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover write")
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected restore")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"ANTHROPIC_API_KEY":"api-key"`) || !strings.Contains(raw, `"ANTHROPIC_AUTH_TOKEN":"auth-token"`) {
		t.Fatalf("both credentials not restored:\n%s", raw)
	}
}

func TestClaudeTakeoverAddsHeadersWhenAbsent(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	if err := util.WriteFile(settings, `{"env":{"ANTHROPIC_BASE_URL":"https://gw.test/v1"}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover write")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `ANTHROPIC_CUSTOM_HEADERS`) || strings.Contains(raw, "x-headroom-base-url: https://gw.test/v1") || !strings.Contains(raw, "x-headroom-base-url: https://gw.test") {
		t.Fatalf("hop header missing or /v1 not stripped:\n%s", raw)
	}
	if strings.Contains(raw, "qwen3.8-max") {
		t.Fatalf("takeover must not pin managed model:\n%s", raw)
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected restore")
	}
	raw, _ = util.ReadFileSafe(settings)
	if strings.Contains(raw, "ANTHROPIC_CUSTOM_HEADERS") || !strings.Contains(raw, "https://gw.test/v1") {
		t.Fatalf("restore wrong:\n%s", raw)
	}
}

func TestClaudeTakeoverPreservesUserModelPin(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://gw.test/v1","ANTHROPIC_MODEL":"user-model-x"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover write")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"ANTHROPIC_MODEL": "user-model-x"`) {
		t.Fatalf("takeover clobbered user model pin:\n%s", raw)
	}
	if strings.Contains(raw, "qwen3.8-max") {
		t.Fatalf("takeover must not inject managed model:\n%s", raw)
	}
}

func TestClaudeTakeoverPreservesUserBYOKRouteAndModel(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://api.provider.test/api","ANTHROPIC_MODEL":"glm-5.3-flash","ANTHROPIC_CUSTOM_HEADERS":"X-User: keep"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover write")
	}
	raw, _ := util.ReadFileSafe(settings)
	for _, want := range []string{
		`"ANTHROPIC_MODEL": "glm-5.3-flash"`,
		`"ANTHROPIC_BASE_URL": "http://127.0.0.1:8787"`,
		`x-headroom-base-url: https://api.provider.test/api`,
		`X-User: keep`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("BYOK setting lost %q:\n%s", want, raw)
		}
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected restore")
	}
	raw, _ = util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"ANTHROPIC_BASE_URL":"https://api.provider.test/api"`) ||
		!strings.Contains(raw, `"ANTHROPIC_MODEL":"glm-5.3-flash"`) ||
		strings.Contains(raw, "x-headroom-base-url") {
		t.Fatalf("BYOK route/model not restored:\n%s", raw)
	}
}

func TestClaudeRestoreKeepsUserHeaderEdits(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	if err := util.WriteFile(settings, `{"env":{"ANTHROPIC_BASE_URL":"https://gw.test/v1","ANTHROPIC_CUSTOM_HEADERS":"X-A: 1"}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover write")
	}
	if err := util.WriteFile(settings, `{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8787","ANTHROPIC_CUSTOM_HEADERS":"x-headroom-base-url: https://gw.test/v1\nX-B: 2"}}`); err != nil {
		t.Fatal(err)
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected restore")
	}
	raw, _ := util.ReadFileSafe(settings)
	if strings.Contains(raw, "X-A") || !strings.Contains(raw, "X-B: 2") || !strings.Contains(raw, `"https://gw.test/v1"`) {
		t.Fatalf("restore did not respect current user state:\n%s", raw)
	}
}

func TestClaudeRestoreKeepsUserCredentialEdits(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	if err := util.WriteFile(settings, `{"env":{"ANTHROPIC_BASE_URL":"https://gw.test/v1","ANTHROPIC_API_KEY":"original-key"}}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover")
	}
	if err := util.WriteFile(settings, `{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8787","ANTHROPIC_AUTH_TOKEN":"user-edited-token"}}`); err != nil {
		t.Fatal(err)
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected restore")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"ANTHROPIC_AUTH_TOKEN": "user-edited-token"`) || strings.Contains(raw, `"ANTHROPIC_AUTH_TOKEN": "original-key"`) {
		t.Fatalf("restore overwrote user credential edit:\n%s", raw)
	}
}

func TestClaudeTakeoverJournalsCompleteStateBeforeSettingsWrite(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	if err := util.WriteFile(settings, `{"env":{"ANTHROPIC_BASE_URL":"https://gw.test/v1","ANTHROPIC_API_KEY":"original-key"}}`); err != nil {
		t.Fatal(err)
	}
	checked := false
	util.SetWriteFileOverride(func(path, _ string) error {
		if path == settings {
			entry, ok := loadClaudeBYOKStash()
			checked = ok && len(entry.Managed) > 0 && entry.BaseKey == "original-key"
			return os.ErrPermission
		}
		return nil
	})
	defer util.SetWriteFileOverride(nil)
	if changed, _ := ConfigureClaudeProxy(); changed {
		t.Fatal("configure reported success when settings write failed")
	}
	if !checked {
		t.Fatal("settings write attempted before complete recovery journal existed")
	}
	if _, ok := loadClaudeBYOKStash(); ok {
		t.Fatal("failed takeover left recovery journal")
	}
}

func TestClaudeCustomHeadersDropVariants(t *testing.T) {
	lines := []string{"X-Keep: yes", "x-headroom-base-url:nospace", "X-Headroom-Base-Url:  spaced\t", "x-headroom-base-url", "Another: v"}
	kept, dropped := claudeCustomHeadersDrop(lines)
	if !dropped || len(kept) != 2 || kept[0] != "X-Keep: yes" || kept[1] != "Another: v" {
		t.Fatalf("drop variants wrong: dropped=%v kept=%v", dropped, kept)
	}
}

func TestClaudeTakeoverStripsV1SuffixFromHop(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://api.qwencoder.test/api/v1","ANTHROPIC_API_KEY":"sk-x"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover write")
	}
	raw, _ := util.ReadFileSafe(settings)
	if strings.Contains(raw, "/v1/v1") || !strings.Contains(raw, "x-headroom-base-url: https://api.qwencoder.test/api") {
		t.Fatalf("hop header must drop /v1 suffix:\n%s", raw)
	}
	stash, ok := loadClaudeBYOKStash()
	if !ok || stash.BaseURL != "https://api.qwencoder.test/api/v1" {
		t.Fatalf("stash must keep the verbatim user URL: %+v ok=%v", stash, ok)
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected restore")
	}
	raw, _ = util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"https://api.qwencoder.test/api/v1"`) {
		t.Fatalf("restore lost verbatim URL:\n%s", raw)
	}
}

func TestClaudeTakeoverLeavesBearerTokenAlone(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://gw.test/v1","ANTHROPIC_AUTH_TOKEN":"user-bearer"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("expected takeover write")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"user-bearer"`) {
		t.Fatalf("user bearer must survive takeover:\n%s", raw)
	}
	if !RemoveClaudeProxy() {
		t.Fatal("expected restore")
	}
	raw, _ = util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"user-bearer"`) {
		t.Fatalf("restore lost user bearer:\n%s", raw)
	}
}

// TestMalformedPIDLock ensures that a lock file with non-numeric content,
// negative PID, or zero PID is stolen after the 10-second stale threshold.
func TestMalformedPIDLock(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	lockPath := filepath.Join(util.HeadroomPathsResolved().Root, "byok.stash.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-15 * time.Second)
	if err := os.WriteFile(lockPath, []byte("123junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(lockPath, past, past); err != nil {
		t.Fatal(err)
	}
	ok := false
	err := withProxyRouteStashLock(func() error {
		ok = true
		return nil
	})
	if !ok || err != nil {
		t.Fatalf("expected lock steal + fn success; got ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected old lock to be removed; remaining: %v", err)
	}
}
