package agents

import (
	"os"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestConfigureOpenCodeMcpAcceptsJSONCComments(t *testing.T) {
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
	if changed, _ := ConfigureOpenCodeMcp("context-mode"); !changed {
		t.Fatal("JSONC config should accept MCP configuration change")
	}
	got, ok := util.ReadFileSafe(path)
	if !ok || !strings.Contains(got, `"context-mode"`) {
		t.Fatalf("context-mode MCP missing:\n%s", got)
	}
}

func TestConfigureOpenCodeProxyRefusesUnreadableConfig(t *testing.T) {
	opencodeProxyTestHome(t)
	path := util.OpenCodePathsResolved().Config
	if err := util.EnsureDir(path); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeProxy(); changed {
		t.Fatal("unreadable OpenCode config must not be replaced")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("OpenCode config path changed: %v", err)
	}
}
