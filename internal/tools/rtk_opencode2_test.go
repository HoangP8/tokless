package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// TestRTKLocationForOpenCode2 pins the OpenCode v2 harness to the v2 runtime plugin, so doctor's RTK row does not report "not wired".
func TestRTKLocationForOpenCode2(t *testing.T) {
	path, label := RTKLocationFor("opencode2")
	if path == "" {
		t.Fatal("RTKLocationFor(opencode2) returned an empty path")
	}
	if label != "plugin tokless-rtk-v2.ts" {
		t.Fatalf("RTKLocationFor(opencode2) label = %q, want %q", label, "plugin tokless-rtk-v2.ts")
	}
	if want := openCodeRtkV2PluginPath(); path != want {
		t.Fatalf("RTKLocationFor(opencode2) path = %q, want %q", path, want)
	}
	if want := filepath.Join(util.OpenCodePathsV2().PluginsDir, "tokless-rtk-v2.ts"); path != want {
		t.Fatalf("RTKLocationFor(opencode2) path = %q, want %q", path, want)
	}
	if dir := filepath.Dir(path); dir != util.OpenCodePathsV2().PluginsDir {
		t.Fatalf("RTKLocationFor(opencode2) path %q is not under v2 plugins dir %q", path, util.OpenCodePathsV2().PluginsDir)
	}
}

