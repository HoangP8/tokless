package util

import (
	"os"
	"path/filepath"
	"testing"
)

// sandboxHome redirects Home + ToklessDataDir into t.TempDir (cross-OS).
func sandboxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	SetHomeOverride(home)
	if IsWin {
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
	t.Cleanup(func() { SetHomeOverride("") })
	_ = os.Remove(InstallMarkerPath())
	return home
}

func TestInferInstallMethod(t *testing.T) {
	home := sandboxHome(t)
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	cases := []struct {
		exe  string
		want string
	}{
		{filepath.Join(home, "go", "bin", "tokless"), "go install"},
		{filepath.Join(home, ".local", "bin", "tokless"), "install script"},
		{"tokless", "unknown"},
	}
	if IsWin {
		local := filepath.Join(home, "AppData", "Local")
		t.Setenv("LOCALAPPDATA", local)
		cases = append(cases,
			struct{ exe, want string }{filepath.Join(local, "Programs", "tokless", "tokless.exe"), "install script"},
			struct{ exe, want string }{filepath.Join(home, "AppData", "Roaming", "npm", "node_modules", "tokless", "node_modules", ".bin", "tokless.cmd"), "npm"},
			struct{ exe, want string }{`C:\Windows\System32\tokless.exe`, "unknown"},
		)
	} else {
		cases = append(cases,
			struct{ exe, want string }{"/opt/homebrew/bin/tokless", "homebrew"},
			struct{ exe, want string }{"/usr/local/Cellar/tokless/1.0.0/bin/tokless", "homebrew"},
			struct{ exe, want string }{"/usr/lib/node_modules/tokless/node_modules/.bin/tokless", "npm"},
			struct{ exe, want string }{"/Users/x/src/tokless/dist/release/tokless-darwin-arm64", "source build"},
			struct{ exe, want string }{"/usr/bin/tokless", "unknown"},
		)
	}
	for _, c := range cases {
		if got := inferInstallMethod(c.exe); got != c.want {
			t.Errorf("inferInstallMethod(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}

func TestInstallInfoRejectsStaleMarker(t *testing.T) {
	home := sandboxHome(t)

	if err := WriteInstallMarker("homebrew", filepath.Join(home, "nowhere", "tokless"), "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, exact := InstallInfo(); exact {
		t.Fatal("stale marker reported as exact")
	}

	if err := WriteInstallMarker("homebrew", ToklessAbs(), "1.0.0"); err != nil {
		t.Fatal(err)
	}
	rec, exact := InstallInfo()
	if !exact || rec.Method != "homebrew" {
		t.Fatalf("matching marker not trusted: %+v exact=%v", rec, exact)
	}
}

func TestRefreshInstallMarkerAfterSelfUpdate(t *testing.T) {
	_ = sandboxHome(t)

	if err := WriteInstallMarker("install script", ToklessAbs(), "0.2.6"); err != nil {
		t.Fatal(err)
	}
	RefreshInstallMarker("0.2.7")

	rec, exact := InstallInfo()
	if !exact {
		t.Fatal("marker not exact after refresh")
	}
	if rec.Version != "0.2.7" {
		t.Errorf("version = %q, want 0.2.7 (stale version survived self-update)", rec.Version)
	}
	if rec.Method != "install script" {
		t.Errorf("method = %q, want the original channel preserved", rec.Method)
	}
}

func TestRefreshInstallMarkerWithoutPrior(t *testing.T) {
	_ = sandboxHome(t)

	RefreshInstallMarker("0.2.7")
	rec, exact := InstallInfo()
	if !exact || rec.Method != "self-update" || rec.Version != "0.2.7" {
		t.Fatalf("got %+v exact=%v, want self-update/0.2.7/exact", rec, exact)
	}
}
