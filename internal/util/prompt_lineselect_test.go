package util

import (
	"reflect"
	"testing"
)

// All options disabled: must print the list and return nil WITHOUT prompting
// (regression: raw-mode picker crashed on space / hung on arrows).
func TestMultiSelectLineAllDisabled(t *testing.T) {
	items := []MultiSelectOption{
		{Value: "claude", Label: "Claude Code", Disabled: true},
		{Value: "codex", Label: "Codex", Disabled: true},
	}
	if got := multiSelectLine("pick", items, true); got != nil {
		t.Errorf("expected nil for all-disabled options, got %v", got)
	}
}

func TestParseLineSelection(t *testing.T) {
	items := []MultiSelectOption{
		{Value: "a", Label: "A", Disabled: false},
		{Value: "b", Label: "B", Disabled: true},
		{Value: "c", Label: "C", Disabled: false},
	}
	numByIdx := []int{1, 0, 2}
	defaults := []string{"a"}

	tests := []struct {
		line     string
		expected []string
	}{
		{"", []string{"a"}},
		{"a", []string{"a", "c"}},
		{"2", []string{"c"}},
		{"1, 2", []string{"a", "c"}},
		{"junk", nil},
		{"A", []string{"a", "c"}},
	}

	for _, test := range tests {
		result := parseLineSelection(test.line, items, numByIdx, defaults)
		if !reflect.DeepEqual(result, test.expected) {
			t.Errorf("For line %q, expected %v, got %v", test.line, test.expected, result)
		}
	}
}