// openCodeStubRuntimes installs opencode v1 and v2 stub binaries so the detected majors are the same on every host.
func openCodeStubRuntimes(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	suffix, bodyFor := "", func(version string) string { return "#!/bin/sh\nprintf '" + version + "'\n" }
	if runtime.GOOS == "windows" {
		suffix = ".bat"
		bodyFor = func(version string) string { return "@echo off\r\necho " + version + "\r\n" }
	}
	for name, version := range map[string]string{"opencode": "1.9.0", "opencode2": "2.1.0"} {
		bin := filepath.Join(binDir, name+suffix)
		if err := os.WriteFile(bin, []byte(bodyFor(version)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
}

// TestRtkV1LaneLeavesV2PluginUntouched proves the v1 lane never creates or rewrites the v2 lane's rtk plugin.
func TestRtkV1LaneLeavesV2PluginUntouched(t *testing.T) {
	opencode2TestHome(t)
	openCodeStubRuntimes(t)
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 wire = %v, %v", ran, err)
	}
	if !util.Exists(openCodeRtkPluginPath()) {
		t.Fatalf("v1 wire did not write %s", openCodeRtkPluginPath())
	}
	if util.Exists(openCodeRtkV2PluginPath()) {
		t.Fatalf("v1 wire created the v2 plugin: %s", openCodeRtkV2PluginPath())
	}
	if ran, err := rtk.WireFor["opencode2"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v2 wire = %v, %v", ran, err)
	}
	before, ok := util.ReadFileSafe(openCodeRtkV2PluginPath())
	if !ok {
		t.Fatalf("missing v2 plugin %s", openCodeRtkV2PluginPath())
	}
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 rewire = %v, %v", ran, err)
	}
	after, ok := util.ReadFileSafe(openCodeRtkV2PluginPath())
	if !ok || after != before {
		t.Fatalf("v1 wire changed the v2 plugin:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestRtkV2LaneLeavesV1PluginUntouched proves the v2 lane never creates or rewrites the v1 lane's rtk plugin.
func TestRtkV2LaneLeavesV1PluginUntouched(t *testing.T) {
	opencode2TestHome(t)
	openCodeStubRuntimes(t)
	if ran, err := rtk.WireFor["opencode2"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v2 wire = %v, %v", ran, err)
	}
	if !util.Exists(openCodeRtkV2PluginPath()) {
		t.Fatalf("v2 wire did not write %s", openCodeRtkV2PluginPath())
	}
	if util.Exists(openCodeRtkPluginPath()) {
		t.Fatalf("v2 wire created the v1 plugin: %s", openCodeRtkPluginPath())
	}
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 wire = %v, %v", ran, err)
	}
	before, ok := util.ReadFileSafe(openCodeRtkPluginPath())
	if !ok {
		t.Fatalf("missing v1 plugin %s", openCodeRtkPluginPath())
	}
	if ran, err := rtk.WireFor["opencode2"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v2 rewire = %v, %v", ran, err)
	}
	after, ok := util.ReadFileSafe(openCodeRtkPluginPath())
	if !ok || after != before {
		t.Fatalf("v2 wire changed the v1 plugin:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestRtkV2UnwireRemovesOnlyV2Plugin proves the v2 lane's unwire takes the v2 plugin only and leaves the v1 lane's plugin in place.
func TestRtkV2UnwireRemovesOnlyV2Plugin(t *testing.T) {
	opencode2TestHome(t)
	openCodeStubRuntimes(t)
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 wire = %v, %v", ran, err)
	}
	if ran, err := rtk.WireFor["opencode2"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v2 wire = %v, %v", ran, err)
	}
	if ok, err := rtk.UnwireFor["opencode2"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("v2 unwire = %v, %v", ok, err)
	}
	if util.Exists(openCodeRtkV2PluginPath()) {
		t.Fatalf("v2 plugin survived the v2 unwire: %s", openCodeRtkV2PluginPath())
	}
	if !util.Exists(openCodeRtkPluginPath()) {
		t.Fatalf("v2 unwire dropped the v1 plugin: %s", openCodeRtkPluginPath())
	}
}

// TestRtkV1WireRemovesStaleV2PluginInV1Dir proves the v1 lane drops the v2-shaped plugin an older tokless left in the v1 dir, and leaves the v2 lane's own copy alone.
func TestRtkV1WireRemovesStaleV2PluginInV1Dir(t *testing.T) {
	tmp := opencode2TestHome(t)
	openCodeStubRuntimes(t)
	openCodeSeparateV2PluginsDir(t, tmp)
	v1Stale := filepath.Join(util.OpenCodePathsResolved().PluginsDir, openCodeRtkV2File)
	v2File := filepath.Join(util.OpenCodePathsV2().PluginsDir, openCodeRtkV2File)
	stale := fmt.Sprintf(openCodeRtkV2Plugin, `"tokless"`)
	for _, path := range []string{v1Stale, v2File} {
		if err := util.WriteFile(path, stale); err != nil {
			t.Fatal(err)
		}
	}
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 wire = %v, %v", ran, err)
	}
	if util.Exists(v1Stale) {
		t.Fatalf("stale v2 plugin survived the v1 wire: %s", v1Stale)
	}
	raw, ok := util.ReadFileSafe(v2File)
	if !ok || raw != stale {
		t.Fatalf("v2 lane plugin changed: %s", v2File)
	}
	if !util.Exists(openCodeRtkPluginPath()) {
		t.Fatalf("v1 wire did not write %s", openCodeRtkPluginPath())
	}
}

// TestRtkV1UnwireRemovesStaleV2PluginInV1Dir proves the v1 lane's unwire drops the stale v2-shaped plugin in the v1 dir and keeps the v2 lane's copy.
func TestRtkV1UnwireRemovesStaleV2PluginInV1Dir(t *testing.T) {
	tmp := opencode2TestHome(t)
	openCodeStubRuntimes(t)
	openCodeSeparateV2PluginsDir(t, tmp)
	v1Stale := filepath.Join(util.OpenCodePathsResolved().PluginsDir, openCodeRtkV2File)
	v2File := filepath.Join(util.OpenCodePathsV2().PluginsDir, openCodeRtkV2File)
	stale := fmt.Sprintf(openCodeRtkV2Plugin, `"tokless"`)
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 wire = %v, %v", ran, err)
	}
	for _, path := range []string{v1Stale, v2File} {
		if err := util.WriteFile(path, stale); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := rtk.UnwireFor["opencode"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("v1 unwire = %v, %v", ok, err)
	}
	if util.Exists(v1Stale) {
		t.Fatalf("stale v2 plugin survived the v1 unwire: %s", v1Stale)
	}
	if util.Exists(openCodeRtkPluginPath()) {
		t.Fatalf("v1 plugin survived the v1 unwire: %s", openCodeRtkPluginPath())
	}
	raw, ok := util.ReadFileSafe(v2File)
	if !ok || raw != stale {
		t.Fatalf("v2 lane plugin changed: %s", v2File)
	}
}

// TestRtkV1WireKeepsForeignV2NamedFile proves the v1 lane removes only a tokless-written file, never a foreign one that shares the name.
func TestRtkV1WireKeepsForeignV2NamedFile(t *testing.T) {
	tmp := opencode2TestHome(t)
	openCodeStubRuntimes(t)
	openCodeSeparateV2PluginsDir(t, tmp)
	foreign := filepath.Join(util.OpenCodePathsResolved().PluginsDir, openCodeRtkV2File)
	if err := util.WriteFile(foreign, "// user plugin\n"); err != nil {
		t.Fatal(err)
	}
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 wire = %v, %v", ran, err)
	}
	raw, ok := util.ReadFileSafe(foreign)
	if !ok || raw != "// user plugin\n" {
		t.Fatalf("foreign file removed or changed: %s", foreign)
	}
}

// TestRtkV1WireKeepsV2PluginInSharedDir proves the v1 lane leaves the v2-named file alone when both lanes share one plugins dir.
func TestRtkV1WireKeepsV2PluginInSharedDir(t *testing.T) {
	opencode2TestHome(t)
	openCodeStubRuntimes(t)
	if openCodeV2PluginsDirIsSeparate() {
		t.Skip("opencode v2 has its own plugins dir in this environment")
	}
	shared := filepath.Join(util.OpenCodePathsResolved().PluginsDir, openCodeRtkV2File)
	stale := fmt.Sprintf(openCodeRtkV2Plugin, `"tokless"`)
	if err := util.WriteFile(shared, stale); err != nil {
		t.Fatal(err)
	}
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 wire = %v, %v", ran, err)
	}
	raw, ok := util.ReadFileSafe(shared)
	if !ok || raw != stale {
		t.Fatalf("shared v2 plugin removed or changed: %s", shared)
	}
}
