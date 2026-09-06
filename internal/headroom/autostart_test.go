package headroom

import (
	"errors"
	"fmt"
	"testing"
)

func TestProxyAutostartUnavailableIsTyped(t *testing.T) {
	err := fmt.Errorf("%w: test", ErrProxyAutostartUnavailable)
	if !errors.Is(err, ErrProxyAutostartUnavailable) {
		t.Fatal("autostart availability error lost its type")
	}
}
