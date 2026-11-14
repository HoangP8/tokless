package commands

import (
	"reflect"
	"strings"
	"testing"
)

func TestLastNonEmptyLines(t *testing.T) {
	input := "a\n\n\x1b[31mred\x1b[0m\nb\nc\nd"

	t.Run("n=3", func(t *testing.T) {
		got := lastNonEmptyLines(input, 3)
		want := []string{"b", "c", "d"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("lastNonEmptyLines(..., 3) = %v, want %v", got, want)
		}
	})

	t.Run("n=10", func(t *testing.T) {
		got := lastNonEmptyLines(input, 10)
		want := []string{"a", "red", "b", "c", "d"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("lastNonEmptyLines(..., 10) = %v, want %v", got, want)
		}
	})
}

func TestStripAnsi(t *testing.T) {
	input := "\x1b[32m✔\x1b[0m done\r"
	got := stripAnsi(input)
	want := "✔ done"

	if got != want {
		t.Errorf("stripAnsi(%q) = %q, want %q", input, got, want)
	}
}

func TestPrintFailureDetailKeepsDecisiveLineAndDedupes(t *testing.T) {
	logs := "first decisive line\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl10\nl10"
	got := lastUniqueLines(logs, failureDetailLines)
	if len(got) != 10 || got[0] != "first decisive line" || got[9] != "l10" {
		t.Fatalf("lastUniqueLines = %v", got)
	}
	out := captureStdout(t, func() { printFailureDetail(map[string]string{"agent": logs}) })
	if !strings.Contains(out, "first decisive line") {
		t.Fatalf("decisive first line dropped:\n%s", out)
	}
	many := strings.Repeat("dup\n", 5) + "a\nb"
	if got := lastUniqueLines(many, 12); !reflect.DeepEqual(got, []string{"dup", "a", "b"}) {
		t.Fatalf("dedupe = %v", got)
	}
}
