//go:build linux

package headroom

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type processIdentityInfo struct {
	Executable string
	Args       []string
	Start      string
}

func processIdentitySupported() bool { return true }

func identifyProcess(pid int) (processIdentityInfo, error) {
	base := filepath.Join("/proc", fmt.Sprint(pid))
	executable, err := os.Readlink(filepath.Join(base, "exe"))
	if err != nil {
		return processIdentityInfo{}, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return processIdentityInfo{}, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return processIdentityInfo{}, err
	}
	data, err := os.ReadFile(filepath.Join(base, "cmdline"))
	if err != nil {
		return processIdentityInfo{}, err
	}
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	if len(parts) < 2 || parts[0] == "" {
		return processIdentityInfo{}, fmt.Errorf("incomplete process identity")
	}
	stat, err := os.ReadFile(filepath.Join(base, "stat"))
	if err != nil {
		return processIdentityInfo{}, err
	}
	close := strings.LastIndex(string(stat), ")")
	if close < 0 {
		return processIdentityInfo{}, fmt.Errorf("incomplete process start identity")
	}
	fields := strings.Fields(string(stat)[close+2:])
	if len(fields) <= 19 {
		return processIdentityInfo{}, fmt.Errorf("incomplete process start identity")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return processIdentityInfo{}, err
	}
	return processIdentityInfo{Executable: executable, Args: parts[1:], Start: fields[19]}, nil
}

func killProcess(proc *os.Process) error { return proc.Kill() }
func processGone(proc *os.Process) bool {
	_, err := identifyProcess(proc.Pid)
	return os.IsNotExist(err)
}
