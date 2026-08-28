//go:build windows

package agents

import (
	"syscall"
	"unsafe"
)

const processQueryLimitedInformation = 0x1000

var (
	agentKernel32      = syscall.NewLazyDLL("kernel32.dll")
	openProcess        = agentKernel32.NewProc("OpenProcess")
	getExitCodeProcess = agentKernel32.NewProc("GetExitCodeProcess")
	closeProcessHandle = agentKernel32.NewProc("CloseHandle")
)

func proxyProcessAlive(pid int) bool {
	handle, _, _ := openProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return false
	}
	defer closeProcessHandle.Call(handle)
	var code uint32
	ret, _, _ := getExitCodeProcess.Call(handle, uintptr(unsafe.Pointer(&code)))
	return ret != 0 && code == 259
}
