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
	skipOnWindows(t)
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
	t.Setenv("TOKLESS_TEST", "1")
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func openCodeCommandJSON(t *testing.T, agentID, toolID string) string {
	t.Helper()
	b, err := json.Marshal(openCodeMcpCommand(agentID, toolID))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOpenCodeMcpDualShapeRead(t *testing.T) {
	openCodeTestHome(t)
	command := openCodeCommandJSON(t, "opencode", "context-mode")

	v2 := `{"mcp":{"servers":{"context-mode":{"type":"local","command":` + command + `,"disabled":false}}}}`
	cfg := util.TryParseJsonc(v2)
	if cfg == nil {
		t.Fatal("failed to parse v2 config")
	}
	if !OpenCodeMcpHas(cfg, "context-mode") {
		t.Fatal("v2 shape must be detected by OpenCodeMcpHas")
	}
	if !OpenCodeMcpMatches(cfg, "context-mode", openCodeMcpCommand("opencode2", "context-mode")) {
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
	if !OpenCodeMcpMatches(cfg, "context-mode", openCodeMcpCommand("opencode", "context-mode")) {
		t.Fatal("v1 shape must match the desired command")
	}
}

func TestOpenCodeMcpDualShapeDisabledNotWired(t *testing.T) {
	openCodeTestHome(t)
	command := openCodeCommandJSON(t, "opencode", "context-mode")

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
	command := openCodeCommandJSON(t, "opencode", "projectmem")
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
	} else if em, ok := entry.(*util.OrderedMap); !ok || !openCodeMcpEntryEnabled(em) || !openCodeMcpEntryCommandMatches(em, openCodeMcpCommand("opencode", "projectmem")) {
		t.Fatalf("v1 entry not healed to canonical form: %s", raw)
	}
}

func TestConfigureOpenCodeMcpV1WritesRootShape(t *testing.T) {
	openCodeTestHome(t)
	path := util.OpenCodePathsResolved().Config
	if err := util.WriteFile(path, `{"mcp":{"servers":{}},"theme":"dark"}`); err != nil {
		t.Fatal(err)
	}
	if changed, _ := ConfigureOpenCodeMcpV1("projectmem"); !changed {
		t.Fatal("missing v1 entry must be written")
	}
	raw, _ := util.ReadFileSafe(path)
	cfg := util.TryParseJsonc(raw)
	mcp, _ := mapChild(cfg, "mcp")
	entry, has := mcp.Get("projectmem")
	if !has {
		t.Fatalf("v1 entry missing: %s", raw)
	}
	em, ok := entry.(*util.OrderedMap)
	if !ok || !openCodeMcpEntryEnabled(em) || !openCodeMcpEntryCommandMatches(em, openCodeMcpCommand("opencode", "projectmem")) {
		t.Fatalf("v1 entry invalid: %s", raw)
	}
	if _, has := mapChild(mcp, "servers"); !has {
		t.Fatalf("existing v2 shape lost: %s", raw)
	}
	if changed, _ := ConfigureOpenCodeMcpV1("projectmem"); changed {
		t.Fatal("second configure must be idempotent")
	}
}

func TestConfigureOpenCodeMcpV2WritesServersShape(t *testing.T) {
	openCodeTestHome(t)
	v1Path := util.OpenCodePathsResolved().Config
	v2Path := util.OpenCodePathsV2().Config
	if err := util.WriteFile(v1Path, `{"theme":"dark"}`); err != nil {
		t.Fatal(err)
	}
	before, _ := util.ReadFileSafe(v1Path)
	if changed, _ := ConfigureOpenCodeMcpV2("projectmem"); !changed {
		t.Fatal("missing v2 entry must be written")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	cfg := util.TryParseJsonc(raw)
	mcp, _ := mapChild(cfg, "mcp")
	servers, ok := mapChild(mcp, "servers")
	if !ok {
		t.Fatalf("v2 servers map missing: %s", raw)
	}
	entry, has := servers.Get("projectmem")
	em, valid := entry.(*util.OrderedMap)
	if !has || !valid || !openCodeMcpEntryEnabled(em) || !openCodeMcpEntryCommandMatches(em, openCodeMcpCommand("opencode2", "projectmem")) {
		t.Fatalf("v2 entry invalid: %s", raw)
	}
	if v1Path != v2Path {
		after, exists := util.ReadFileSafe(v1Path)
		if !exists || after != before {
			t.Fatalf("v1 config changed: %s", after)
		}
	}
}

func TestRemoveOpenCodeMcpV1RemovesRootOnly(t *testing.T) {
	openCodeTestHome(t)
	command := openCodeCommandJSON(t, "opencode", "codegraph")
	body := `{"mcp":{"codegraph":{"type":"local","command":` + command + `,"enabled":true},"servers":{"codegraph":{"type":"local","command":` + command + `,"disabled":false}}}}`
	if err := util.WriteFile(util.OpenCodePathsResolved().Config, body); err != nil {
		t.Fatal(err)
	}
	if !RemoveOpenCodeMcpV1("codegraph") {
		t.Fatal("v1 remove must report removal")
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsResolved().Config)
	cfg := util.TryParseJsonc(raw)
	mcp, _ := mapChild(cfg, "mcp")
	if _, exists := mcp.Get("codegraph"); exists {
		t.Fatalf("v1 entry survived: %s", raw)
	}
	if _, exists := mapChild(mcp, "servers"); !exists || !strings.Contains(raw, `"codegraph"`) {
		t.Fatalf("v2 entry was removed: %s", raw)
	}
}

func TestRemoveOpenCodeMcpV2RemovesServersOnly(t *testing.T) {
	openCodeTestHome(t)
	command := openCodeCommandJSON(t, "opencode2", "codegraph")
	body := `{"mcp":{"codegraph":{"type":"local","command":` + command + `,"enabled":true},"servers":{"codegraph":{"type":"local","command":` + command + `,"disabled":false}}}}`
	if err := util.WriteFile(util.OpenCodePathsV2().Config, body); err != nil {
		t.Fatal(err)
	}
	if !RemoveOpenCodeMcpV2("codegraph") {
		t.Fatal("v2 remove must report removal")
	}
	raw, _ := util.ReadFileSafe(util.OpenCodePathsV2().Config)
	cfg := util.TryParseJsonc(raw)
	mcp, _ := mapChild(cfg, "mcp")
	if _, exists := mcp.Get("codegraph"); !exists {
		t.Fatalf("v1 entry was removed: %s", raw)
	}
	if _, exists := mcp.Get("codegraph"); !exists {
		t.Fatalf("v1 entry was removed: %s", raw)
	}
	if servers, exists := mapChild(mcp, "servers"); exists {
		if _, exists := servers.Get("codegraph"); exists {
			t.Fatalf("v2 entry survived: %s", raw)
		}
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
	v1Path := util.OpenCodePathsResolved().Config
	v2Path := util.OpenCodePathsV2().Config
	staleV1 := `{"mcp":{"context-mode":{"type":"local","command":["old"],"enabled":true}}}`
	if err := util.WriteFile(v1Path, staleV1); err != nil {
		t.Fatal(err)
	}
	body := `{"mcp":{"servers":{"context-mode":{"type":"local","command":["context-mode","serve"],"disabled":false}}}}`
	if err := util.WriteFile(v2Path, body); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureOpenCodeMcpV2("context-mode")
	if !changed {
		t.Fatal("outdated v2 entry must be healed and rewritten")
	}
	raw, _ := util.ReadFileSafe(v2Path)
	cfg := util.TryParseJsonc(raw)
	mcp, _ := mapChild(cfg, "mcp")
	servers, ok := mapChild(mcp, "servers")
	if !ok {
		t.Fatalf("v2 servers map lost after heal: %s", raw)
	}
	entry, has := servers.Get("context-mode")
	em, valid := entry.(*util.OrderedMap)
	if !has || !valid || !openCodeMcpEntryCommandMatches(em, openCodeMcpCommand("opencode2", "context-mode")) || !openCodeMcpEntryEnabled(em) {
		t.Fatalf("v2 entry not healed: %s", raw)
	}
	if v1Path != v2Path {
		gotV1, exists := util.ReadFileSafe(v1Path)
		if !exists {
			t.Fatalf("v1 config missing after v2 heal")
		}
		v1Cfg := util.TryParseJsonc(gotV1)
		v1Mcp, _ := mapChild(v1Cfg, "mcp")
		entry, has := v1Mcp.Get("context-mode")
		em, valid := entry.(*util.OrderedMap)
		if !has || !valid || !openCodeMcpEntryCommandMatches(em, []string{"old"}) {
			t.Fatalf("stale v1 entry changed by v2 heal: %s", gotV1)
		}
	}
}

func TestOpenCodeInstalledMajorWithoutShim(t *testing.T) {
	skipOnWindows(t)
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
