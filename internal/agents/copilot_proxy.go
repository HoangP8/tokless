package agents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const (
	copilotFenceHead = "# tokless:headroom copilot begin"
	copilotFenceFoot = "# tokless:headroom copilot end"
)

func copilotEnvFile() string { return filepath.Join(util.Home(), ".zshenv") }

func copilotFence(raw string) (string, bool) {
	lines := strings.SplitAfter(raw, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == copilotFenceHead {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == copilotFenceFoot {
			return strings.Join(lines[start:i+1], ""), true
		}
	}
	return "", false
}

func copilotFenceText() string {
	return copilotFenceHead + "\nexport COPILOT_PROVIDER_BASE_URL=" + ProxyEndpointFor("copilot") + "\nexport COPILOT_PROVIDER_API_KEY=\"$TOKLESS_OPENCODE_GO_KEY\"\nexport COPILOT_PROVIDER_TYPE=openai\n" + copilotFenceFoot + "\n"
}

func copilotStripFence(raw string) string {
	lines := strings.SplitAfter(raw, "\n")
	var out []string
	in := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == copilotFenceHead {
			in = true
			continue
		}
		if t == copilotFenceFoot {
			in = false
			continue
		}
		if !in {
			out = append(out, line)
		}
	}
	return strings.Join(out, "")
}

func copilotUnfencedConflict(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "export COPILOT_PROVIDER_") {
			key, value, ok := strings.Cut(strings.TrimPrefix(trimmed, "export "), "=")
			if ok && copilotUnfencedValueMatches(key, strings.TrimSpace(value)) {
				continue
			}
			return true
		}
	}
	return false
}

func copilotUnfencedValueMatches(key, value string) bool {
	want := map[string]string{"COPILOT_PROVIDER_BASE_URL": ProxyEndpointFor("copilot"), "COPILOT_PROVIDER_API_KEY": `"$TOKLESS_OPENCODE_GO_KEY"`, "COPILOT_PROVIDER_TYPE": "openai"}
	return want[key] == value
}

func copilotStripExactUnfenced(raw string) string {
	var out []string
	for _, line := range strings.SplitAfter(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "export COPILOT_PROVIDER_") {
			key, value, ok := strings.Cut(strings.TrimPrefix(trimmed, "export "), "=")
			if ok && copilotUnfencedValueMatches(key, strings.TrimSpace(value)) {
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

func ConfigureCopilotProxy() (bool, string) {
	file := copilotEnvFile()
	raw, _ := util.ReadFileSafe(file)
	if block, found := copilotFence(raw); found {
		if block != copilotFenceText() {
			return false, file
		}
		return false, file
	}
	if copilotUnfencedConflict(raw) {
		return false, file
	}
	raw = copilotStripExactUnfenced(raw)
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	if err := util.WriteFile(file, raw+copilotFenceText()); err != nil {
		return false, file
	}
	return true, file
}

func RemoveCopilotProxy() bool {
	file := copilotEnvFile()
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false
	}
	block, found := copilotFence(raw)
	if !found || block != copilotFenceText() {
		return false
	}
	return util.WriteFile(file, copilotStripFence(raw)) == nil
}

func CopilotProxyWired() bool {
	raw, ok := util.ReadFileSafe(copilotEnvFile())
	if !ok {
		return false
	}
	block, found := copilotFence(raw)
	return found && block == copilotFenceText()
}

func detectCopilotProxy(cap ProxyCapability) ProxyDetection {
	raw, err := readProxyConfig(copilotEnvFile())
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "shell environment not configured", ProxyStateUnconfigured)
		}
		return proxyDetection(cap.ID, "shell environment unreadable", ProxyStateUnreadable)
	}
	if block, found := copilotFence(raw); found {
		if block == copilotFenceText() {
			return proxyDetection(cap.ID, "exact managed shell block", ProxyStateManaged)
		}
		return proxyDetection(cap.ID, "managed shell block differs", ProxyStateConflict)
	}
	if copilotUnfencedConflict(raw) {
		return proxyDetection(cap.ID, "unfenced provider environment; ownership unknown", ProxyStateForeignBYOK)
	}
	return proxyDetection(cap.ID, "shell environment not configured", ProxyStateUnconfigured)
}
