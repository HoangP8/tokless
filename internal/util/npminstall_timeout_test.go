package util

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNpmInstallTimeoutDefault(t *testing.T) {
	if npmInstallTimeout != 15*time.Minute {
		t.Fatalf("npmInstallTimeout = %v, want 15m", npmInstallTimeout)
	}
	if npmConfigTimeout != 30*time.Second {
		t.Fatalf("npmConfigTimeout = %v, want 30s", npmConfigTimeout)
	}
}

func TestNpmConfig_HonorsTimeout(t *testing.T) {
	origExec, origTimeout := runNpmExec, npmConfigTimeout
	defer func() { runNpmExec, npmConfigTimeout = origExec, origTimeout }()

	npmConfigTimeout = 100 * time.Millisecond

	var seen time.Duration
	runNpmExec = func(npmBin string, args, env []string, ctx context.Context) ExecResult {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("runNpmConfig did not pass a context with deadline")
		}
		seen = time.Until(deadline)
		return ExecResult{Code: 0, Stdout: "https://registry.npmjs.org/"}
	}

	runNpmConfig([]string{"config", "get", "registry"})
	if seen < 0 || seen > 200*time.Millisecond {
		t.Fatalf("expected deadline near 100ms, got %v", seen)
	}
}

func TestNpmRun_HonorsTimeout(t *testing.T) {
	origExec, origTimeout := runNpmExec, npmInstallTimeout
	defer func() { runNpmExec, npmInstallTimeout = origExec, origTimeout }()

	npmInstallTimeout = 100 * time.Millisecond

	var seen time.Duration
	runNpmExec = func(npmBin string, args, env []string, ctx context.Context) ExecResult {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("npmRun did not pass a context with deadline")
		}
		seen = time.Until(deadline)
		return ExecResult{Code: 0}
	}

	npmRun([]string{"install", "x"})
	if seen < 0 || seen > 200*time.Millisecond {
		t.Fatalf("expected deadline near 100ms, got %v", seen)
	}
}

func TestNpmRunEnv_HonorsTimeout(t *testing.T) {
	origExec, origTimeout := runNpmExec, npmInstallTimeout
	defer func() { runNpmExec, npmInstallTimeout = origExec, origTimeout }()
	npmInstallTimeout = 100 * time.Millisecond

	var seen time.Duration
	runNpmExec = func(npmBin string, args, env []string, ctx context.Context) ExecResult {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("npmRunEnv did not pass a context with deadline")
		}
		seen = time.Until(deadline)
		return ExecResult{Code: 0}
	}

	npmRunEnv([]string{"install", "x"}, []string{"FOO=bar"})
	if seen < 0 || seen > 200*time.Millisecond {
		t.Fatalf("expected deadline near 100ms, got %v", seen)
	}
}

func TestNpmRun_PassesEnv(t *testing.T) {
	origExec := runNpmExec
	defer func() { runNpmExec = origExec }()

	var gotEnv []string
	runNpmExec = func(npmBin string, args, env []string, ctx context.Context) ExecResult {
		gotEnv = env
		return ExecResult{Code: 0}
	}

	env := []string{"FOO=bar"}
	npmRunEnv([]string{"install", "x"}, env)
	if len(gotEnv) != 1 || gotEnv[0] != "FOO=bar" {
		t.Fatalf("env not passed through: %v", gotEnv)
	}
}

func TestNpmRun_MissingNpmDoesNotRun(t *testing.T) {
	origResolve := resolveNpmBinary
	defer func() { resolveNpmBinary = origResolve }()
	resolveNpmBinary = func() string { return "" }

	execCalled := false
	origExec := runNpmExec
	defer func() { runNpmExec = origExec }()
	runNpmExec = func(npmBin string, args, env []string, ctx context.Context) ExecResult {
		execCalled = true
		return ExecResult{Code: 0}
	}

	r := npmRun([]string{"install", "x"})
	if execCalled {
		t.Fatal("Run should not be called when npm binary missing")
	}
	if r.Code != 127 || !strings.Contains(r.Stderr, "npm not found") {
		t.Fatalf("want npm not found, got %+v", r)
	}
}
