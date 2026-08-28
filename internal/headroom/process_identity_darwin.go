//go:build darwin

package headroom

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type processIdentityInfo struct {
	Executable string
	Args       []string
	Start      string
}

func processIdentitySupported() bool { return true }

func identifyProcess(pid int) (processIdentityInfo, error) {
	startOutput, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return processIdentityInfo{}, err
	}
	data, err := syscall.Sysctl("kern.procargs2." + strconv.Itoa(pid))
	if err != nil {
		return processIdentityInfo{}, err
	}
	start := strings.TrimSpace(string(startOutput))
	argv := darwinProcArgs([]byte(data))
	if start == "" || len(argv) < 2 {
		return processIdentityInfo{}, fmt.Errorf("incomplete process identity")
	}
	executable, err := filepath.EvalSymlinks(argv[0])
	if err != nil {
		return processIdentityInfo{}, err
	}
	return processIdentityInfo{Executable: executable, Args: argv[1:], Start: start}, nil
}

func darwinProcArgs(data []byte) []string {
	if len(data) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(data[:4]))
	if argc < 1 || argc > 1<<16 || argc > len(data)-4 {
		return nil
	}
	data = data[4:]
	read := func() string {
		if len(data) == 0 {
			return ""
		}
		i := strings.IndexByte(string(data), 0)
		if i < 0 {
			i = len(data)
		}
		v := string(data[:i])
		if i < len(data) {
			data = data[i+1:]
		} else {
			data = nil
		}
		return v
	}
	args := make([]string, 0, argc)
	if executable := read(); executable != "" {
		args = append(args, executable)
	}
	for len(args) < argc {
		if len(data) == 0 {
			return nil
		}
		if data[0] == 0 {
			data = data[1:]
			continue
		}
		args = append(args, read())
	}
	return args
}

func killProcess(proc *os.Process) error { return proc.Kill() }
func processGone(proc *os.Process) bool {
	err := proc.Signal(syscall.Signal(0))
	return err == syscall.ESRCH
}
