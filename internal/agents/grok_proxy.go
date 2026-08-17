package agents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const grokManagedProfile = "headroom"

func grokProxyStateFile() string { return filepath.Join(util.ToklessDataDir(), "grok-profile-prev") }

func grokProfileText() string {
	return "[model.headroom]\nmodel = \"deepseek-v4-flash\"\nbase_url = \"" + ProxyEndpointFor("grok") + "\"\napi_backend = \"chat_completions\"\nstream_tool_calls = false\napi_key = \"" + os.Getenv("TOKLESS_OPENCODE_GO_KEY") + "\"\n"
}

func grokProfileBlock(raw string) (start, end int, block string, found bool) {
	lines := strings.SplitAfter(raw, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "[model."+grokManagedProfile+"]" {
			continue
		}
		end = i + 1
		for end < len(lines) && (strings.HasPrefix(lines[end], " ") || strings.HasPrefix(lines[end], "\t") || strings.TrimSpace(lines[end]) == "" || !strings.HasPrefix(lines[end], "[")) {
			end++
		}
		return i, end, strings.Join(lines[i:end], ""), true
	}
	return 0, 0, "", false
}

func ConfigureGrokProxy() (bool, string) {
	file := grokConfigFile()
	raw, exists := util.ReadFileSafe(file)
	if !exists {
		raw = ""
	}
	_, _, block, found := grokProfileBlock(raw)
	want := grokProfileText()
	if found {
		if block != want {
			if !strings.Contains(block, `base_url = ""`) {
				return false, file
			}
			healed := strings.Replace(raw, block, want, 1)
			if err := util.WriteFile(file, healed); err != nil {
				return false, file
			}
			return true, file
		}
		return false, file
	}
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	if err := util.WriteFile(file, raw+want); err != nil {
		return false, file
	}
	_ = util.WriteFile(grokProxyStateFile(), `{"present":false}`)
	return true, file
}

func RemoveGrokProxy() bool {
	file := grokConfigFile()
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false
	}
	start, end, block, found := grokProfileBlock(raw)
	if !found || block != grokProfileText() {
		return false
	}
	lines := strings.SplitAfter(raw, "\n")
	next := strings.Join(append(lines[:start], lines[end:]...), "")
	if err := util.WriteFile(file, next); err != nil {
		return false
	}
	_ = os.Remove(grokProxyStateFile())
	return true
}

func GrokProxyWired() bool {
	raw, ok := util.ReadFileSafe(grokConfigFile())
	if !ok {
		return false
	}
	_, _, block, found := grokProfileBlock(raw)
	return found && block == grokProfileText()
}

func detectGrokProxy(cap ProxyCapability) ProxyDetection {
	raw, err := readProxyConfig(grokConfigFile())
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "config file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "config unreadable", ProxyStateUnreadable)
	}
	_, _, block, found := grokProfileBlock(raw)
	if !found {
		return proxyDetection(cap.ID, "managed profile not configured", ProxyStateUnconfigured)
	}
	if block != grokProfileText() {
		return proxyDetection(cap.ID, "managed profile differs", ProxyStateConflict)
	}
	return proxyDetection(cap.ID, "exact managed profile", ProxyStateManaged)
}
