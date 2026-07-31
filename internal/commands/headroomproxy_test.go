package commands

import (
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestHeadroomProxyModeIsRemembered(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	defer util.SetHomeOverride("")
	t.Setenv("TOKLESS_TEST", "1")

	if util.Exists(proxyDeclinedMarker()) {
		t.Fatal("nothing declined yet")
	}
	if code := RunHeadroomProxy("off"); code != 0 {
		t.Fatalf("off = %d", code)
	}
	if !util.Exists(proxyDeclinedMarker()) {
		t.Error("declining should be remembered, or every install asks again")
	}
	if code := RunHeadroomProxy("on"); code != 0 {
		t.Fatalf("on = %d", code)
	}
	if util.Exists(proxyDeclinedMarker()) {
		t.Error("turning it on should clear the earlier decline")
	}
}

func TestHeadroomProxyRejectsUnknownMode(t *testing.T) {
	if code := RunHeadroomProxy("sideways"); code == 0 {
		t.Error("unknown mode should not report success")
	}
}
