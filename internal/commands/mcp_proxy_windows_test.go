//go:build windows

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdBatchRelayWithForwardSlashPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "relay fixture")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "relay fixture.cmd")
	if err := os.WriteFile(fixture, []byte("@echo relay-ok\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	comspec := os.Getenv("COMSPEC")
	if comspec == "" {
		comspec = `C:\Windows\System32\cmd.exe`
	}
	_, args := resolveMcpCommand(comspec, []string{comspec, "/c", filepath.ToSlash(fixture)})
	out, err := exec.Command(comspec, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("cmd relay failed: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "relay-ok") {
		t.Fatalf("unexpected output: %s", out)
	}
}
