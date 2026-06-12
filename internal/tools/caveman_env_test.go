package tools

import (
	"path/filepath"
	"testing"
)

func TestCavemanOpencodeInstallEnv(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("valid opencode dir", func(t *testing.T) {
		opencodeDir := filepath.Join(tempDir, "opencode")
		t.Setenv("OPENCODE_CONFIG_DIR", opencodeDir)
		
		got := cavemanOpencodeInstallEnv()
		if len(got) != 1 || got[0] != "XDG_CONFIG_HOME="+tempDir {
			t.Errorf("got %v, want [XDG_CONFIG_HOME=%s]", got, tempDir)
		}
	})

	t.Run("weird name", func(t *testing.T) {
		weirdDir := filepath.Join(tempDir, "weird-name")
		t.Setenv("OPENCODE_CONFIG_DIR", weirdDir)
		
		got := cavemanOpencodeInstallEnv()
		if len(got) != 0 {
			t.Errorf("got %v, want nil", got)
		}
	})
}
