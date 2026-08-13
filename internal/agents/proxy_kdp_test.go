package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// proxyEndpoint is the OpenAI-compatible endpoint derived from the daemon URL.
const proxyEndpoint = proxyTestURL + "/v1"

func kiloProxyTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("KILO_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func piProxyTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func droidProxyTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
}

type proxyConfigCase struct {
	name         string
	seed         string
	skipConfig   bool
	wantConfigCh bool
	wantWired    bool
	wantRemove   bool
	wantContains []string
	wantRetained []string
	wantAbsent   []string
	wantNoFile   bool
}

type proxyAgentOps struct {
	configPath func(t *testing.T) string
	home       func(t *testing.T)
	configure  func() (bool, string)
	wired      func() bool
	remove     func() bool
}

func runProxyConfigCases(t *testing.T, ops proxyAgentOps, cases []proxyConfigCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ops.home(t)
			cfg := ops.configPath(t)
			if tc.seed != "" {
				if err := util.WriteFile(cfg, tc.seed); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.skipConfig {
				changed, file := ops.configure()
				if changed != tc.wantConfigCh {
					t.Fatalf("Configure changed = %v, want %v", changed, tc.wantConfigCh)
				}
				if file != cfg {
					t.Fatalf("Configure file = %q, want %q", file, cfg)
				}
			}
			if got := ops.wired(); got != tc.wantWired {
				t.Fatalf("Wired = %v, want %v", got, tc.wantWired)
			}
			if raw, _ := util.ReadFileSafe(cfg); len(tc.wantContains) > 0 {
				for _, want := range tc.wantContains {
					if !strings.Contains(raw, want) {
						t.Fatalf("missing %q in config:%s", want, raw)
					}
				}
			}
			if got := ops.remove(); got != tc.wantRemove {
				t.Fatalf("Remove = %v, want %v", got, tc.wantRemove)
			}
			if ops.wired() {
				t.Fatal("still wired after Remove")
			}
			raw, ok := util.ReadFileSafe(cfg)
			if tc.wantNoFile {
				if ok {
					t.Fatalf("config file was created:%s", raw)
				}
				return
			}
			for _, want := range tc.wantRetained {
				if !strings.Contains(raw, want) {
					t.Fatalf("missing %q after Remove:%s", want, raw)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(raw, absent) {
					t.Fatalf("unexpected %q after Remove:%s", absent, raw)
				}
			}
		})
	}
}

func TestKiloProxyConfigurators(t *testing.T) {
	injected := `{
  "provider": {
    "tokless-headroom": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Headroom Proxy",
      "options": { "baseURL": "http://127.0.0.1:8787/v1" },
      "models": {
        "headroom": { "name": "Headroom", "tool_call": true, "limit": { "context": 128000, "output": 16384 } }
      }
    }
  }
}`
	reordered := `{
  "provider": {
    "tokless-headroom": {
      "models": {
        "headroom": { "limit": { "output": 16384, "context": 128000 }, "tool_call": true, "name": "Headroom" }
      },
      "options": { "baseURL": "http://127.0.0.1:8787/v1" },
      "name": "Headroom Proxy",
      "npm": "@ai-sdk/openai-compatible"
    }
  }
}`
	cases := []proxyConfigCase{
		{
			name:         "inject writes provider entry",
			wantConfigCh: true,
			wantWired:    true,
			wantRemove:   true,
			wantContains: []string{
				`"tokless-headroom"`,
				`"npm": "@ai-sdk/openai-compatible"`,
				`"baseURL": "http://127.0.0.1:8787/v1"`,
				`"tool_call": true`,
				`"context": 128000`,
			},
			wantAbsent: []string{proxyEndpoint},
		},
		{
			name:         "idempotent when already wired",
			seed:         injected,
			wantConfigCh: false,
			wantWired:    true,
			wantRemove:   true,
		},
		{
			name:         "idempotent even with reordered keys",
			seed:         reordered,
			wantConfigCh: false,
			wantWired:    true,
			wantRemove:   true,
		},
		{
			name:         "refuses differing existing entry",
			seed:         `{"provider":{"tokless-headroom":{"npm":"@ai-sdk/openai-compatible","name":"User","options":{"baseURL":"http://user.example:9999/v1"},"models":{}}}}`,
			wantConfigCh: false,
			wantWired:    false,
			wantRemove:   false,
			wantRetained: []string{`"http://user.example:9999/v1"`},
		},
		{
			name:         "refuses non-object provider field",
			seed:         `{"provider":"user"}`,
			wantConfigCh: false,
			wantWired:    false,
			wantRemove:   false,
			wantRetained: []string{`"provider":"user"`},
		},
		{
			name:         "merges under existing provider object",
			seed:         `{"provider":{"user-provider":{"npm":"@ai-sdk/openai","name":"User","options":{"baseURL":"http://u.example:1/v1"},"models":{}}}}`,
			wantConfigCh: true,
			wantWired:    true,
			wantRemove:   true,
			wantContains: []string{`"user-provider"`, `"http://u.example:1/v1"`, `"tokless-headroom"`},
			wantRetained: []string{`"user-provider"`, `"http://u.example:1/v1"`},
			wantAbsent:   []string{proxyEndpoint},
		},
		{
			name:       "wired false when file missing",
			skipConfig: true,
			wantWired:  false,
			wantRemove: false,
			wantNoFile: true,
		},
	}
	runProxyConfigCases(t, proxyAgentOps{
		home:       kiloProxyTestHome,
		configPath: func(t *testing.T) string { return util.KiloPathsResolved().Config },
		configure:  ConfigureKiloProxy,
		wired:      KiloProxyWired,
		remove:     RemoveKiloProxy,
	}, cases)
}

