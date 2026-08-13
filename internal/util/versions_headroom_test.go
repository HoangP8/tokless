package util

import "testing"

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
