package headroom

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestProxyAutostartUnavailableIsTyped(t *testing.T) {
	err := fmt.Errorf("%w: test", ErrProxyAutostartUnavailable)
	if !errors.Is(err, ErrProxyAutostartUnavailable) {
		t.Fatal("autostart availability error lost its type")
	}
}

func TestEnableProxyAutostartRefusedWhenTurnedOff(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	if err := util.SetProxyAutostartAllowed(false); err != nil {
		t.Fatal(err)
	}
	err := EnableProxyAutostart()
	if !errors.Is(err, ErrProxyAutostartUnavailable) || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("EnableProxyAutostart = %v, want turned-off ErrProxyAutostartUnavailable", err)
	}
}
