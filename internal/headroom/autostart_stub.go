//go:build !linux && !darwin && !windows

package headroom

import "fmt"

const proxyAutostartMechanism = ""

func stopProxySupervisorForPatch() error { return nil }
func stopByokGatewaySupervisor() error   { return nil }

func enableProxyAutostartOS() error {
	return fmt.Errorf("%w: unsupported on this OS; keeping proxy running for this session", ErrProxyAutostartUnavailable)
}
func DisableProxyAutostart() error   { return nil }
func ProxyAutostartEnabled() bool    { return false }
func ProxyAutostartConfigured() bool { return false }
