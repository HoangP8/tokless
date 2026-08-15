package agents

import (
	"os"

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
	ID string
	Key string
	Npm string
	Name string
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

func apiboxProviderSpec() ProviderSpec {
	return ProviderSpec{
		ID:     "apibox",
		Key:    "apibox",
		Npm:    "@ai-sdk/anthropic",
		Name:   "APIBox",
		KeyEnv: "TOKLESS_APIOBOX_KEY",
		Models: []ProviderModel{
			{ID: "deepseek-v4-flash", Display: "APIBox DeepSeek V4 Flash", Context: 200000, Output: 64000, Reasoning: true},
			{ID: "qwen3.8-max", Display: "APIBox Qwen3 8 Max", Context: 200000, Output: 64000, Reasoning: true},
		},
	}
}

// ProviderSpecActive returns the selected provider, defaulting to the
// byte-compatible headroom spec.
func ProviderSpecActive() ProviderSpec {
	switch os.Getenv(proxyProviderEnv) {
	case providerApibox:
		return apiboxProviderSpec()
	}
	if st, ok := util.ReadProxyRuntime(); ok && st.Provider == providerApibox {
		return apiboxProviderSpec()
	}
	return DefaultProviderSpec()
}

const providerApibox = "apibox"