package tools

import (
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func TestRtkCursorWireNeedsNoInstructionOwner(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("HOME", tmp)
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(tmp, ".cursor"))
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")

	ran, err := rtk.WireFor["cursor"](core.RunOpts{})
	if err != nil || !ran {
		t.Fatalf("wire: ran=%v err=%v", ran, err)
	}
	if !agents.HasCursorRtkHook() || !agents.HasCursorRtkPermissions() {
		t.Fatal("expected Cursor RTK hook and permissions after wire")
	}
	if HasOwner("cursor", "rtk") {
		t.Fatal("Cursor RTK must not require an instruction owner")
	}
	if verify := rtk.VerifyFor["cursor"](); verify == nil || !*verify {
		t.Fatal("verify failed")
	}
}
