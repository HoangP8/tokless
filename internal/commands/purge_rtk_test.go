package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// rtk exits non-zero when told to uninstall the way we ask, so the binary has
// to go regardless of what the command reports.
func TestPurgeRtkRemovesBinaryEvenWhenItsUninstallFails(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "rtk")
	if err := os.WriteFile(bin, []byte("not a real binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !purgeRtk(bin) {
		t.Fatal("purgeRtk reported failure")
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("rtk binary survived purge: %v", err)
	}
}

func TestPurgeRtkReportsFailureWhenNothingRemoved(t *testing.T) {
	if purgeRtk(filepath.Join(t.TempDir(), "absent")) {
		t.Fatal("removing a missing binary should not count")
	}
}
