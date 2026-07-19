package commands

import (
	"reflect"
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

func TestNormalizeCmdBatchArgs(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    []string
	}{
		{"cmd", "cmd", []string{"/c", "C:/Users/user/AppData/Roaming/npm/codegraph.CMD", "serve"}, []string{"/c", `C:\Users\user\AppData\Roaming\npm\codegraph.CMD`, "serve"}},
		{"cmd exe upper C", `C:\Windows\System32\cmd.exe`, []string{"/C", "C:/tools/codegraph.bat"}, []string{"/C", `C:\tools\codegraph.bat`}},
		{"non cmd", "powershell", []string{"/c", "C:/tools/codegraph.cmd"}, []string{"/c", "C:/tools/codegraph.cmd"}},
		{"non batch", "cmd", []string{"/c", "echo", "C:/keep/slashes"}, []string{"/c", "echo", "C:/keep/slashes"}},
		{"short", "cmd", []string{"/c"}, []string{"/c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.args...)
			got := normalizedCmdBatchArgs(tt.command, tt.args, true)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("args = %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(tt.args, original) {
				t.Fatalf("input mutated: got %q, want %q", tt.args, original)
			}
		})
	}
}

func TestNormalizedCmdBatchArgsNonWindows(t *testing.T) {
	args := []string{"/c", "C:/tools/codegraph.cmd"}
	if got := normalizedCmdBatchArgs("cmd.exe", args, false); !reflect.DeepEqual(got, args) {
		t.Fatalf("non-Windows args changed: %q", got)
	}
}

func TestResolveMcpCommandDoesNotMutateArgv(t *testing.T) {
	argv := []string{"cmd", "/c", "C:/tools/codegraph.cmd", "serve"}
	original := append([]string(nil), argv...)
	_, _ = resolveMcpCommand("cmd", argv)
	if !reflect.DeepEqual(argv, original) {
		t.Fatalf("argv mutated: got %q, want %q", argv, original)
	}
}

func TestResolveMcpCommandShortArgvNoPanic(t *testing.T) {
	_, args := resolveMcpCommand("cmd", []string{"cmd"})
	if len(args) != 0 {
		t.Fatalf("expected empty args, got %v", args)
	}
}
