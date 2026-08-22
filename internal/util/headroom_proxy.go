package util

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const headroomProxyDefaultPort = 8787

// ProxyRuntime persists the effective headroom proxy runtime (port + upstream
// URLs) so `tokless proxy up` reproduces the same daemon the checks and agents
// were last wired to, even in a shell with no tokless env vars set.
type ProxyRuntime struct {
	Port         int    `json:"port"`
	AnthropicURL string `json:"anthropic_api_url"`
	OpenAIURL    string `json:"openai_api_url"`
	GeminiURL    string `json:"gemini_api_url"`
	CloudCodeURL string `json:"cloudcode_api_url"`
	Provider     string `json:"provider"`
}

func headroomProxyRuntimeFile() string {
	return filepath.Join(HeadroomPathsResolved().Root, "proxy.runtime.json")
}

// ReadProxyRuntime returns the persisted proxy runtime, if any.
func ReadProxyRuntime() (ProxyRuntime, bool) {
	raw, ok := ReadFileSafe(headroomProxyRuntimeFile())
	if !ok {
		return ProxyRuntime{}, false
	}
	var st ProxyRuntime
	if err := json.Unmarshal([]byte(raw), &st); err != nil || st.Port <= 0 {
		return ProxyRuntime{}, false
	}
	return st, true
}

// SaveHeadroomProxyRuntime persists the effective proxy runtime for future
// `tokless proxy` invocations.
func SaveHeadroomProxyRuntime(st ProxyRuntime) error {
	if st.Port <= 0 {
		return nil
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return WriteFile(headroomProxyRuntimeFile(), string(b))
}

// ClearHeadroomProxyRuntime removes the persisted runtime (daemon stopped).
func ClearHeadroomProxyRuntime() error {
	if err := os.Remove(headroomProxyRuntimeFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// HeadroomProxyPort returns the headroom daemon port: env, then persisted
// runtime, then the default 8787.
func HeadroomProxyPort() int {
	if raw := strings.TrimSpace(os.Getenv("TOKLESS_HEADROOM_PROXY_PORT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	if st, ok := ReadProxyRuntime(); ok {
		return st.Port
	}
	return headroomProxyDefaultPort
}

// HeadroomProxyURL is the local daemon base URL.
func HeadroomProxyURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(HeadroomProxyPort())
}

// HeadroomProxyOpenAIURL is the daemon's OpenAI-compatible base URL.
func HeadroomProxyOpenAIURL() string {
	return HeadroomProxyURL() + "/v1"
}
