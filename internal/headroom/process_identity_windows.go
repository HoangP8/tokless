//go:build windows

package headroom

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

type processIdentityInfo struct {
	Executable string
	Args       []string
	Start      string
}

type windowsProcess struct {
	Executable string `json:"ExecutablePath"`
	Command    string `json:"CommandLine"`
	Created    string `json:"CreationDate"`
}

func processIdentitySupported() bool { return true }

func identifyProcess(pid int) (processIdentityInfo, error) {
	filter := fmt.Sprintf("ProcessId = %d", pid)
	script := "$p = Get-CimInstance Win32_Process -Filter '" + filter + "'; if ($null -eq $p) { exit 1 }; $p | Select-Object ExecutablePath,CommandLine,CreationDate | ConvertTo-Json -Compress"
	var out []byte
	var err error
	for _, shell := range []string{"powershell", "pwsh"} {
		if _, lookErr := exec.LookPath(shell); lookErr != nil {
			continue
		}
		out, err = exec.Command(shell, "-NoProfile", "-Command", script).Output()
		if err == nil {
			break
		}
	}
	if err != nil {
		return processIdentityInfo{}, err
	}
	var p windowsProcess
	if err := json.Unmarshal(out, &p); err != nil || p.Executable == "" || p.Command == "" || p.Created == "" {
		return processIdentityInfo{}, fmt.Errorf("incomplete process identity")
	}
	executable, err := filepath.Abs(p.Executable)
	if err != nil {
		return processIdentityInfo{}, err
	}
	args := windowsCommandLineArgs(p.Command)
	if len(args) < 2 {
		return processIdentityInfo{}, fmt.Errorf("incomplete process identity")
	}
	return processIdentityInfo{Executable: executable, Args: args[1:], Start: p.Created}, nil
}

func windowsCommandLineArgs(command string) []string {
	ptr, err := syscall.UTF16PtrFromString(command)
	if err != nil {
		return nil
	}
	var argc int32
	argv, _, _ := commandLineToArgvW.Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&argc)))
	if argv == 0 || argc <= 0 {
		return nil
	}
	defer localFree.Call(argv)
	if uintptr(argc) > ^uintptr(0)/unsafe.Sizeof(uintptr(0)) {
		return nil
	}
	pointers := unsafe.Slice((**uint16)(unsafe.Pointer(argv)), int(argc))
	args := make([]string, len(pointers))
	for i, p := range pointers {
		if p == nil {
			return nil
		}
		args[i] = syscall.UTF16ToString(unsafe.Slice(p, 32768))
	}
	return args
}

var (
	shell32            = syscall.NewLazyDLL("shell32.dll")
	commandLineToArgvW = shell32.NewProc("CommandLineToArgvW")
	localFree          = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

func killProcess(proc *os.Process) error { return proc.Kill() }
func processGone(proc *os.Process) bool {
	handle, _, callErr := processOpen.Call(processQueryLimitedInformation, 0, uintptr(proc.Pid))
	if handle == 0 {
		return callErr == syscall.Errno(87)
	}
	defer processClose.Call(handle)
	var code uint32
	ret, _, _ := processExitCode.Call(handle, uintptr(unsafe.Pointer(&code)))
	return ret != 0 && code != 259
}

const processQueryLimitedInformation = 0x1000

var (
	processKernel32 = syscall.NewLazyDLL("kernel32.dll")
	processOpen     = processKernel32.NewProc("OpenProcess")
	processExitCode = processKernel32.NewProc("GetExitCodeProcess")
	processClose    = processKernel32.NewProc("CloseHandle")
)
