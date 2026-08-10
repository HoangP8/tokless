package util

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
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

// CursorProjectRuleSpec describes one checked-in Cursor project rule.
type CursorProjectRuleSpec struct {
	Filename    string
	Description string
	Owner       string
}

var cursorProjectRuleSpecs = []CursorProjectRuleSpec{
	{Filename: "principles.mdc", Description: "This rule guides software work toward simple, correct, and purposeful solutions.", Owner: "principles"},
	{Filename: "response-style.mdc", Description: "This rule provides standards for concise, direct, and technically accurate communication.", Owner: "caveman"},
	{Filename: "build-discipline.mdc", Description: "This rule guides simple, focused implementations that reuse existing solutions and avoid unnecessary work.", Owner: "ponytail"},
	{Filename: "code-index.mdc", Description: "This rule guides codebase exploration through relevant symbols, flows, dependencies, and affected areas.", Owner: "codegraph"},
	{Filename: "context-tools.mdc", Description: "This rule guides efficient context collection, analysis, and retrieval while keeping raw data focused.", Owner: "context-mode"},
}

// CursorProjectRuleSpecs returns the Cursor project rule specifications.
func CursorProjectRuleSpecs() []CursorProjectRuleSpec {
	return append([]CursorProjectRuleSpec(nil), cursorProjectRuleSpecs...)
}

// CursorProjectRuleContent renders one Cursor project rule from embedded instructions.
func CursorProjectRuleContent(spec CursorProjectRuleSpec) string {
	return "---\ndescription: " + spec.Description + "\nalwaysApply: true\n---\n" + instructionSection(spec.Owner) + "\n"
}

// InstallCursorProjectRules installs Cursor rules in workspace, never overwriting
// existing files.
func InstallCursorProjectRules(workspace string, dryRun bool) (bool, error) {
	if workspace == "" {
		return false, fmt.Errorf("Cursor project rules require non-empty workspace")
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return false, fmt.Errorf("stat Cursor workspace: %w", err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("Cursor workspace is not a directory: %s", workspace)
	}
	if dryRun {
		return true, nil
	}
	rulesDir := filepath.Join(workspace, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return false, fmt.Errorf("create Cursor rules directory: %w", err)
	}
	for _, spec := range cursorProjectRuleSpecs {
		path := filepath.Join(rulesDir, spec.Filename)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(CursorProjectRuleContent(spec)), 0o644); err != nil {
			return false, fmt.Errorf("write Cursor rule %s: %w", spec.Filename, err)
		}
	}
	return true, nil
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
	body := strings.TrimRight(normalizedAgentInstructions(), "\n")
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
	body := strings.TrimRight(normalizedAgentInstructions(), "\n")
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

func normalizedAgentInstructions() string {
	return strings.ReplaceAll(agentInstructionsTemplate, "\r\n", "\n")
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
