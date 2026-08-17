package agents

// Concrete provider identities — EXAMPLES, not core machinery.

const providerApibox = "apibox"
const providerOpencodeGo = "opencode-go"

func apiboxProviderSpec() ProviderSpec {
	return ProviderSpec{
		ID:     providerApibox,
		Key:    providerApibox,
		Npm:    "@ai-sdk/anthropic",
		Name:   "APIBox",
		KeyEnv: "TOKLESS_APIOBOX_KEY",
		Models: []ProviderModel{
			{ID: "deepseek-v4-flash", Display: "APIBox DeepSeek V4 Flash", Context: 200000, Output: 64000, Reasoning: true},
			{ID: "qwen3.8-max", Display: "APIBox Qwen3 8 Max", Context: 200000, Output: 64000, Reasoning: true},
		},
	}
}

// opencodeGoProviderSpec is the opencode.ai/zen Go gateway: one key serves the
// anthropic shape (/v1/messages), the OpenAI shapes (/v1/chat/completions,
// /v1/responses) — the proxy's URL-path routing picks the upstream per shape.
func opencodeGoProviderSpec() ProviderSpec {
	return ProviderSpec{
		ID:     providerOpencodeGo,
		Key:    providerOpencodeGo,
		Npm:    "@ai-sdk/anthropic",
		Name:   "OpenCode Go",
		KeyEnv: "TOKLESS_OPENCODE_GO_KEY",
		Models: []ProviderModel{
			{ID: "qwen3.8-max", Display: "OpenCode Go Qwen3 8 Max", Context: 200000, Output: 64000, Reasoning: true},
			{ID: "qwen3.7-max", Display: "OpenCode Go Qwen3 7 Max", Context: 200000, Output: 64000, Reasoning: true},
			{ID: "qwen3.7-plus", Display: "OpenCode Go Qwen3 7 Plus", Context: 200000, Output: 64000, Reasoning: true},
			{ID: "minimax-m3", Display: "OpenCode Go MiniMax M3", Context: 200000, Output: 64000},
		},
	}
}

// exampleProviderSpecs is the registry of known example providers; adding a
// backend is one entry here plus its spec function above.
var exampleProviderSpecs = map[string]func() ProviderSpec{
	providerApibox:     apiboxProviderSpec,
	providerOpencodeGo: opencodeGoProviderSpec,
}

// exampleProviderSpec looks up a registered example provider; empty id → zero spec.
func exampleProviderSpec(id string) ProviderSpec {
	if f, ok := exampleProviderSpecs[id]; ok {
		return f()
	}
	return ProviderSpec{}
}
