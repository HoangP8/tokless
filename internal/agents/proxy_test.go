package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestClaudeProxyDoesNotClobberUserValue(t *testing.T) {
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
	if changed, _ := ConfigureClaudeProxy(); changed {
		t.Fatal("configure clobbered a user-set ANTHROPIC_BASE_URL")
	}
	if ClaudeProxyWired() {
		t.Fatal("differing user value must not report wired")
	}
	if RemoveClaudeProxy() {
		t.Fatal("remove must not delete a differing user value")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, "http://user.example:9999") || !strings.Contains(raw, "sk-test") {
		t.Fatalf("user env was clobbered:\n%s", raw)
	}
}

func TestClaudeProxyConfigureEnsuresModelPinOnRewire(t *testing.T) {
	claudeProxyTestHome(t)
	settings := util.ClaudeCodePaths().Settings
	seed := `{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8787"}}`
	if err := util.WriteFile(settings, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureClaudeProxy(); !changed {
		t.Fatal("re-wire with base URL set but no model pin must write pin")
	}
	raw, _ := util.ReadFileSafe(settings)
	if !strings.Contains(raw, `"ANTHROPIC_MODEL": "qwen3.8-max"`) {
		t.Fatalf("model pin not written on re-wire:\n%s", raw)
	}
	if changed, _ := ConfigureClaudeProxy(); changed {
		t.Fatal("second configure after pin is set should be a no-op")
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
	if got := DetectProxy("codex"); got.State != ProxyStateUnknown {
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
supports_websockets = true
`, state: ProxyStateUnknown},
		{name: "exact block", block: `
[model_providers.headroom]
supports_websockets = true # text [model_providers.headroom] name = "wrong"
name = "Headroom persistent proxy"
base_url = "` + endpoint + `#not-a-comment"
`, state: ProxyStateUnknown},
		{name: "exact desired block", block: `
[model_providers.headroom]
name = "Headroom persistent proxy" # foreign text [model_providers.headroom]
base_url = "` + endpoint + `"
supports_websockets = true
`, state: ProxyStateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `model_provider = "headroom"
openai_base_url = "` + endpoint + `"
` + tc.block
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
	if got.State != ProxyStateUnknown {
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
	if got := DetectProxy("codex"); got.State != ProxyStateUnknown {
		t.Fatalf("codex state = %s, want %s", got.State, ProxyStateUnknown)
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
	setTestHome(t)
	if ConfigureProxyAgent("codex") || RemoveProxyAgent("codex") || ProxyAgentWired("codex") {
		t.Fatal("codex proxy must be manual and unwired")
	}
	if got := DetectProxy("codex"); got.State != ProxyStateUnknown || got.Capability.WireKind != ProxyWireManual {
		t.Fatalf("codex detection = %+v", got)
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
}

func TestOpenCodeProxyScenarios(t *testing.T) {
	byokSeed := `{"$schema": "https://opencode.ai/config.json", "theme": "dark", "provider": {"prov-a": {"npm": "@ai-sdk/openai-compatible", "options": {"baseURL": "https://api.provider-a.test/v1", "apiKey": "prov-a-key"}, "models": {"m1": {"name": "M1"}}}}}`
	cases := []proxyScenario{
		{name: "wire rewrites BYOK baseURL, no headroom inject", seed: byokSeed,
			wantChange: true, wantWired: true, wantContains: []string{
				`"prov-a"`,
				`"baseURL": "http://127.0.0.1:8787/v1"`,
				`"theme": "dark"`,
			}, wantAbsent: []string{`"headroom"`, `"provider-a.test"`}},
		{name: "second inject idempotent", seed: byokSeed, preConfigure: true,
			wantChange: false, wantWired: true},
		{name: "no BYOK no-op", seed: openCodeProxySeed(false),
			wantChange: false, wantWired: false},
		{name: "adds missing providers, refuses differing entry", seed: openCodeProxySeed(true),
			wantChange: false, wantWired: false, keepContains: []string{"User Proxy", "user.example"}},
		{name: "refuses non-map provider", seed: `{"provider": "junk"}
`,
			wantChange: false, wantWired: false, keepContains: []string{`"junk"`}},
		{name: "remove restores BYOK baseURL", seed: byokSeed, preConfigure: true,
			remove: true, wantRemoved: true, wantWired: false, wantContains: []string{"provider-a.test"}, wantAbsent: []string{"127.0.0.1:8787"}},
		{name: "remove leaves differing value", seed: openCodeProxySeed(true),
			remove: true, wantRemoved: false, wantWired: false, keepContains: []string{"User Proxy", "user.example"}},
		{name: "absent file not created", seed: "", absent: true,
			wantChange: false, wantWired: false},
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
