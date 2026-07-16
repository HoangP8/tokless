package util

import (
	"testing"
)

func TestRunningInsideClaudeCode(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"none", nil, false},
		{"CLAUDECODE_1", map[string]string{"CLAUDECODE": "1"}, true},
		{"CLAUDECODE_0", map[string]string{"CLAUDECODE": "0"}, false},
		{"CHILD_SESSION", map[string]string{"CLAUDE_CODE_CHILD_SESSION": "1"}, true},
		{"CHILD_SESSION_0", map[string]string{"CLAUDE_CODE_CHILD_SESSION": "0"}, false},
		{"both", map[string]string{"CLAUDECODE": "1", "CLAUDE_CODE_CHILD_SESSION": "1"}, true},
		{"other_env", map[string]string{"TERM": "xterm", "NO_COLOR": "1"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// t.Setenv saves/restores automatically — no manual cleanup needed.
			for _, k := range []string{"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION"} {
				if v, set := c.env[k]; set {
					t.Setenv(k, v)
				} else {
					// t.Setenv panics if env var was not already set; use os.Unsetenv via helper
					t.Setenv(k, "")
				}
			}
			got := RunningInsideClaudeCode()
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestInteractiveTTYDisabledInClaudeCode(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	if !RunningInsideClaudeCode() {
		t.Fatal("expected RunningInsideClaudeCode=true")
	}
	if interactiveTTY() {
		t.Fatal("interactiveTTY should be false inside claude code")
	}
}

func TestEraseStyledLineSafeInClaudeCode(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	got := EraseStyledLine("foo")
	if got != "" {
		t.Fatalf("EraseStyledLine should return empty in claude code, got %q", got)
	}
}