package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGatherVersionsReportsHeadroomPresence(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { SetHomeOverride("") })
	info, ok := GatherVersions()["headroom"]
	if !ok || info.Channel != "uv" || info.Present {
		t.Fatalf("headroom version info = %#v, exists=%v", info, ok)
	}
}

func TestHeadroomDistInfoVersionPosixAndWindowsLayouts(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	site := filepath.Join(HeadroomPathsResolved().Tools, "headroom-ai", "lib", "python3.13", "site-packages")
	if err := os.MkdirAll(filepath.Join(site, "headroom_ai-0.35.0.dist-info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v := headroomDistInfoVersion(); v == nil || *v != "0.35.0" {
		t.Fatalf("posix layout: got %v, want 0.35.0", v)
	}
	winSite := filepath.Join(HeadroomPathsResolved().Tools, "headroom-ai", "Lib", "site-packages")
	if err := os.MkdirAll(filepath.Join(winSite, "headroom_ai-0.37.0.dist-info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v := headroomDistInfoVersion(); v == nil || *v != "0.37.0" {
		t.Fatalf("windows layout wins on higher semver: got %v, want 0.37.0", v)
	}
	if err := os.MkdirAll(filepath.Join(site, "headroom_ai-0.36.0.dist-info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v := headroomDistInfoVersion(); v == nil || *v != "0.37.0" {
		t.Fatalf("max semver across dist-infos: got %v, want 0.37.0", v)
	}
}
