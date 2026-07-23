package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// pluginStrings returns the plugin[] entries of cfg as []string.
func pluginStrings(t *testing.T, cfg *util.OrderedMap) []string {
	t.Helper()
	var out []string
	for _, p := range getArr(cfg, "plugin") {
		if s, ok := p.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func countContextMode(entries []string) int {
	n := 0
	for _, e := range entries {
		if pluginIsContextMode(e) {
			n++
		}
	}
	return n
}

func mcpKeys(cfg *util.OrderedMap) []string {
	mv, ok := cfg.Get("mcp")
	if !ok {
		return nil
	}
	mm, ok := mv.(*util.OrderedMap)
	if !ok {
		return nil
	}
	return mm.Keys()
}

func TestSetContextModePluginBare_AppendsBareWhenMissing(t *testing.T) {
	cfg := util.TryParseJsonc(`{"plugin":["other@1.0.0"]}`)
	removeContextModePlugin(cfg)
	got := pluginStrings(t, cfg)
	want := []string{"other@1.0.0"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q (order must be preserved)", i, got[i], want[i])
		}
	}
}

func TestSetContextModePluginBare_StripsStalePinToBare(t *testing.T) {
	cfg := util.TryParseJsonc(`{"plugin":["other@1.0.0","context-mode@1.0.157"]}`)
	removeContextModePlugin(cfg)
	got := pluginStrings(t, cfg)
	want := []string{"other@1.0.0"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestSetContextModePluginBare_DropsMcpContextMode(t *testing.T) {
	cfg := util.TryParseJsonc(`{
		"plugin":["other@1.0.0","context-mode@1.0.157"],
		"mcp":{"context-mode":{"type":"local"},"codegraph":{"type":"local"}}
	}`)
	removeContextModePlugin(cfg)

	got := pluginStrings(t, cfg)
	want := []string{"other@1.0.0"}
	if len(got) != len(want) {
		t.Fatalf("plugin mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("plugin[%d]=%q want %q", i, got[i], want[i])
		}
	}
	keys := mcpKeys(cfg)
	if len(keys) != 2 || keys[0] != "context-mode" || keys[1] != "codegraph" {
		t.Fatalf("mcp entries changed: %v", keys)
	}
}

func TestSetContextModePluginBare_RemovesMcpKeyEntirelyWhenOnlyEntry(t *testing.T) {
	cfg := util.TryParseJsonc(`{"plugin":[],"mcp":{"context-mode":{"type":"local"}}}`)
	removeContextModePlugin(cfg)
	if _, ok := cfg.Get("mcp"); !ok {
		t.Fatal("context-mode MCP entry should remain")
	}
}

func TestSetContextModePluginBare_Idempotent(t *testing.T) {
	cfg := util.TryParseJsonc(`{"plugin":["a@1","context-mode","b@2"]}`)
	removeContextModePlugin(cfg)
	first := pluginStrings(t, cfg)
	removeContextModePlugin(cfg)
	second := pluginStrings(t, cfg)

	if countContextMode(second) != 0 {
		t.Fatalf("context-mode plugin remains: %v", second)
	}
	if len(first) != len(second) {
		t.Fatalf("non-idempotent: %v then %v", first, second)
	}
	if len(second) != 2 || second[0] != "a@1" || second[1] != "b@2" {
		t.Fatalf("unexpected ordering after idempotent re-apply: %v", second)
	}
}

func TestSetContextModePluginBare_NeverVersionPins(t *testing.T) {
	cfg := util.NewOrderedMap()
	removeContextModePlugin(cfg)
	got := pluginStrings(t, cfg)
	if len(got) != 0 {
		t.Fatalf("unexpected plugins: %v", got)
	}
}

func TestCleanAllContextModeCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	util.SetHomeOverride(home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	defer util.SetHomeOverride("")

	cache := filepath.Join(home, ".cache", "opencode", "packages")
	dirs := []string{
		"context-mode@latest",
		"context-mode@1.0.146",
		"context-mode@1.0.162",
		"context-mode",
		"oh-my-opencode@1.1.1",
		"context-mode-helper@1",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(cache, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cleanAllContextModeCache()

	gone := []string{"context-mode@latest", "context-mode@1.0.146", "context-mode@1.0.162", "context-mode"}
	for _, d := range gone {
		if _, err := os.Stat(filepath.Join(cache, d)); err == nil {
			t.Fatalf("%s should have been cleaned", d)
		}
	}
	survive := []string{"oh-my-opencode@1.1.1", "context-mode-helper@1"}
	for _, d := range survive {
		if _, err := os.Stat(filepath.Join(cache, d)); err != nil {
			t.Fatalf("%s must survive (only context-mode itself is cleaned)", d)
		}
	}
}
