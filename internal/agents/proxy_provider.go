package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const proxyProviderEnv = "TOKLESS_PROXY_PROVIDER"

// ProviderModel is the model metadata a provider-block spec carries.
type ProviderModel struct {
	ID        string
	Display   string
	Context   int
	Output    int
	Reasoning bool
	ToolCall  bool
}

// ProviderSpec is one selectable proxy backend identity.
type ProviderSpec struct {
	ID     string
	Key    string
	Npm    string
	Name   string
	KeyEnv string
	Models []ProviderModel
}

func DefaultProviderSpec() ProviderSpec {
	return ProviderSpec{
		ID:   "headroom",
		Key:  "headroom",
		Npm:  "@ai-sdk/openai-compatible",
		Name: "Headroom Proxy",
		Models: []ProviderModel{
			{ID: "gpt-4o", Display: "GPT-4o", Context: 128000, Output: 16384},
			{ID: "gpt-4.1", Display: "GPT-4.1", Context: 1048576, Output: 32768},
		},
	}
}

// ProviderSpecActive returns the managed headroom proxy identity.
func ProviderSpecActive() ProviderSpec {
	return DefaultProviderSpec()
}

// proxyWireModel returns the model id tokless pins into OpenAI-compatible
// agent configs.
func proxyWireModel() string {
	if v := strings.TrimSpace(os.Getenv("TOKLESS_PROXY_MODEL")); v != "" {
		return v
	}
	return "headroom"
}

// proxyWireKey returns the API key tokless pins into agent configs that have
// no environment-key mechanism.
func proxyWireKey() string {
	if v := strings.TrimSpace(os.Getenv("TOKLESS_PROXY_KEY")); v != "" {
		return v
	}
	return "tokless"
}

type proxyRouteStashEntry struct {
	File      string `json:"file"`
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	Upstream  string `json:"upstream,omitempty"`
	BaseKey   string `json:"base_key,omitempty"`
	HadHeader bool   `json:"had_header,omitempty"`
	Header    string `json:"header,omitempty"`
	BaseLine  string `json:"base_line,omitempty"`
	HeaderLine string `json:"header_line,omitempty"`
}

type proxyRouteStashFile struct {
	Providers map[string]proxyRouteStashEntry `json:"providers"`
}

func proxyRouteStashPath(agent string) string {
	return filepath.Join(util.HeadroomPathsResolved().Root, agent+".byok.stash.json")
}

func loadProxyRouteStash(agent string) map[string]proxyRouteStashEntry {
	raw, ok := util.ReadFileSafe(proxyRouteStashPath(agent))
	if !ok {
		return map[string]proxyRouteStashEntry{}
	}
	var f proxyRouteStashFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil || f.Providers == nil {
		return map[string]proxyRouteStashEntry{}
	}
	return f.Providers
}

func saveProxyRouteStash(agent string, providers map[string]proxyRouteStashEntry) error {
	if len(providers) == 0 {
		_ = os.Remove(proxyRouteStashPath(agent))
		return nil
	}
	b, err := json.Marshal(proxyRouteStashFile{Providers: providers})
	if err != nil {
		return err
	}
	path := proxyRouteStashPath(agent)
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err = f.Write(b); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

func normalizedHeadroomUpstream(baseURL, api string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if api == "openai-completions" && strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL[:len(baseURL)-len("/v1")]
	}
	if api == "anthropic-messages" && strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL[:len(baseURL)-len("/v1")]
	}
	return baseURL
}

func proxyEndpointForAPI(api string) string {
	if api == "anthropic-messages" {
		return util.HeadroomProxyURL()
	}
	if api == "openai-completions" {
		return util.HeadroomProxyOpenAIURL()
	}
	return ""
}
