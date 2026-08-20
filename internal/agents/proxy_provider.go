package agents

import (
	"os"
	"strings"
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
