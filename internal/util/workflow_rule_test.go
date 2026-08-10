package util

import (
	"strings"
	"testing"
)

func TestCursorRulesMatchAgentInstructions(t *testing.T) {
	want := CursorProjectRuleSpecs()

	seen := make(map[string]bool, len(want))
	for _, spec := range want {
		if seen[spec.Filename] {
			t.Fatalf("duplicate rule %q", spec.Filename)
		}
		seen[spec.Filename] = true
		content := CursorProjectRuleContent(spec)
		if strings.Contains(content, "\r") {
			t.Errorf("%s must use LF line endings", spec.Filename)
		}
		if !strings.HasSuffix(content, "\n") || strings.HasSuffix(content, "\n\n") {
			t.Errorf("%s must have exactly one trailing newline", spec.Filename)
		}

		const prefix = "---\ndescription: "
		if !strings.HasPrefix(content, prefix) {
			t.Fatalf("%s has invalid frontmatter start", spec.Filename)
		}
		closing := strings.Index(content[len(prefix):], "\n---\n")
		if closing < 0 {
			t.Fatalf("%s has invalid frontmatter end", spec.Filename)
		}
		closing += len(prefix)
		frontmatter := content[:closing+len("\n---\n")]
		wantFrontmatter := "---\ndescription: " + spec.Description + "\nalwaysApply: true\n---\n"
		if frontmatter != wantFrontmatter {
			t.Errorf("%s frontmatter mismatch:\n got %q\nwant %q", spec.Filename, frontmatter, wantFrontmatter)
		}

		body := content[len(frontmatter):]
		if content != CursorProjectRuleContent(spec) || body != instructionSection(spec.Owner)+"\n" {
			t.Errorf("%s body does not match generated embedded source section", spec.Filename)
		}
	}

}

func TestCursorProjectRuleContentNormalizesEmbeddedCRLF(t *testing.T) {
	original := agentInstructionsTemplate
	agentInstructionsTemplate = strings.ReplaceAll(strings.ReplaceAll(original, "\r\n", "\n"), "\n", "\r\n")
	t.Cleanup(func() { agentInstructionsTemplate = original })

	if got := CursorProjectRuleContent(CursorProjectRuleSpecs()[0]); strings.Contains(got, "\r") {
		t.Errorf("Cursor rule content contains CRLF")
	}
}
