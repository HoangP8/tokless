//go:build darwin

package headroom

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
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
	execPath, argv := darwinProcArgs([]byte(data))
	if start == "" || len(argv) == 0 {
		return processIdentityInfo{}, fmt.Errorf("incomplete process identity")
	}
	exeSrc := execPath
	if exeSrc == "" {
		exeSrc = argv[0]
	}
	if exeSrc == "" {
		return processIdentityInfo{}, fmt.Errorf("incomplete process identity")
	}
	executable, err := normalizeIdentityExecutable(exeSrc)
	if err != nil {
		return processIdentityInfo{}, err
	}
	return processIdentityInfo{Executable: executable, Args: argv[1:], Start: start}, nil
}

func darwinProcArgs(data []byte) (string, []string) {
	if len(data) < 4 {
		return "", nil
	}
	argc := int(binary.LittleEndian.Uint32(data[:4]))
	if argc < 1 || argc > 1<<16 || argc > len(data)-4 {
		return "", nil
	}
	rest := data[4:]
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return "", nil
	}
	execPath := string(rest[:i])
	rest = rest[i+1:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	argv := make([]string, 0, argc)
	for len(argv) < argc {
		if len(rest) == 0 {
			return "", nil
		}
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			return "", nil
		}
		argv = append(argv, string(rest[:j]))
		rest = rest[j+1:]
	}
	return execPath, argv
}

func killProcess(proc *os.Process) error { return proc.Kill() }
func processGone(proc *os.Process) bool {
	err := proc.Signal(syscall.Signal(0))
	return err == syscall.ESRCH
}
