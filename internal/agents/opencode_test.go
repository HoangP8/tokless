package agents

import (
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestConfigureOpenCodeMcpRefusesJSONCComments(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Cleanup(func() { util.SetHomeOverride("") })

	path := util.OpenCodePathsResolved().Config
	seed := `{
  // Keep this user comment.
  "theme": "dark"
}
`
	if err := util.WriteFile(path, seed); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeMcp("context-mode"); changed {
		t.Fatal("must refuse MCP configuration change")
	}
	got, ok := util.ReadFileSafe(path)
	if !ok {
		t.Fatal("OpenCode config missing")
	}
	if got != seed {
		t.Fatalf("JSONC config changed:\n%s", got)
	}
}
