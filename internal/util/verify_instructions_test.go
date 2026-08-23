package util

import (
	"strings"
	"testing"
)

func TestAgentInstructionsContent(t *testing.T) {
	owners := []string{"principles", "caveman", "ponytail", "codegraph", "context-mode"}
	body := ToklessAgentBody(owners)

	for _, section := range []string{
		"## Principles",
		"## Response Style (caveman)",
		"## Build Discipline (ponytail)",
		"## Code Index (codegraph)",
		"## Context Tools (context-mode)",
	} {
		if !strings.Contains(body, section) {
			t.Errorf("MISSING section: %s", section)
		}
	}

	if strings.Contains(body, "hook") || strings.Contains(body, "Hook") {
		t.Error("instructions contain 'hook' — should be zero hooks")
	}
	for _, forbidden := range []string{"headroom_compress", "headroom_retrieve", "Headroom (headroom)"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("instructions contain retired Headroom MCP guidance: %s", forbidden)
		}
	}

	for _, kw := range []string{
		"Drop articles", "terse", "YAGNI", "codegraph_explore",
		"ctx_execute", "Think Before Coding", "Simplicity First",
		"Surgical Changes", "Goal-Driven Execution",
	} {
		if !strings.Contains(body, kw) {
			t.Errorf("MISSING keyword: %s", kw)
		}
	}
	t.Logf("body length: %d bytes", len(body))
}

func TestFullAgentBodyMatchesTemplate(t *testing.T) {
	if got, want := ToklessAgentBody(ToklessOwners)+"\n", normalizedAgentInstructions(); got != want {
		t.Fatal("full generated agent body drifted from embedded instructions template")
	}
}

func TestPerOwnerRendering(t *testing.T) {
	for _, tc := range []struct {
		owner   string
		marker  string
		example string
	}{
		{"caveman", "## Response Style (caveman)", "Drop articles"},
		{"ponytail", "## Build Discipline (ponytail)", "YAGNI"},
		{"codegraph", "## Code Index (codegraph)", "codegraph_explore"},
		{"context-mode", "## Context Tools (context-mode)", "ctx_execute"},
		{"principles", "## Principles", "Think Before Coding"},
	} {
		body := ToklessAgentBody([]string{tc.owner})
		if !strings.Contains(body, tc.marker) {
			t.Errorf("%s: missing marker %q", tc.owner, tc.marker)
		}
		if !strings.Contains(body, tc.example) {
			t.Errorf("%s: missing example keyword %q", tc.owner, tc.example)
		}
	}
}
