package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManualNodeTarballInstall(t *testing.T) {
	if os.Getenv("TOKLESS_MANUAL_NODE") != "1" {
		t.Skip("manual: TOKLESS_MANUAL_NODE=1")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PATH", "/usr/bin:/bin")
	if !installNodeUnixTarball() {
		t.Fatal("installNodeUnixTarball returned false")
	}
	for _, b := range []string{"node", "npm", "npx"} {
		p := filepath.Join(tmp, ".local", "bin", b)
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("missing symlink %s: %v", b, err)
		}
	}
	out, err := exec.Command(filepath.Join(tmp, ".local", "bin", "node"), "--version").Output()
	if err != nil {
		t.Fatalf("node --version: %v", err)
	}
	t.Logf("node %s", out)
	out2, err := exec.Command(filepath.Join(tmp, ".local", "bin", "npm"), "--version").Output()
	if err != nil {
		t.Fatalf("npm --version: %v", err)
	}
	t.Logf("npm %s", out2)
	rc, _ := os.ReadFile(filepath.Join(tmp, ".npmrc"))
	t.Logf("npmrc: %s", rc)
}
