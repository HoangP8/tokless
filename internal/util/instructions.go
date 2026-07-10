package util

import (
	_ "embed"
	"strings"
)

// ToklessOwners is render order: meta rules first, then tools.
var ToklessOwners = []string{
	"principles",
	"caveman",
	"ponytail",
	"codegraph",
	"context-mode",
}

// SectionsByOwner maps each owner to its heading marker.
var SectionsByOwner = map[string]string{
	"principles":   "## Principles",
	"caveman":      "## Response Style (caveman)",
	"ponytail":     "## Build Discipline (ponytail)",
	"codegraph":    "## Code Index (codegraph)",
	"context-mode": "## Context Tools (context-mode)",
}

var legacySectionsByOwner = map[string][]string{
	"principles":   {"## 1. Principles", "## Principles (craft) →", "## Principles (craft)"},
	"caveman":      {"## 2. Response Style", "## Response Style", "## Style", "## Caveman Style", "## Caveman", "## Voice (caveman)", "## Response Style (caveman)"},
	"ponytail":     {"## 3. Build Discipline", "## Build Discipline", "## Build Less", "## Ponytail", "## Ponytail: Build Less", "## Reuse Ladder (ponytail)", "## Lazy Ladder (ponytail)", "## Build Discipline (ponytail)"},
	"codegraph":    {"## 4. Code Search", "## Codegraph", "## Codegraph — MUST USE FOR CODE", "## Code Index (codegraph)"},
	"context-mode": {"## 5. Context Control", "## Context Tools", "## Context Tools — MUST USE FOR DATA", "## Context Tools (context-mode)"},
}

func SectionPresent(body, owner string) bool {
	for _, marker := range SectionMarkers(owner) {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

func SectionMarkers(owner string) []string {
	marker, ok := SectionsByOwner[owner]
	if !ok {
		return nil
	}
	markers := []string{marker}
	markers = append(markers, legacySectionsByOwner[owner]...)
	return markers
}

//go:embed agent_instructions.md
var agentInstructionsTemplate string

func instructionIndexSection() string {
	body := strings.TrimRight(agentInstructionsTemplate, "\n")
	idx := strings.Index(body, "\n## ")
	if idx < 0 {
		return body
	}
	return body[:idx]
}

func instructionSection(owner string) string {
	marker := SectionsByOwner[owner]
	if marker == "" {
		return ""
	}
	body := strings.TrimRight(agentInstructionsTemplate, "\n")
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	if start > 0 {
		start = strings.LastIndex(body[:start], "\n") + 1
	}
	rest := body[start:]
	if idx := strings.Index(rest[1:], "\n## "); idx >= 0 {
		return strings.TrimRight(rest[:idx+1], "\n")
	}
	return strings.TrimRight(rest, "\n")
}

// ToklessAgentBody renders the full markdown body for the given owners.
func ToklessAgentBody(owners []string) string {
	var b strings.Builder

	if len(owners) >= 2 {
		b.WriteString(instructionIndexSection())
		b.WriteString("\n\n")
	}
	if len(owners) > 0 {
		b.WriteString(instructionSection("principles"))
		b.WriteString("\n\n")
	}
	if hasOwner(owners, "caveman") {
		b.WriteString(instructionSection("caveman"))
		b.WriteString("\n\n")
	}
	if hasOwner(owners, "ponytail") {
		b.WriteString(instructionSection("ponytail"))
		b.WriteString("\n\n")
	}
	if hasOwner(owners, "codegraph") {
		b.WriteString(instructionSection("codegraph"))
		b.WriteString("\n\n")
	}
	if hasOwner(owners, "context-mode") {
		b.WriteString(instructionSection("context-mode"))
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}


// TokenizeBody infers active owners from section headings present in body.
func TokenizeBody(body string) []string {
	var out []string
	for _, owner := range ToklessOwners {
		if SectionPresent(body, owner) {
			out = append(out, owner)
		}
	}
	return out
}

func hasOwner(owners []string, want string) bool {
	for _, o := range owners {
		if o == want {
			return true
		}
	}
	return false
}
