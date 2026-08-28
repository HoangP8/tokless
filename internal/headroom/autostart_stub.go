//go:build !linux && !darwin && !windows

package headroom

func EnableProxyAutostart() error  { return nil }
func DisableProxyAutostart() error { return nil }
func ProxyAutostartEnabled() bool  { return false }