func TestPiProxyConfigurators(t *testing.T) {
	injected := `{
  "providers": {
    "tokless-headroom": {
      "baseUrl": "http://127.0.0.1:8787/v1",
      "api": "openai-completions",
      "apiKey": "tokless",
      "models": [ { "id": "headroom", "reasoning": false } ]
    }
  }
}`
	cases := []proxyConfigCase{
		{
			name:         "inject writes provider entry",
			wantConfigCh: true,
			wantWired:    true,
			wantRemove:   true,
			wantContains: []string{
				`"tokless-headroom"`,
				`"baseUrl": "http://127.0.0.1:8787/v1"`,
				`"api": "openai-completions"`,
				`"apiKey": "tokless"`,
				`"reasoning": false`,
			},
			wantAbsent: []string{proxyEndpoint},
		},
		{
			name:         "idempotent when already wired",
			seed:         injected,
			wantConfigCh: false,
			wantWired:    true,
			wantRemove:   true,
		},
		{
			name:         "refuses differing existing entry",
			seed:         `{"providers":{"tokless-headroom":{"baseUrl":"http://user.example:9999/v1","api":"openai","apiKey":"news","models":[]}}}`,
			wantConfigCh: false,
			wantWired:    false,
			wantRemove:   false,
			wantRetained: []string{`"http://user.example:9999/v1"`},
		},
		{
			name:         "refuses non-object providers field",
			seed:         `{"providers":"user"}`,
			wantConfigCh: false,
			wantWired:    false,
			wantRemove:   false,
			wantRetained: []string{`"providers":"user"`},
		},
		{
			name:         "merges under existing providers object",
			seed:         `{"providers":{"user-provider":{"baseUrl":"http://u.example:1/v1","api":"openai","apiKey":"k","models":[]}}}`,
			wantConfigCh: true,
			wantWired:    true,
			wantRemove:   true,
			wantContains: []string{`"user-provider"`, `"http://u.example:1/v1"`, `"tokless-headroom"`},
			wantRetained: []string{`"user-provider"`, `"http://u.example:1/v1"`},
			wantAbsent:   []string{proxyEndpoint},
		},
		{
			name:       "wired false when file missing",
			skipConfig: true,
			wantWired:  false,
			wantRemove: false,
			wantNoFile: true,
		},
	}
	runProxyConfigCases(t, proxyAgentOps{
		home:       piProxyTestHome,
		configPath: func(t *testing.T) string { return filepath.Join(piAgentDir(), "models.json") },
		configure:  ConfigurePiProxy,
		wired:      PiProxyWired,
		remove:     RemovePiProxy,
	}, cases)
}

