//go:build darwin

package headroom

import (
	"encoding/binary"
	"testing"
)

func TestDarwinProcArgsParsesArgvAndRejectsTruncatedData(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 3)
	data = append(data, []byte("/tmp/tokless bin\x00\x00--flag\x00value with spaces\x00")...)
	got := darwinProcArgs(data)
	want := []string{"/tmp/tokless bin", "--flag", "value with spaces"}
	if len(got) != len(want) {
		t.Fatalf("argv length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	truncated := append([]byte(nil), data[:len(data)-1]...)
	if got := darwinProcArgs(truncated); got != nil {
		t.Fatalf("truncated argv = %#v, want nil", got)
	}
}
