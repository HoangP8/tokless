package commands

import (
	"testing"
)

func TestMcpChildEnvPassThrough(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/root", "TERM=xterm-256color", "NO_COLOR="}

	// Env passed through unchanged regardless of CLAUDECODE
	t.Run("claude_code", func(t *testing.T) {
		t.Setenv("CLAUDECODE", "1")
		got := mcpChildEnv(base)
		if len(got) != len(base) {
			t.Fatalf("env should be unchanged:\n got=%v\n want=%v", got, base)
		}
		for i := range got {
			if got[i] != base[i] {
				t.Fatalf("env[%d] changed: got %q, want %q", i, got[i], base[i])
			}
		}
	})

	t.Run("non_claude", func(t *testing.T) {
		got := mcpChildEnv(base)
		if len(got) != len(base) {
			t.Fatalf("env should be unchanged:\n got=%v\n want=%v", got, base)
		}
		for i := range got {
			if got[i] != base[i] {
				t.Fatalf("env[%d] changed: got %q, want %q", i, got[i], base[i])
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := mcpChildEnv(nil)
		if got != nil {
			t.Fatalf("nil env should return nil, got %v", got)
		}
	})
}
