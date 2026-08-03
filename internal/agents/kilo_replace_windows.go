//go:build windows

package agents

import (
	"syscall"
	"unsafe"
)

const movefileReplaceExisting = 0x00000001

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	moveFileExProc = kernel32.NewProc("MoveFileExW")
)

func replaceKiloFile(tmpPath, path string) error {
	from, err := syscall.UTF16PtrFromString(tmpPath)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, callErr := moveFileExProc.Call(
		uintptr(unsafe.Pointer(from)),
		uintptr(unsafe.Pointer(to)),
		movefileReplaceExisting,
	)
	if r == 0 {
		return callErr
	}
	return nil
}
