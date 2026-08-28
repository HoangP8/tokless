//go:build !windows

package headroom

import "fmt"

func requestProxyStop() error           { return nil }
func clearProxyStopRequest() error      { return nil }
func proxyStopRequested() (bool, error) { return false, nil }

func RunProxyWatchdog() error { return fmt.Errorf("proxy watchdog is only supported on Windows") }
