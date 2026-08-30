package util

import (
	"os"
	"strconv"
	"strings"
)

const GrokOAuthProxyDefaultPort = 8788

func GrokOAuthProxyPort() int {
	if v := strings.TrimSpace(os.Getenv("TOKLESS_GROK_PROXY_PORT")); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	return GrokOAuthProxyDefaultPort
}

func GrokOAuthProxyBaseURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(GrokOAuthProxyPort()) + "/v1"
}
