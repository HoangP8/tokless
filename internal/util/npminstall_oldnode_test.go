package util

import (
	"testing"
)

// TestNpmGlobalInstall_OldNodeFailsElegantly locks the contract: when every
// install strategy fails (the old-Node scenario), NpmGlobalInstall returns
// ("", false, nil) — no crash, no panic — and the caller can print the
// "all strategies failed" message.
func TestNpmGlobalInstall_OldNodeFailsElegantly(t *testing.T) {
	origResolve, origRun, origRead, origRunEnv := npmResolve, npmRun, npmReadInstalled, npmRunEnv
	defer func() {
		npmResolve, npmRun, npmReadInstalled, npmRunEnv = origResolve, origRun, origRead, origRunEnv
	}()

	npmResolve = func(pkg, spec string) (string, string, bool) {
		return "1.0.162", "https://registry.npmjs.org/context-mode/-/context-mode-1.0.162.tgz", true
	}
	npmRun = func(args []string) ExecResult {
		return ExecResult{Code: 1, Stderr: "gyp ERR! find VS\nnode-gyp build failed"}
	}
	npmReadInstalled = func(pkg string) *string { return nil }
	npmRunEnv = func(args, env []string) ExecResult {
		return ExecResult{Code: 1, Stderr: "node-gyp rebuild failed (node too old)"}
	}

	v, ok, err := NpmGlobalInstall("context-mode", "latest")
	if ok {
		t.Fatalf("expected install to fail on old node, got success: %q", v)
	}
	if err != nil {
		t.Fatalf("must NOT return error on exhaustion (caller prints message): %v", err)
	}
	if v != "" {
		t.Fatalf("expected empty version on failure, got %q", v)
	}
}

// TestNpmGlobalInstall_OldNodeFailsNoCrashNoBreak verifies the broader
// contract: even when npm itself is missing (ENOENT), the function returns
// gracefully rather than panicking.
func TestNpmGlobalInstall_NpmMissingNoCrash(t *testing.T) {
	origResolve, origRun, origRead, origRunEnv := npmResolve, npmRun, npmReadInstalled, npmRunEnv
	defer func() {
		npmResolve, npmRun, npmReadInstalled, npmRunEnv = origResolve, origRun, origRead, origRunEnv
	}()

	npmResolve = func(pkg, spec string) (string, string, bool) { return "", "", false }
	npmRun = func(args []string) ExecResult { return ExecResult{Code: 127, Stderr: "npm: not found"} }
	npmReadInstalled = func(pkg string) *string { return nil }
	npmRunEnv = func(args, env []string) ExecResult { return ExecResult{Code: 127, Stderr: "npm: not found"} }

	v, ok, err := NpmGlobalInstall("context-mode", "latest")
	if ok || err != nil || v != "" {
		t.Fatalf("missing npm must yield (\"\", false, nil), got (%q, %v, %v)", v, ok, err)
	}
}

// TestFirstNpmErrorLine confirms the helper surfaces the first meaningful
// npm error line so the user sees WHY it failed, not just "all strategies failed".
func TestFirstNpmErrorLine(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		stdout string
		want   string
	}{
		{"stderr first line", "npm ERR! code EBADENGINE\nmore", "", "npm ERR! code EBADENGINE"},
		{"empty stderr uses stdout", "", "gyp ERR! build failed\nx", "gyp ERR! build failed"},
		{"both empty", "", "", "no output"},
		{"whitespace only", "  \n  ", "", "no output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstNpmLine(tt.stderr, tt.stdout)
			if got != tt.want {
				t.Fatalf("firstNpmLine(%q, %q) = %q, want %q", tt.stderr, tt.stdout, got, tt.want)
			}
		})
	}
}