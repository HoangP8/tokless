package agents

import "github.com/HoangP8/tokless/internal/util"

// ProxyEndpointFor returns the headroom daemon endpoint tokless targets when it
// wires an agent's config, applying the provider rule:
//
//   - OpenAI-compatible providers (codex, opencode, kilo, pi, droid) use the
//     /v1 URL — their client appends /chat/completions, /responses, /models.
//   - Anthropic-native providers (claude, omp) use the bare URL — their SDK
//     appends /v1/messages itself.
//
// It returns "" for agents tokless does not wire through a config file.
func ProxyEndpointFor(id string) string {
	switch id {
	case "codex", "opencode", "kilo", "pi", "droid":
		return util.HeadroomProxyOpenAIURL()
	case "claude", "omp":
		return util.HeadroomProxyURL()
	}
	return ""
}
