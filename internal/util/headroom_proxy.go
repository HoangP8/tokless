package util

import (
	"os"
	"strconv"
	"strings"
)

const headroomProxyDefaultPort = 8787

// HeadroomProxyPort returns the headroom daemon port: TOKLESS_HEADROOM_PROXY_PORT.
func HeadroomProxyPort() int {
	if raw := strings.TrimSpace(os.Getenv("TOKLESS_HEADROOM_PROXY_PORT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 65535 {
			return n
		}
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
