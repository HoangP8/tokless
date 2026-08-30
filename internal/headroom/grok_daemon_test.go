package headroom

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestGrokOwnershipValidCases(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	pidFile, _ := grokProxyFiles()
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
		t.Fatal(err)
	}

	if grokOwnershipValid() {
		t.Fatal("missing record must be invalid")
	}
	if err := os.WriteFile(pidFile, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if grokOwnershipValid() {
		t.Fatal("garbage record must be invalid")
	}
	if err := os.WriteFile(pidFile, []byte(`{"pid":999999,"executable":"/nonexistent","args":["proxy"],"start_fingerprint":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if grokOwnershipValid() {
		t.Fatal("dead-pid record must be invalid")
	}
}
