package agents

import "github.com/HoangP8/tokless/internal/util"

// ProxyEndpointFor returns the headroom daemon endpoint tokless targets when it
// wires an agent's config, applying per-type rules:
//
//   - OpenAI-compatible providers (codex, opencode, kilo, pi, droid, copilot,
//     cline, omp, kimi, zcode, dsh) use the shared daemon's /v1 URL — their client
//     appends /chat/completions, /responses, /models.
//   - Anthropic-native (claude) and Gemini-native (antigravity) use the bare
//     shared URL — their SDK appends /v1/messages and /v1beta/models itself.
//   - Grok owns a dedicated OAuth proxy (not shared) — returns "".
func ProxyEndpointFor(id string) string {
	switch id {
	case "codex", "opencode", "kilo", "pi", "droid", "copilot", "cline", "omp", "kimi", "zcode", "dsh":
		return util.HeadroomProxyOpenAIURL()
	case "claude", "antigravity":
		return util.HeadroomProxyURL()
	case "grok":
		return ""
	}
	return ""
}
