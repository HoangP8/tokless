package agents

import "github.com/HoangP8/tokless/internal/util"

// ProxyEndpointFor returns the headroom daemon endpoint tokless wires an agent to, per protocol.
func ProxyEndpointFor(id string) string {
	switch id {
	case "codex", "opencode", "opencode2", "kilo", "pi", "droid", "copilot", "cline", "omp", "kimi", "zcode", "dsh":
		return util.HeadroomProxyOpenAIURL()
	case "claude", "antigravity":
		return util.HeadroomProxyURL()
	case "grok":
		return ""
	}
	return ""
}
