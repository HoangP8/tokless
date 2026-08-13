//go:build windows

package headroom

import (
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x00000001
	moveFileWriteThrough    = 0x00000008
)

var (
	replaceKernel32 = syscall.NewLazyDLL("kernel32.dll")
	moveFileExProc  = replaceKernel32.NewProc("MoveFileExW")
)

func replaceProxyFile(tmpPath, path string) error {
	from, err := syscall.UTF16PtrFromString(tmpPath)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, callErr := moveFileExProc.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), moveFileReplaceExisting|moveFileWriteThrough)
	if r == 0 {
		return callErr
	}
	return nil
}
