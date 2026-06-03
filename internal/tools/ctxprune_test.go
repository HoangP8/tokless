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

func TestSetContextModePlugin_ReplacesStalePin(t *testing.T) {
	cfg := util.TryParseJsonc(`{"plugin":["context-mode@1.0.162"]}`)
	setContextModePlugin(cfg)
	got := pluginStrings(t, cfg)
	if len(got) != 1 || got[0] != "context-mode" {
		t.Fatalf("expected bare [context-mode], got %v", got)
	}
}

func TestSetContextModePlugin_KeepsOrderAndDropsMcp(t *testing.T) {
	cfg := util.TryParseJsonc(`{
		"plugin":["other@1.0.0","context-mode@1.0.157"],
		"mcp":{"context-mode":{"type":"local"},"codegraph":{"type":"local"}}
	}`)
	setContextModePlugin(cfg)

	got := pluginStrings(t, cfg)
	want := []string{"other@1.0.0", "context-mode"}
	if len(got) != len(want) {
		t.Fatalf("plugin mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("plugin[%d]=%q want %q (order must be preserved)", i, got[i], want[i])
		}
	}
	keys := mcpKeys(cfg)
	if len(keys) != 1 || keys[0] != "codegraph" {
		t.Fatalf("mcp must keep only codegraph, got %v (zero-tools trap if context-mode remains)", keys)
	}
}

func TestSetContextModePlugin_AppendsWhenMissing(t *testing.T) {
	cfg := util.TryParseJsonc(`{"plugin":["other@1.0.0"]}`)
	setContextModePlugin(cfg)
	got := pluginStrings(t, cfg)
	if len(got) != 2 || got[1] != "context-mode" {
		t.Fatalf("expected bare context-mode appended after other, got %v", got)
	}
}

func TestSetContextModePlugin_EmptyConfig(t *testing.T) {
	cfg := util.NewOrderedMap()
	setContextModePlugin(cfg)
	got := pluginStrings(t, cfg)
	if len(got) != 1 || got[0] != "context-mode" {
		t.Fatalf("expected [context-mode] on empty config, got %v", got)
	}
}

func TestSetContextModePlugin_RemovesMcpKeyEntirelyWhenOnlyEntry(t *testing.T) {
	cfg := util.TryParseJsonc(`{"plugin":[],"mcp":{"context-mode":{"type":"local"}}}`)
	setContextModePlugin(cfg)
	if _, ok := cfg.Get("mcp"); ok {
		t.Fatalf("mcp key should be removed entirely when context-mode was its only entry")
	}
}

func TestSetContextModePlugin_Idempotent(t *testing.T) {
	cfg := util.TryParseJsonc(`{"plugin":["a@1","context-mode","b@2"]}`)
	setContextModePlugin(cfg)
	first := pluginStrings(t, cfg)
	setContextModePlugin(cfg)
	second := pluginStrings(t, cfg)

	if countContextMode(second) != 1 {
		t.Fatalf("idempotency broken: %d context-mode entries: %v", countContextMode(second), second)
	}
	if len(first) != len(second) {
		t.Fatalf("non-idempotent: %v then %v", first, second)
	}
	if second[0] != "a@1" || second[len(second)-1] != "context-mode" {
		t.Fatalf("unexpected ordering after idempotent re-apply: %v", second)
	}
}

func TestCleanAllContextModeCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	util.SetHomeOverride(home)
	defer util.SetHomeOverride("")

	cache := filepath.Join(home, ".cache", "opencode", "packages")
	dirs := []string{
		"context-mode@latest",   // empty culprit
		"context-mode@1.0.146",  // old populated
		"context-mode@1.0.162",  // dangling/unpublished pin
		"context-mode",          // bare
		"oh-my-opencode@1.1.1",  // unrelated — must survive
		"context-mode-helper@1", // different package, must survive (no @ boundary)
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
