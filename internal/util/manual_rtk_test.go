package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManualRtkTarball(t *testing.T) {
	if os.Getenv("TOKLESS_MANUAL_RTK") != "1" {
		t.Skip("manual: TOKLESS_MANUAL_RTK=1")
	}
	dest := t.TempDir()
	url := "https://github.com/rtk-ai/rtk/releases/latest/download/rtk-x86_64-unknown-linux-musl.tar.gz"
	if err := DownloadAndExtractTarGz(url, dest); err != nil {
		t.Fatalf("download/extract: %v", err)
	}
	bin := filepath.Join(dest, "rtk")
	_ = os.Chmod(bin, 0o755)
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("rtk --version: %v", err)
	}
	t.Logf("rtk %s", out)
}
