package headroom

import (
	"errors"
	"fmt"

	"github.com/HoangP8/tokless/internal/util"
)

var ErrProxyAutostartUnavailable = errors.New("proxy autostart unavailable")

// EnableProxyAutostart registers the proxy to start at login.
func EnableProxyAutostart() error {
	if !util.ProxyAutostartAllowed() {
		return fmt.Errorf("%w: turned off (run: tokless proxy autostart on)", ErrProxyAutostartUnavailable)
	}
	return enableProxyAutostartOS()
}

// ProxyAutostartMechanism names the OS login mechanism for user-facing text.
func ProxyAutostartMechanism() string { return proxyAutostartMechanism }
