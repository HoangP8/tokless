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

func TestHeadroomDistInfoVersionUVToolLayout(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	site := filepath.Join(home, ".local", "share", "uv", "tools", "headroom-ai", "lib", "python3.13", "site-packages")
	dist := filepath.Join(site, "headroom_ai-0.35.0.dist-info")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "METADATA"), []byte("Metadata-Version: 2.1\nName: headroom-ai\nVersion: 0.35.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := HeadroomMetadataVersion(); v == nil || *v != "0.35.0" {
		t.Fatalf("uv tool layout: got %v, want 0.35.0", v)
	}
	if v := HeadroomInstalledVersion(); v == nil || *v != "0.35.0" {
		t.Fatalf("metadata fast path: got %v, want 0.35.0", v)
	}
	if err := os.WriteFile(filepath.Join(dist, "METADATA"), []byte("Version: 0.36.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := headroomDistInfoVersion(); v == nil || *v != "0.36.0" {
		t.Fatalf("METADATA parse: got %v, want 0.36.0", v)
	}
	other := t.TempDir()
	t.Setenv("UV_TOOL_DIR", other)
	otherDist := filepath.Join(other, "headroom-ai", "Lib", "site-packages", "headroom_ai-0.38.0.dist-info")
	if err := os.MkdirAll(otherDist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDist, "METADATA"), []byte("Version: 0.38.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := headroomDistInfoVersion(); v == nil || *v != "0.38.0" {
		t.Fatalf("UV_TOOL_DIR layout: got %v, want 0.38.0", v)
	}
	if err := os.Remove(filepath.Join(otherDist, "METADATA")); err != nil {
		t.Fatal(err)
	}
	if v := headroomDistInfoVersion(); v == nil || *v != "0.38.0" {
		t.Fatalf("dirname fallback: got %v, want 0.38.0", v)
	}
}
