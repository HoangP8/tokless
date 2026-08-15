package agents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const (
	antigravityProxyEnvKey    = "GOOGLE_GEMINI_BASE_URL"
	antigravityProxyFenceHead = "# tokless:headroom begin"
	antigravityProxyFenceFoot = "# tokless:headroom end"
)

func antigravityEnvFile() string {
	return filepath.Join(util.Home(), ".gemini", ".env")
}

// antigravityEnvValue returns the current value of key, preferring the tokless
// fenced block over any un-fenced occurrence.
func antigravityEnvValue(raw, key string) string {
	lines := strings.Split(raw, "\n")
	inFence := false
	fenced, unfenced := "", ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == antigravityProxyFenceHead {
			inFence = true
			continue
		}
		if trimmed == antigravityProxyFenceFoot {
			inFence = false
			continue
		}
		name, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(name) != key {
			continue
		}
		v := strings.TrimSpace(value)
		if inFence {
			fenced = v
		} else if unfenced == "" {
			unfenced = v
		}
	}
	if fenced != "" {
		return fenced
	}
	return unfenced
}

// ConfigureAntigravityProxy writes GOOGLE_GEMINI_BASE_URL inside a tokless
// fenced block of ~/.gemini/.env.
func ConfigureAntigravityProxy() (changed bool, file string) {
	file = antigravityEnvFile()
	url := ProxyEndpointFor("antigravity")
	raw, _ := util.ReadFileSafe(file)
	if antigravityEnvValue(raw, antigravityProxyEnvKey) == url {
		return false, file
	}
	next := antigravityStripFence(raw)
	var sb strings.Builder
	sb.WriteString(strings.TrimSuffix(next, "\n"))
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(antigravityProxyFenceHead + "\n")
	sb.WriteString(antigravityProxyEnvKey + "=" + url + "\n")
	sb.WriteString(antigravityProxyFenceFoot + "\n")
	if err := util.WriteFile(file, sb.String()); err != nil {
		return false, file
	}
	return true, file
}

// RemoveAntigravityProxy deletes the tokless fenced block only when its key
// still matches what tokless set.
func RemoveAntigravityProxy() bool {
	file := antigravityEnvFile()
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false
	}
	url := ProxyEndpointFor("antigravity")
	if antigravityEnvValue(raw, antigravityProxyEnvKey) != url {
		return false
	}
	next := antigravityStripFence(raw)
	if next == raw {
		return false
	}
	next = strings.TrimSuffix(next, "\n")
	if strings.TrimSpace(next) == "" {
		return removeFileIfExists(file)
	}
	return util.WriteFile(file, next) == nil
}

// AntigravityProxyWired reports whether the fenced block sets the endpoint.
func AntigravityProxyWired() bool {
	raw, ok := util.ReadFileSafe(antigravityEnvFile())
	if !ok {
		return false
	}
	return antigravityEnvValue(raw, antigravityProxyEnvKey) == ProxyEndpointFor("antigravity")
}

func antigravityStripFence(raw string) string {
	lines := strings.Split(raw, "\n")
	var out []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == antigravityProxyFenceHead {
			inFence = true
			continue
		}
		if trimmed == antigravityProxyFenceFoot {
			inFence = false
			continue
		}
		if inFence {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// removeFileIfExists removes file if it exists; returns true if absent after.
func removeFileIfExists(path string) bool {
	if util.Exists(path) {
		if err := os.Remove(path); err != nil {
			return false
		}
	}
	return true
}