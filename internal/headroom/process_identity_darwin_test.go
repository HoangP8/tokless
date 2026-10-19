//go:build darwin

package headroom

import (
	"encoding/binary"
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
	if !equalArgv(got, argv) {
		t.Fatalf("argv = %#v, want %#v", got, argv)
	}
	exeSrc := gotExec
	if exeSrc == "" && len(got) > 0 {
		exeSrc = got[0]
	}
	if exeSrc != "" {
		t.Fatalf("exeSrc = %q, want empty (fail-safe)", exeSrc)
	}
}
