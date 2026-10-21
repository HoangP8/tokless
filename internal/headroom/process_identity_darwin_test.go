//go:build darwin

package headroom

import (
	"encoding/binary"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func buildProcArgs2(execPath string, pad int, argv []string, env []string) []byte {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(len(argv)))
	data = append(data, []byte(execPath)...)
	data = append(data, 0)
	for i := 0; i < pad; i++ {
		data = append(data, 0)
	}
	for _, a := range argv {
		data = append(data, []byte(a)...)
		data = append(data, 0)
	}
	for _, e := range env {
		data = append(data, []byte(e)...)
		data = append(data, 0)
	}
	return data
}

func equalArgv(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestDarwinProcArgsParsesArgvAndRejectsTruncatedData(t *testing.T) {
	execPath := "/tmp/tokless bin"
	argv := []string{"/tmp/tokless bin", "--flag", "value with spaces"}
	data := buildProcArgs2(execPath, 1, argv, nil)
	gotExec, got := darwinProcArgs(data)
	if gotExec != execPath {
		t.Fatalf("exec_path = %q, want %q", gotExec, execPath)
	}
	if !equalArgv(got, argv) {
		t.Fatalf("argv = %#v, want %#v", got, argv)
	}
	// Cut right after first argv entry so buffer runs out mid-argv.
	truncated := append([]byte(nil), data[:4+len(execPath)+1+1+len(argv[0])+1]...)
	if _, got := darwinProcArgs(truncated); got != nil {
		t.Fatalf("truncated argv = %#v, want nil", got)
	}
}

func TestDarwinProcArgsPreservesEmptyEntry(t *testing.T) {
	execPath := "/usr/bin/prog"
	argv := []string{"/usr/bin/prog", "", "--flag"}
	data := buildProcArgs2(execPath, 2, argv, nil)
	gotExec, got := darwinProcArgs(data)
	if gotExec != execPath {
		t.Fatalf("exec_path = %q, want %q", gotExec, execPath)
	}
	if !equalArgv(got, argv) {
		t.Fatalf("argv = %#v, want %#v", got, argv)
	}
}

func TestDarwinProcArgsIgnoresTrailingEnv(t *testing.T) {
	execPath := "/usr/bin/prog"
	argv := []string{"/usr/bin/prog", "-x"}
	env := []string{"PATH=/usr/bin:/bin", "HOME=/root"}
	data := buildProcArgs2(execPath, 0, argv, env)
	gotExec, got := darwinProcArgs(data)
	if gotExec != execPath {
		t.Fatalf("exec_path = %q, want %q", gotExec, execPath)
	}
	if !equalArgv(got, argv) {
		t.Fatalf("argv = %#v, want %#v", got, argv)
	}
}

func TestDarwinProcArgsRejectsZeroArgc(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 0)
	data = append(data, 0)
	if execPath, argv := darwinProcArgs(data); execPath != "" || argv != nil {
		t.Fatalf("argc=0 = (%q, %#v), want (\"\", nil)", execPath, argv)
	}
}

func TestDarwinProcArgsEmptyExecPathFailsSafe(t *testing.T) {
	argv := []string{"", "-x"}
	data := buildProcArgs2("", 0, argv, nil)
	gotExec, got := darwinProcArgs(data)
	if gotExec != "" {
		t.Fatalf("exec_path = %q, want empty", gotExec)
	}
	if got != nil {
		t.Fatalf("argv = %#v, want nil", got)
	}
}

func TestDarwinProcArgs2ReadsOwnProcess(t *testing.T) {
	data, err := darwinProcArgs2(os.Getpid())
	if err != nil {
		t.Fatalf("darwinProcArgs2() error = %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("darwinProcArgs2() returned %d bytes, want at least 4", len(data))
	}
	execPath, argv := darwinProcArgs(data)
	if execPath == "" {
		t.Fatal("darwinProcArgs() exec_path empty")
	}
	if len(argv) == 0 {
		t.Fatal("darwinProcArgs() argv empty")
	}
	if !strings.HasSuffix(argv[0], ".test") {
		t.Fatalf("argv[0] = %q, want the test binary path", argv[0])
	}
}

func TestDarwinProcessGoneDetectsReapedProcess(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !processGone(cmd.Process) {
		t.Fatal("processGone() = false, want true for a reaped process")
	}
}

func TestDarwinIdentifyProcessReadsOwnProcess(t *testing.T) {
	identity, err := identifyProcess(os.Getpid())
	if err != nil {
		t.Fatalf("identifyProcess() error = %v", err)
	}
	want, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want, err = normalizeIdentityExecutable(want)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Executable != want {
		t.Fatalf("Executable = %q, want %q", identity.Executable, want)
	}
	if !equalArgv(identity.Args, os.Args[1:]) {
		t.Fatalf("Args = %#v, want %#v", identity.Args, os.Args[1:])
	}
	if identity.Start == "" {
		t.Fatal("Start empty")
	}
}
