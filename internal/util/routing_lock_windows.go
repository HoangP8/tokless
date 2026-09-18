//go:build windows

package util

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	routingKernel32     = syscall.NewLazyDLL("kernel32.dll")
	routingLockFileEx   = routingKernel32.NewProc("LockFileEx")
	routingUnlockFileEx = routingKernel32.NewProc("UnlockFileEx")
)

func acquireRoutingFileLock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped syscall.Overlapped
	const lockfileFailImmediately = 0x1
	const lockfileExclusiveLock = 0x2
	r, _, callErr := routingLockFileEx.Call(uintptr(file.Fd()), lockfileFailImmediately|lockfileExclusiveLock, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if r == 0 {
		_ = file.Close()
		return nil, fmt.Errorf("routing store lock: %w", callErr)
	}
	return func() {
		_, _, _ = routingUnlockFileEx.Call(uintptr(file.Fd()), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
		_ = file.Close()
	}, nil
}
