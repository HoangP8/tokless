package util

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFakeUV(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureUVUsesPathHitWithoutInstalling(t *testing.T) {
	dir := t.TempDir()
	uvPath := filepath.Join(dir, "uv")
	writeFakeUV(t, uvPath)
	t.Setenv("PATH", dir)
	t.Setenv("TOKLESS_TEST", "1")
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })

	called := false
	restore := SetUVInstallCmdForTest(func(context.Context, string, []string) error {
		called = true
		return nil
	})
	t.Cleanup(restore)

	got, err := EnsureUV(context.Background())
	if err != nil {
		t.Fatalf("EnsureUV: %v", err)
	}
	if got != uvPath {
		t.Fatalf("EnsureUV = %q, want %q", got, uvPath)
	}
	if called {
		t.Fatal("installer must not run when uv is already on PATH")
	}
}

func TestEnsureUVBootstrapsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("TOKLESS_TEST", "0")
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })

	uvPath := filepath.Join(dir, "uv")
	restore := SetUVInstallCmdForTest(func(_ context.Context, name string, args []string) error {
		if name == "" || len(args) == 0 {
			return errors.New("missing installer command")
		}
		writeFakeUV(t, uvPath)
		return nil
	})
	t.Cleanup(restore)

	got, err := EnsureUV(context.Background())
	if err != nil {
		t.Fatalf("EnsureUV: %v", err)
	}
	if got != uvPath {
		t.Fatalf("EnsureUV = %q, want re-resolved %q", got, uvPath)
	}
}

func TestEnsureUVInstallFailureMentionsDocs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("TOKLESS_TEST", "0")
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })

	restore := SetUVInstallCmdForTest(func(context.Context, string, []string) error {
		return errors.New("network unreachable")
	})
	t.Cleanup(restore)

	_, err := EnsureUV(context.Background())
	if err == nil {
		t.Fatal("EnsureUV must fail when the installer fails")
	}
	if !strings.Contains(err.Error(), "docs.astral.sh") {
		t.Fatalf("error must name the official docs, got: %v", err)
	}
}

func TestEnsureUVSkipsInstallerInTestMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("TOKLESS_TEST", "1")
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })

	called := false
	restore := SetUVInstallCmdForTest(func(context.Context, string, []string) error {
		called = true
		return nil
	})
	t.Cleanup(restore)

	if _, err := EnsureUV(context.Background()); err == nil {
		t.Fatal("EnsureUV must error when uv is absent in test mode")
	}
	if called {
		t.Fatal("installer must not run when TOKLESS_TEST=1")
	}
}

// TestEnsureUVTimesOutStalledInstaller: caller passes context.Background();
// a hung installer hook must still abort via EnsureUV's internal timeout.
func TestEnsureUVTimesOutStalledInstaller(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("TOKLESS_TEST", "0")
	SetHomeOverride(t.TempDir())
	t.Cleanup(func() { SetHomeOverride("") })

	origTimeout := ensureUVTimeout
	ensureUVTimeout = 100 * time.Millisecond
	t.Cleanup(func() { ensureUVTimeout = origTimeout })

	restore := SetUVInstallCmdForTest(func(ctx context.Context, name string, args []string) error {
		<-ctx.Done() // block until EnsureUV's timeout cancels the context
		return ctx.Err()
	})
	t.Cleanup(restore)

	start := time.Now()
	_, err := EnsureUV(context.Background())
	if err == nil {
		t.Fatal("EnsureUV must abort on a stalled installer")
	}
	if !strings.Contains(err.Error(), "uv install failed") {
		t.Fatalf("expected install-failed error, got: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline-exceeded cause, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("EnsureUV returned after %v, want well under 3s", elapsed)
	}
}

func TestUVInstallArgs(t *testing.T) {
	for _, tc := range []struct {
		goos string
		name string
		last string
	}{
		{"linux", "sh", "curl -LsSf https://astral.sh/uv/install.sh | sh"},
		{"darwin", "sh", "curl -LsSf https://astral.sh/uv/install.sh | sh"},
		{"windows", "powershell", "irm https://astral.sh/uv/install.ps1 | iex"},
	} {
		name, args := uvInstallArgs(tc.goos)
		if name != tc.name {
			t.Errorf("%s: name = %q, want %q", tc.goos, name, tc.name)
		}
		if len(args) == 0 || args[len(args)-1] != tc.last {
			t.Errorf("%s: args = %v, want last %q", tc.goos, args, tc.last)
		}
	}
}

func TestUVCandidatePathsPerOS(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Cleanup(func() { SetHomeOverride("") })

	unix := uvCandidatePaths("linux")
	if len(unix) != 2 || unix[0] != filepath.Join(home, ".local", "bin", "uv") || unix[1] != filepath.Join(home, ".cargo", "bin", "uv") {
		t.Fatalf("unix candidates = %v", unix)
	}
	if !strings.HasSuffix(unix[0], filepath.Join(".local", "bin", "uv")) {
		t.Fatalf("unix candidate missing .local/bin/uv: %v", unix)
	}

	win := uvCandidatePaths("windows")
	wantWin := []string{
		filepath.Join(home, ".local", "bin", "uv.exe"),
		filepath.Join(home, "AppData", "Local", "uv", "uv.exe"),
	}
	if len(win) != len(wantWin) {
		t.Fatalf("windows candidates = %v", win)
	}
	for i := range wantWin {
		if win[i] != wantWin[i] {
			t.Fatalf("windows candidates = %v, want %v", win, wantWin)
		}
	}
}