func TestDroidProxyConfigurators(t *testing.T) {
	injected := `{
  "customModels": [
    {
      "model": "headroom",
      "displayName": "Headroom Proxy",
      "baseUrl": "http://127.0.0.1:8787/v1",
      "provider": "generic-chat-completion-api"
    }
  ]
}`
	cases := []proxyConfigCase{
		{
			name:         "inject appends customModel entry",
			wantConfigCh: true,
			wantWired:    true,
			wantRemove:   true,
			wantContains: []string{
				`"model": "headroom"`,
				`"displayName": "Headroom Proxy"`,
				`"baseUrl": "http://127.0.0.1:8787/v1"`,
				`"provider": "generic-chat-completion-api"`,
			},
			wantAbsent: []string{proxyEndpoint},
		},
		{
			name:         "idempotent when already wired",
			seed:         injected,
			wantConfigCh: false,
			wantWired:    true,
			wantRemove:   true,
		},
		{
			name:         "refuses differing existing entry",
			seed:         `{"customModels":[{"model":"headroom","displayName":"Headroom Proxy","baseUrl":"http://user.example:9999/v1","provider":"generic-chat-completion-api"}]}`,
			wantConfigCh: false,
			wantWired:    false,
			wantRemove:   false,
			wantRetained: []string{`"http://user.example:9999/v1"`},
		},
		{
			name:         "refuses non-array customModels field",
			seed:         `{"customModels":"user"}`,
			wantConfigCh: false,
			wantWired:    false,
			wantRemove:   false,
			wantRetained: []string{`"customModels":"user"`},
		},
		{
			name: "remove keeps other customModels entries",
			seed: `{"customModels":[
				{"model":"headroom","displayName":"Headroom Proxy","baseUrl":"http://127.0.0.1:8787/v1","provider":"generic-chat-completion-api"},
				{"model":"gpt","displayName":"User","baseUrl":"http://u.example:1/v1","provider":"generic-chat-completion-api"}
			]}`,
			wantConfigCh: false,
			wantWired:    true,
			wantRemove:   true,
			wantRetained: []string{`"User"`, `"http://u.example:1/v1"`},
			wantAbsent:   []string{proxyEndpoint},
		},
		{
			name:         "remove keeps user entry sharing proxy URL",
			seed:         `{"customModels":[{"model":"mine","displayName":"My Model","baseUrl":"http://127.0.0.1:8787/v1","provider":"generic-chat-completion-api"}]}`,
			wantConfigCh: true,
			wantWired:    true,
			wantRemove:   true,
			wantRetained: []string{`"mine"`, `"My Model"`, proxyEndpoint},
			wantAbsent:   []string{`"Headroom Proxy"`},
		},
		{
			name:       "wired false when file missing",
			skipConfig: true,
			wantWired:  false,
			wantRemove: false,
			wantNoFile: true,
		},
	}
	runProxyConfigCases(t, proxyAgentOps{
		home:       droidProxyTestHome,
		configPath: func(t *testing.T) string { return droidSettingsFile() },
		configure:  ConfigureDroidProxy,
		wired:      DroidProxyWired,
		remove:     RemoveDroidProxy,
	}, cases)
}

func TestProxyConfiguratorsRefusePersistenceFailure(t *testing.T) {
	tests := []struct {
		name      string
		home      func(*testing.T)
		path      func() string
		configure func() (bool, string)
		remove    func() bool
	}{
		{"claude", claudeProxyTestHome, func() string { return util.ClaudeCodePaths().Settings }, ConfigureClaudeProxy, RemoveClaudeProxy},
		{"opencode", opencodeProxyTestHome, func() string { return util.OpenCodePathsResolved().Config }, ConfigureOpenCodeProxy, RemoveOpenCodeProxy},
		{"kilo", kiloProxyTestHome, func() string { return util.KiloPathsResolved().Config }, ConfigureKiloProxy, RemoveKiloProxy},
		{"pi", piProxyTestHome, func() string { return filepath.Join(piAgentDir(), "models.json") }, ConfigurePiProxy, RemovePiProxy},
		{"droid", droidProxyTestHome, droidSettingsFile, ConfigureDroidProxy, RemoveDroidProxy},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.home(t)
			if err := util.WriteFile(tc.path(), "{}"); err != nil {
				t.Fatal(err)
			}
			util.SetWriteFileOverride(func(string, string) error { return os.ErrPermission })
			t.Cleanup(func() { util.SetWriteFileOverride(nil) })
			if changed, _ := tc.configure(); changed {
				t.Fatal("configure reported success when persistence failed")
			}
			util.SetWriteFileOverride(nil)
			if changed, _ := tc.configure(); !changed {
				t.Fatal("setup configure did not change")
			}
			util.SetWriteFileOverride(func(string, string) error { return os.ErrPermission })
			if tc.remove() {
				t.Fatal("remove reported success when persistence failed")
			}
		})
	}
}
