package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestOpenCodeMajorVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    int
	}{{"1.18.35", 1}, {"opencode v2.0.6\n", 2}, {"unknown", 0}} {
		bin := filepath.Join(t.TempDir(), "opencode")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s' '"+tc.version+"'\n"), 0755); err != nil {
			t.Fatal(err)
		}
		if got := OpenCodeMajorVersion(bin); got != tc.want {
			t.Errorf("OpenCodeMajorVersion(%q) = %d, want %d", tc.version, got, tc.want)
		}
	}
	if got := OpenCodeMajorVersion(filepath.Join(t.TempDir(), "missing")); got != 0 {
		t.Fatalf("missing binary version = %d", got)
	}
}

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

func TestOpenCodeTransportPluginPathUsesWindowsLayout(t *testing.T) {
	oldWin := util.IsWin
	util.IsWin = true
	t.Cleanup(func() { util.IsWin = oldWin })

	want := filepath.Join(util.HeadroomPathsResolved().Tools, "headroom-ai", "Lib", "site-packages", "headroom", "providers", "opencode", "_dist", "entry.opencode.js")
	if got := openCodeTransportPluginPath(); got != want {
		t.Fatalf("Windows OpenCode plugin path = %q, want %q", got, want)
	}
}

func openCodeTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func openCodeCommandJSON(t *testing.T, toolID string) string {
	t.Helper()
	b, err := json.Marshal(openCodeMcpCommand(toolID))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOpenCodeMcpDualShapeRead(t *testing.T) {
	openCodeTestHome(t)
	command := openCodeCommandJSON(t, "context-mode")

	v2 := `{"mcp":{"servers":{"context-mode":{"type":"local","command":` + command + `,"disabled":false}}}}`
	cfg := util.TryParseJsonc(v2)
	if cfg == nil {
		t.Fatal("failed to parse v2 config")
	}
	if !OpenCodeMcpHas(cfg, "context-mode") {
		t.Fatal("v2 shape must be detected by OpenCodeMcpHas")
	}
	if !OpenCodeMcpMatches(cfg, "context-mode", openCodeMcpCommand("context-mode")) {
		t.Fatal("v2 shape must match the desired command")
	}

	v1 := `{"mcp":{"context-mode":{"type":"local","command":` + command + `,"enabled":true}}}`
	cfg = util.TryParseJsonc(v1)
	if cfg == nil {
		t.Fatal("failed to parse v1 config")
	}
	if !OpenCodeMcpHas(cfg, "context-mode") {
		t.Fatal("v1 shape must be detected by OpenCodeMcpHas")
	}
	if !OpenCodeMcpMatches(cfg, "context-mode", openCodeMcpCommand("context-mode")) {
		t.Fatal("v1 shape must match the desired command")
	}
}

func TestOpenCodeMcpDualShapeDisabledNotWired(t *testing.T) {
	openCodeTestHome(t)
	command := openCodeCommandJSON(t, "context-mode")

	v1Disabled := `{"mcp":{"context-mode":{"type":"local","command":` + command + `,"enabled":false}}}`
	if OpenCodeMcpHas(util.TryParseJsonc(v1Disabled), "context-mode") {
		t.Fatal("v1 entry with enabled:false must not report as wired")
	}

	v2Disabled := `{"mcp":{"servers":{"context-mode":{"type":"local","command":` + command + `,"disabled":true}}}}`
	if OpenCodeMcpHas(util.TryParseJsonc(v2Disabled), "context-mode") {
		t.Fatal("v2 entry with disabled:true must not report as wired")
	}
}

func TestConfigureOpenCodeMcpWritesV1WhenOnlyV2Present(t *testing.T) {
	openCodeTestHome(t)
	command := openCodeCommandJSON(t, "projectmem")
	body := `{"mcp":{"servers":{"projectmem":{"type":"local","command":` + command + `,"disabled":false}}}}`
	if err := util.WriteFile(util.OpenCodePathsResolved().Config, body); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureOpenCodeMcp("projectmem")
	if !changed {
		t.Fatal("v1 entry is missing — must be written even when v2 is correct")
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("config unparsable: %s", raw)
	}
	mcp, _ := mapChild(cfg, "mcp")
	if entry, has := mcp.Get("projectmem"); !has {
		t.Fatalf("canonical v1 entry not written: %s", raw)
	} else if em, ok := entry.(*util.OrderedMap); !ok || !openCodeMcpEntryEnabled(em) || !openCodeMcpEntryCommandMatches(em, openCodeMcpCommand("projectmem")) {
		t.Fatalf("v1 entry not healed to canonical form: %s", raw)
	}
}

func TestConfigureOpenCodeMcpWritesV2WhenServersMapExists(t *testing.T) {
	openCodeTestHome(t)
	body := `{"mcp":{"servers":{}},"theme":"dark"}`
	if err := util.WriteFile(util.OpenCodePathsResolved().Config, body); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureOpenCodeMcp("projectmem")
	if !changed {
		t.Fatal("missing v1 entry must be written")
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("config unparsable after write: %s", raw)
	}
	mcp, _ := mapChild(cfg, "mcp")
	if _, has := mcp.Get("projectmem"); !has {
		t.Fatalf("canonical v1 entry not written: %s", raw)
	}
	servers, ok := mapChild(mcp, "servers")
	if !ok {
		t.Fatalf("v2 servers map lost: %s", raw)
	}
	if _, has := servers.Get("projectmem"); !has {
		t.Fatalf("v2 servers entry not updated: %s", raw)
	}
	if !OpenCodeMcpMatches(cfg, "projectmem", openCodeMcpCommand("projectmem")) {
		t.Fatal("written entry does not match desired command")
	}
}

func TestRemoveOpenCodeMcpRemovesBothShapes(t *testing.T) {
	openCodeTestHome(t)
	command := openCodeCommandJSON(t, "codegraph")
	body := `{"mcp":{"codegraph":{"type":"local","command":` + command + `,"enabled":true},"servers":{"codegraph":{"type":"local","command":` + command + `,"disabled":false}}}}`
	if err := util.WriteFile(util.OpenCodePathsResolved().Config, body); err != nil {
		t.Fatal(err)
	}
	if !RemoveOpenCodeMcp("codegraph") {
		t.Fatal("dual-shape remove must report removal")
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if strings.Contains(raw, "codegraph") {
		t.Fatalf("codegraph entry survived removal: %s", raw)
	}
}

func TestRemoveOpenCodeMcpLeavesForeignEntry(t *testing.T) {
	openCodeTestHome(t)
	body := `{"mcp":{"projectmem":{"type":"local","command":["/usr/bin/foreign-mcp","--stdio"],"enabled":true}}}`
	if err := util.WriteFile(util.OpenCodePathsResolved().Config, body); err != nil {
		t.Fatal(err)
	}
	if RemoveOpenCodeMcp("projectmem") {
		t.Fatal("foreign entry with a different command must not be removed")
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	if !strings.Contains(raw, "foreign-mcp") {
		t.Fatalf("foreign entry was disturbed: %s", raw)
	}
}

func TestConfigureOpenCodeMcpHealsOutdatedV2Entry(t *testing.T) {
	openCodeTestHome(t)
	body := `{"mcp":{"servers":{"context-mode":{"type":"local","command":["context-mode","serve"],"disabled":false}}}}`
	if err := util.WriteFile(util.OpenCodePathsResolved().Config, body); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureOpenCodeMcp("context-mode")
	if !changed {
		t.Fatal("outdated v2 entry must be healed and rewritten")
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		t.Fatalf("config unparsable after heal: %s", raw)
	}
	mcp, _ := mapChild(cfg, "mcp")
	servers, ok := mapChild(mcp, "servers")
	if !ok {
		t.Fatalf("v2 servers map lost after heal: %s", raw)
	}
	entry, has := servers.Get("context-mode")
	if !has {
		t.Fatalf("context-mode entry missing after heal: %s", raw)
	}
	em, ok := entry.(*util.OrderedMap)
	if !ok {
		t.Fatalf("context-mode entry is not an object: %s", raw)
	}
	expected := openCodeMcpCommand("context-mode")
	if !openCodeMcpEntryCommandMatches(em, expected) {
		t.Fatalf("v2 entry not healed to canonical command: %s", raw)
	}
	if !openCodeMcpEntryEnabled(em) {
		t.Fatal("healed v2 entry must be enabled (disabled != true)")
	}
}

func TestOpenCodeInstalledMajorWithoutShim(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", home+"/empty")
	t.Cleanup(func() { util.SetHomeOverride("") })

	binDir := filepath.Join(home, ".opencode", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '2.0.6'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if got := OpenCodeInstalledMajor(); got != 2 {
		t.Fatalf("OpenCodeInstalledMajor() = %d, want 2 (plain opencode, no opencode2 shim)", got)
	}
}

func TestOpenCodeInstalledMajorNone(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", home+"/empty")
	t.Cleanup(func() { util.SetHomeOverride("") })

	if got := OpenCodeInstalledMajor(); got != 0 {
		t.Fatalf("OpenCodeInstalledMajor() = %d, want 0 when no binary", got)
	}
}
