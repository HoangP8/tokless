package commands

import (
	"os"
	"os/exec"
	"testing"
)

func TestShellTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   \t\n", nil},
		{"simple", "git status", []string{"git", "status"}},
		{"double quotes", `find . -name "*.go"`, []string{"find", ".", "-name", "*.go"}},
		{"single quotes", `find . -name '*.go'`, []string{"find", ".", "-name", "*.go"}},
		{"mixed quotes", `find . -name "*.go" -path 'foo bar'`, []string{"find", ".", "-name", "*.go", "-path", "foo bar"}},
		{"backslash escape", `find . -name \*go`, []string{"find", ".", "-name", "*go"}},
		{"literal -not in double quotes", `echo "use -not to filter"`, []string{"echo", "use -not to filter"}},
		{"literal ! in single quotes", `grep '!=foo'`, []string{"grep", "!=foo"}},
		{"tabs and newlines", "find\t.\n -name\t*.go", []string{"find", ".", "-name", "*.go"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shellTokens(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("shellTokens(%q) = %v (len %d); want %v (len %d)", tc.input, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("shellTokens(%q)[%d] = %q; want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestFirstSegment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"single", "git status", "git status"},
		{"and-chain", "git add . && git commit", "git add ."},
		{"or-chain", "git status || echo fail", "git status"},
		{"semicolon", "find . -name foo ; ls", "find . -name foo"},
		{"pipe", "find . -name foo | head", "find . -name foo"},
		{"double pipe", "find . -name foo || head", "find . -name foo"},
		{"quoted pipe", `echo "a | b" && ls`, `echo "a | b"`},
		{"leading whitespace", "  git status", "git status"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := firstSegment(tc.input)
			if got != tc.want {
				t.Errorf("firstSegment(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRtkUnsafeFind(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Bug repro — must return true (passthrough).
		{"find with -not", `find . -name "*.go" -not -path "*/.*"`, true},
		{"find with -exec", `find . -name "*.go" -exec wc -l {} \;`, true},
		{"find with -size", `find . -size +1M`, true},
		{"find with -perm", `find . -perm 644`, true},
		{"find with -print0", `find . -print0`, true},
		{"find with -delete", `find . -name x -delete`, true},
		{"find with -or", `find . -name x -o -name y`, true},
		{"find with -a", `find . -name x -a -type f`, true},
		{"find with !", `find . ! -name "*.test.go"`, true},
		{"find with -regex", `find . -regex ".*\.go"`, true},
		{"find with -mtime", `find . -mtime -1`, true},

		// Compound: bad first segment → unsafe.
		{"bad find then git", `find . -delete; git status`, true},
		{"bad find then git (and)", `find . -exec rm {} \; && git status`, true},

		// Safe — must return false (rewrite allowed).
		{"bare find", `find .`, false},
		{"find -name only", `find . -name "*.go"`, false},
		{"find -name -type", `find . -name "*.go" -type f`, false},
		{"find -name -maxdepth", `find . -name "*.go" -maxdepth 3`, false},
		{"find -iname", `find . -iname "Makefile"`, false},
		{"find -type d", `find . -type d -name node_modules`, false},

		// Quoted literals must not false-positive.
		{"literal -not in filename", `find . -name "*-not-suffix"`, false},
		{"literal ! in single-quoted arg", `find . -name '!=foo'`, false},
		{"echo with -not literal", `echo "use -not to filter"`, false},

		// Non-find commands — never flag.
		{"git status", `git status`, false},
		{"cargo test", `cargo test`, false},
		{"empty", ``, false},
		{"whitespace only", `   `, false},
		{"bash with find inside string", `bash -c 'find . -name "*.go" -not -path x'`, false},

		// Compound: clean find first segment → safe (rtk handles rest).
		{"clean find and git", `find . -name foo && git status`, false},
		{"clean find ; git", `find . -type d; git status`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rtkUnsafeFind(tc.input)
			if got != tc.want {
				t.Errorf("rtkUnsafeFind(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestRtkRewriteHook is an integration check that exercises rtkRewrite against
// the actual installed rtk binary. Each case asserts the user-visible behavior:
// (1) bad input → empty string + false (passthrough, no broken command emitted)
// (2) good input → rewritten string + true (rtk prefix applied)
func TestRtkRewriteHook(t *testing.T) {
	if !utilHaveRtk() {
		t.Skip("rtk binary not installed; integration test skipped")
	}
	tests := []struct {
		name      string
		input     string
		wantPass  bool // true: must emit a rewritten rtk command
		wantEmpty bool // true: must return empty (passthrough)
	}{
		// User's bug case: must passthrough (not emit broken rtk find).
		{"user bug find -not", `find . -name "*.go" -not -path "*/.*"`, false, true},
		{"find -exec", `find . -name "*.go" -exec wc -l {} \;`, false, true},
		{"find -size", `find . -size +1M`, false, true},
		{"find -delete", `find . -name x -delete`, false, true},
		{"find bare", `find . -name x -delete; git status`, false, true},

		// Sanity: clean input must still rewrite.
		{"clean find", `find . -name "*.go" -type f`, true, false},
		{"clean find -maxdepth", `find . -name "*.go" -maxdepth 3`, true, false},
		{"git status", `git status`, true, false},
		{"cargo test", `cargo test`, true, false},
		{"git log", `git log --oneline -10`, true, false},

		// Quoted literals: git with -not in arg must still rewrite.
		{"git grep literal", `git log --grep=-not`, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := rtkRewrite(tc.input)
			if tc.wantEmpty && (changed || got != "") {
				t.Errorf("rtkRewrite(%q) = (%q, %v); want passthrough (empty, false)", tc.input, got, changed)
			}
			if tc.wantPass && !changed {
				t.Errorf("rtkRewrite(%q) = (%q, %v); want rewrite (non-empty, true)", tc.input, got, changed)
			}
			if tc.wantPass && got == tc.input {
				t.Errorf("rtkRewrite(%q) returned input unchanged; expected rtk-prefixed rewrite", tc.input)
			}
			if tc.wantPass && !containsRtkPrefix(got) {
				t.Errorf("rtkRewrite(%q) = %q; missing 'rtk ' prefix", tc.input, got)
			}
		})
	}
}

func containsRtkPrefix(s string) bool {
	for i := 0; i+4 <= len(s); i++ {
		if s[i:i+4] == "rtk " {
			return true
		}
	}
	return false
}

func utilHaveRtk() bool {
	for _, p := range []string{
		"/home/docker_user/.local/bin/rtk",
		"/usr/local/bin/rtk",
	} {
		if _, err := exec.LookPath("rtk"); err == nil {
			return true
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
