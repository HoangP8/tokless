package tools

import (
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestCodegraphConfigureMcp(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_DIR", tempDir)

	codegraphConfigureMcp("opencode")

	if !codegraphVerify("opencode") {
		t.Errorf("codegraphVerify() returned false after configuration")
	}

	op := util.OpenCodePathsResolved()
	if !util.Exists(op.Config) {
		t.Errorf("config file was not created at %q", op.Config)
	}
}
