package tools

import (
	"regexp"

	"github.com/HoangP8/tokless/internal/util"
)

func EnsureInstructionSeparators(agentIDs []string) {
	for _, id := range agentIDs {
		var path string
		switch id {
		case "claude":
			path = util.ClaudeCodePaths().Instructions
		case "opencode":
			path = util.OpenCodePathsResolved().Instructions
		case "codex":
			path = util.CodexPathsResolved().Instructions
		case "antigravity":
			path = util.AntigravityPathsResolved().Instructions
		default:
			continue
		}
		normalizeSeparators(path)
	}
}

// Match any <!-- ... end ... --> (case-insensitive: end / END / End).
var endMarkerRe = regexp.MustCompile(`(?i)<!-- [^>]+[_-]end(?:[^>]*)? -->`)
var startMarkerRe = regexp.MustCompile(`(?i)<!-- [^>]+[_-](?:start|begin)(?:[^>]*)? -->`)

func normalizeSeparators(path string) {
	raw, ok := util.ReadFileSafe(path)
	if !ok || raw == "" {
		return
	}
	matches := endMarkerRe.FindAllStringIndex(raw, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		em := matches[i]
		eAbs := em[1]
		nextStart := startMarkerRe.FindStringIndex(raw[eAbs:])
		if nextStart == nil {
			continue
		}
		between := raw[eAbs : eAbs+nextStart[0]]
		nl := countNewlines(between)
		if nl == 2 {
			continue
		}
		newBetween := "\n\n"
		raw = raw[:eAbs] + newBetween + raw[eAbs+nextStart[0]:]
		_ = util.WriteFile(path, raw)
	}
}

func countNewlines(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			n++
		}
	}
	return n
}
