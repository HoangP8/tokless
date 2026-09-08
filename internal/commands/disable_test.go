package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestRunPurgeSkipsRemoveAllWhenStopFails(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("PATH", home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	root := util.HeadroomPathsResolved().Root
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "keep-me")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed := 0
	if got := runPurgeWith(purgeOps{
		proxyRunning:                 func() bool { return false },
		proxyAutostartEnabled:        func() bool { return false },
		startProxy:                   func() error { return nil },
		enableAutostart:              func() error { return nil },
		copilotRunning:               func() bool { return false },
		startCopilotProxy:            func() error { return nil },
		acquireLifecycle:             func() (func(), error) { return func() {}, nil },
		stopProxyPreservingAutostart: func() error { return errors.New("stop boom") },
		stopCopilotProxy:             func() error { return nil },
		disableAutostart:             func() error { return nil },
		removeAll:                    func(string) error { removed++; return nil },
		removeFile:                   os.Remove,
	}); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}
	if removed != 0 {
		t.Fatalf("RemoveAll called %d times after stop failure", removed)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("headroom root must survive stop failure: %v", err)
	}
}

func TestRunPurgeRemovesRootWhenStopsSucceed(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("PATH", home) // no real rtk/npm/pi
	t.Cleanup(func() { util.SetHomeOverride("") })
	var removedPath string
	if got := runPurgeWith(purgeOps{
		proxyRunning:                 func() bool { return false },
		proxyAutostartEnabled:        func() bool { return false },
		startProxy:                   func() error { return nil },
		enableAutostart:              func() error { return nil },
		copilotRunning:               func() bool { return false },
		startCopilotProxy:            func() error { return nil },
		acquireLifecycle:             func() (func(), error) { return func() {}, nil },
		stopProxyPreservingAutostart: func() error { return nil },
		stopCopilotProxy:             func() error { return nil },
		disableAutostart:             func() error { return nil },
		removeAll:                    func(path string) error { removedPath = path; return nil },
		removeFile:                   os.Remove,
	}); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if removedPath != util.HeadroomPathsResolved().Root {
		t.Fatalf("RemoveAll path = %q, want %q", removedPath, util.HeadroomPathsResolved().Root)
	}
}

func TestRunPurgeFailsWhenRtkBinaryRemovalFails(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("PATH", home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	rtk := filepath.Join(home, "rtk")
	if err := os.WriteFile(rtk, []byte("#!/bin/sh\nprintf 'rtk 1.0.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	removedRoot := false
	if got := runPurgeWith(purgeOps{
		proxyRunning:                 func() bool { return false },
		proxyAutostartEnabled:        func() bool { return false },
		startProxy:                   func() error { return nil },
		enableAutostart:              func() error { return nil },
		copilotRunning:               func() bool { return false },
		startCopilotProxy:            func() error { return nil },
		acquireLifecycle:             func() (func(), error) { return func() {}, nil },
		stopProxyPreservingAutostart: func() error { return nil },
		stopCopilotProxy:             func() error { return nil },
		disableAutostart:             func() error { return nil },
		removeAll:                    func(string) error { removedRoot = true; return nil },
		removeFile:                   func(string) error { return errors.New("remove boom") },
	}); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}
	if !removedRoot {
		t.Fatal("RemoveAll was not attempted after RTK file removal failure")
	}
}
