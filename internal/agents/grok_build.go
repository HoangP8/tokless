package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const grokBuildMarkerStart = "# --- headroom:grok-build:start ---"
const grokBuildMarkerEnd = "# --- headroom:grok-build:end ---"

var grokBuildBlockRE = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(grokBuildMarkerStart) + `.*?` + regexp.QuoteMeta(grokBuildMarkerEnd) + `\n?`)

var grokBuildTableRE = regexp.MustCompile(`(?m)^\[model\.grok-build\]\s*(?:#[^\n]*)?$`)
var grokBuildNextTableRE = regexp.MustCompile(`(?m)^\[`)


func grokBuildHomeDir() string {
	if v := strings.TrimSpace(os.Getenv("GROK_HOME")); v != "" {
		return v
	}
	return filepath.Join(util.Home(), ".grok")
}

func grokBuildConfigFile() string { return filepath.Join(grokBuildHomeDir(), "config.toml") }

func stripGrokBuildBlocks(content string) string {
	stripped := grokBuildBlockRE.ReplaceAllString(content, "")
	if _, start, end := grokBuildTableSection(stripped); start >= 0 {
		stripped = stripped[:start] + stripped[end:]
	}
	if stripped == content {
		return content
	}
	stripped = regexp.MustCompile(`\n{3,}`).ReplaceAllString(stripped, "\n\n")
	return strings.TrimRight(stripped, "\n")
}


func nextGrokBuildTableIndex(content string, from int) int {
	loc := grokBuildNextTableRE.FindStringIndex(content[from:])
	if loc == nil {
		return len(content)
	}
	return from + loc[0]
}

func grokBuildTableSection(content string) (string, int, int) {
	match := grokBuildTableRE.FindStringIndex(content)
	if match == nil {
		return "", 0, 0
	}
	sectionStart := match[1]
	sectionEnd := nextGrokBuildTableIndex(content, sectionStart)
	return content[match[0]:sectionEnd], match[0], sectionEnd
}

func RemoveGrokBuildProxy() bool {
	removed := false
	_ = withProxyRouteStashLock(func() error {
		removed = removeGrokBuildProxyLocked()
		return nil
	})
	return removed
}

func removeGrokBuildProxyLocked() bool {
	cfg := grokBuildConfigFile()
	data, err := os.ReadFile(cfg)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		return false
	}
	stripped := stripGrokBuildBlocks(string(data))
	if strings.TrimSpace(stripped) == "" {
		_ = os.Remove(cfg)
		return true
	}
	if stripped == string(data) {
		return false
	}
	return util.WriteFileAtomic(cfg, stripped, 0o600) == nil
}
