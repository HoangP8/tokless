package tools

import (
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// opencode2TestHome isolates HOME and the OpenCode config dirs so the lanes can be steered independently.
func opencode2TestHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(tmp, ".config", "opencode"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })
	return tmp
}

// openCodeSeparateV2PluginsDir points the v2 lane at its own config dir, so the v1 and v2 plugins dirs differ.
func openCodeSeparateV2PluginsDir(t *testing.T, tmp string) {
	t.Helper()
	t.Setenv("OPENCODE_CONFIG_DIR_V2", filepath.Join(tmp, ".config", "opencode2"))
	if !openCodeV2PluginsDirIsSeparate() {
		t.Fatal("v2 plugins dir must differ from the v1 one")
	}
}
