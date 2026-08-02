package tools

import (
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func TestInstructionToolsWireIdempotently(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })

	for _, tool := range []*core.ToolManifest{caveman, ponytail} {
		wire := tool.WireFor["claude"]
		if ok, err := wire(core.RunOpts{}); err != nil || !ok {
			t.Fatalf("first wire %s: ok=%v err=%v", tool.ID, ok, err)
		}
		if !HasOwner("claude", tool.ID) {
			t.Fatalf("first wire %s did not write owner", tool.ID)
		}

		if ok, err := wire(core.RunOpts{}); err != nil || !ok {
			t.Fatalf("second wire %s: ok=%v err=%v", tool.ID, ok, err)
		}
		if !HasOwner("claude", tool.ID) {
			t.Fatalf("second wire %s removed owner", tool.ID)
		}
	}
}
