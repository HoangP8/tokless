package commands

import (
	"reflect"
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
