//go:build windows

package util

import (
	"path/filepath"
	"testing"
)

func TestRoutingFileLockContentionAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing.lock")
	release, err := acquireRoutingFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireRoutingFileLock(path); err == nil {
		t.Fatal("second exclusive lock succeeded")
	}
	release()
	reacquire, err := acquireRoutingFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	reacquire()
}
