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
	"headroom",
	"projectmem",
}

// SectionsByOwner maps each owner to its heading marker.
var SectionsByOwner = map[string]string{
	"principles":   "## Principles",
	"caveman":      "## Response Style (caveman)",
	"ponytail":     "## Build Discipline (ponytail)",
	"codegraph":    "## Code Index (codegraph)",
	"context-mode": "## Context Tools (context-mode)",
	"headroom":     "## Context Compression (headroom)",
	"projectmem":   "## Project Memory (projectmem)",
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

// Skills always apply; tools only work when called. Keeping them apart stops
// the agent from treating a tool as advice and never calling it.
var passiveOwners = map[string]bool{
	"principles": true,
	"caveman":    true,
	"ponytail":   true,
}

var indexBulletByOwner = map[string]string{
	"principles":   "- **Principles** — think, simplify, edit surgically, verify.",
	"caveman":      "- **Response Style (caveman)** — terse prose, full technical accuracy.",
	"ponytail":     "- **Build Discipline (ponytail)** — reuse first, write only what must exist.",
	"codegraph":    "- **Code Index (codegraph)** — CALL `codegraph_explore` for structure, flows, callers. Not grep + read.",
	"context-mode": "- **Context Tools (context-mode)** — CALL `ctx_execute`, `ctx_execute_file`, `ctx_search`. Derive in-sandbox; keep raw bytes out.",
	"headroom":     "- **Context Compression (headroom)** — CALL `headroom_compress` on payloads that must enter context anyway.",
	"projectmem":   "- **Project Memory (projectmem)** — CALL `get_context` / `precheck_file` before coding, `record_fix` / `add_decision` after.",
}

// InstructionIndexBullets is every line the index can hold, for telling our
// index apart from the user's own text.
func InstructionIndexBullets() []string {
	out := make([]string, 0, len(indexBulletByOwner))
	for _, b := range indexBulletByOwner {
		out = append(out, b)
	}
	return out
}

// Everything in the template above the first section.
func instructionIndexHeader() string {
	body := strings.TrimRight(agentInstructionsTemplate, "\n")
	idx := strings.Index(body, "\n## ")
	if idx < 0 {
		return body
	}
	return strings.TrimRight(body[:idx], "\n")
}

// instructionIndexSection lists only wired owners, so the agent is never told
// to call a tool that isn't there.
func instructionIndexSection(owners []string) string {
	var skills, toolLines []string
	for _, owner := range ToklessOwners {
		bullet := indexBulletByOwner[owner]
		if bullet == "" || !hasOwner(owners, owner) {
			continue
		}
		if passiveOwners[owner] {
			skills = append(skills, bullet)
		} else {
			toolLines = append(toolLines, bullet)
		}
	}

	var b strings.Builder
	b.WriteString(instructionIndexHeader())
	if len(skills) > 0 {
		b.WriteString("\n\n**Skills — apply on every coding task. No tool call.**\n\n")
		b.WriteString(strings.Join(skills, "\n"))
	}
	if len(toolLines) > 0 {
		b.WriteString("\n\n**Tools — MCP functions. Invoke them; reading about them does nothing.**\n\n")
		b.WriteString(strings.Join(toolLines, "\n"))
	}
	return b.String()
}

// instructionSection prefers the downloaded copy and falls back to the one
// built into the binary, so a first run with no network still works.
func instructionSection(owner string) string {
	if body, ok := SkillContent(owner); ok {
		return strings.TrimRight(body, "\n")
	}
	return embeddedSection(owner)
}

func embeddedSection(owner string) string {
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
		b.WriteString(instructionIndexSection(owners))
		b.WriteString("\n\n")
	}
	for _, owner := range ToklessOwners {
		if !hasOwner(owners, owner) {
			continue
		}
		section := instructionSection(owner)
		if section == "" {
			continue
		}
		b.WriteString(section)
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
