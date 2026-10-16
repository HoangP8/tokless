package headroom

import (
	"os"
	"runtime"
	"testing"
)

func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(why)
	}
}

func requireProcessIdentity(t *testing.T) {
	t.Helper()
	if _, err := proxyIdentity(os.Getpid()); err != nil {
		t.Skipf("process identity unavailable: %v", err)
	}
}
