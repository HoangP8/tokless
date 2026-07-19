package util

import (
	"path/filepath"
	"testing"
)

func TestInferInstallMethod(t *testing.T) {
	if IsWin {
		t.Skip("path shapes below are unix-specific")
	}
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	cases := []struct {
		exe  string
		want string
	}{
		{"/opt/homebrew/bin/tokless", "homebrew"},
		{"/usr/local/Cellar/tokless/1.0.0/bin/tokless", "homebrew"},
		{"/usr/lib/node_modules/tokless/node_modules/.bin/tokless", "npm"},
		{filepath.Join(home, "go", "bin", "tokless"), "go install"},
		{filepath.Join(home, ".local", "bin", "tokless"), "install script"},
		{"/Users/x/src/tokless/dist/release/tokless-darwin-arm64", "source build"},
		{"/usr/bin/tokless", "unknown"},
		{"tokless", "unknown"},
	}
	for _, c := range cases {
		if got := inferInstallMethod(c.exe); got != c.want {
			t.Errorf("inferInstallMethod(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}

// A marker pointing at a different binary is stale (installed one way, then
// rebuilt another) and must not be reported as exact.
func TestInstallInfoRejectsStaleMarker(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })

	if err := WriteInstallMarker("homebrew", filepath.Join(home, "nowhere", "tokless")); err != nil {
		t.Fatal(err)
	}
	if _, exact := InstallInfo(); exact {
		t.Fatal("stale marker reported as exact")
	}

	if err := WriteInstallMarker("homebrew", ToklessAbs()); err != nil {
		t.Fatal(err)
	}
	rec, exact := InstallInfo()
	if !exact || rec.Method != "homebrew" {
		t.Fatalf("matching marker not trusted: %+v exact=%v", rec, exact)
	}
}
