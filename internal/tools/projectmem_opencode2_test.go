package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

// TestProjectmemOpenCode2UsesV2Template proves the v2 harness gets the v2 hook template at its own file name, not the kilo one.
func TestProjectmemOpenCode2UsesV2Template(t *testing.T) {
	opencode2TestHome(t)
	if !writeProjectmemExtension("opencode2") {
		t.Fatal("writeProjectmemExtension(opencode2) = false")
	}
	path := filepath.Join(util.OpenCodePathsV2().PluginsDir, "projectmem-context-v2.ts")
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		t.Fatalf("missing v2 projectmem plugin %s", path)
	}
	if !strings.Contains(raw, "ctx.session.hook") {
		t.Fatalf("v2 plugin missing the v2 hook:\n%s", raw)
	}
	if !hasProjectmemExtension("opencode2") {
		t.Fatal("hasProjectmemExtension(opencode2) = false after write")
	}
	exe := util.ToklessPersistedAbs()
	if want := fmt.Sprintf(openCodeProjectmemV2Plugin, strconv.Quote(exe)); raw != want {
		t.Fatalf("v2 plugin content mismatch:\n%s", raw)
	}
	kiloMarker := "experimental.chat.system.transform"
	if !strings.Contains(kiloProjectmemPlugin, kiloMarker) {
		t.Fatalf("kilo template lost its distinctive marker %q", kiloMarker)
	}
	if strings.Contains(raw, kiloMarker) {
		t.Fatalf("v2 plugin must not carry the kilo template:\n%s", raw)
	}
}

// TestProjectmemOpenCode2RemovesLegacyFileInSeparateDir proves a stale projectmem-context.ts in the v2 plugins dir is dropped, and only there.
func TestProjectmemOpenCode2RemovesLegacyFileInSeparateDir(t *testing.T) {
	tmp := opencode2TestHome(t)
	v2Dir := filepath.Join(tmp, ".config", "opencode2")
	if err := os.MkdirAll(v2Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG_DIR_V2", v2Dir)
	if !openCodeV2PluginsDirIsSeparate() {
		t.Skip("opencode v2 shares the v1 plugins dir in this environment")
	}
	legacy := filepath.Join(util.OpenCodePathsV2().PluginsDir, "projectmem-context.ts")
	if err := util.WriteFile(legacy, "// stale legacy plugin\n"); err != nil {
		t.Fatal(err)
	}
	if !writeProjectmemExtension("opencode2") {
		t.Fatal("writeProjectmemExtension(opencode2) = false")
	}
	if util.Exists(legacy) {
		t.Fatalf("stale legacy plugin survived the v2 write: %s", legacy)
	}
	if err := util.WriteFile(legacy, "// stale legacy plugin\n"); err != nil {
		t.Fatal(err)
	}
	removeProjectmemExtension("opencode2")
	if util.Exists(legacy) {
		t.Fatalf("stale legacy plugin survived the v2 removal: %s", legacy)
	}
}

// TestProjectmemOpenCode2SharedDirKeepsV1File proves a shared plugins dir leaves the v1 projectmem-context.ts untouched.
func TestProjectmemOpenCode2SharedDirKeepsV1File(t *testing.T) {
	opencode2TestHome(t)
	if openCodeV2PluginsDirIsSeparate() {
		t.Skip("opencode v2 has its own plugins dir in this environment")
	}
	if !writeProjectmemExtension("opencode") {
		t.Fatal("writeProjectmemExtension(opencode) = false")
	}
	v1File := filepath.Join(util.OpenCodePathsResolved().PluginsDir, "projectmem-context.ts")
	if !util.Exists(v1File) {
		t.Fatalf("v1 projectmem plugin missing: %s", v1File)
	}
	if !writeProjectmemExtension("opencode2") {
		t.Fatal("writeProjectmemExtension(opencode2) = false")
	}
	if !util.Exists(v1File) {
		t.Fatalf("v1 projectmem plugin removed by the v2 write: %s", v1File)
	}
	if !hasProjectmemExtension("opencode") {
		t.Fatal("hasProjectmemExtension(opencode) = false after the v2 write")
	}
}

// TestProjectmemV1LaneLeavesV2PluginUntouched proves the v1 lane never creates, rewrites or removes the v2 lane's projectmem plugin.
func TestProjectmemV1LaneLeavesV2PluginUntouched(t *testing.T) {
	opencode2TestHome(t)
	v2Path := filepath.Join(util.OpenCodePathsV2().PluginsDir, openCodeProjectmemV2File)
	if util.Exists(v2Path) {
		t.Fatalf("v2 plugin exists before any write: %s", v2Path)
	}
	if !writeProjectmemExtension("opencode") {
		t.Fatal("writeProjectmemExtension(opencode) = false")
	}
	if util.Exists(v2Path) {
		t.Fatalf("v1 lane created the v2 plugin: %s", v2Path)
	}
	if !writeProjectmemExtension("opencode2") {
		t.Fatal("writeProjectmemExtension(opencode2) = false")
	}
	before, ok := util.ReadFileSafe(v2Path)
	if !ok {
		t.Fatalf("missing v2 projectmem plugin %s", v2Path)
	}
	if !writeProjectmemExtension("opencode") {
		t.Fatal("writeProjectmemExtension(opencode) = false")
	}
	after, ok := util.ReadFileSafe(v2Path)
	if !ok || after != before {
		t.Fatalf("v1 write changed the v2 plugin:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	removeProjectmemExtension("opencode")
	after, ok = util.ReadFileSafe(v2Path)
	if !ok || after != before {
		t.Fatalf("v1 removal changed the v2 plugin:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestProjectmemV2RemovalKeepsV1Plugin proves the v2 lane's removal takes the v2 plugin only, even when both lanes share one plugins dir.
func TestProjectmemV2RemovalKeepsV1Plugin(t *testing.T) {
	opencode2TestHome(t)
	if !writeProjectmemExtension("opencode") {
		t.Fatal("writeProjectmemExtension(opencode) = false")
	}
	if !writeProjectmemExtension("opencode2") {
		t.Fatal("writeProjectmemExtension(opencode2) = false")
	}
	v1Path := projectmemExtensionPath("opencode")
	v2Path := filepath.Join(util.OpenCodePathsV2().PluginsDir, openCodeProjectmemV2File)
	removeProjectmemExtension("opencode2")
	if util.Exists(v2Path) {
		t.Fatalf("v2 projectmem plugin survived the v2 removal: %s", v2Path)
	}
	if !util.Exists(v1Path) {
		t.Fatalf("v2 removal dropped the v1 plugin: %s", v1Path)
	}
}

// TestProjectmemV1WriteRemovesStaleV2PluginInV1Dir proves the v1 lane drops the v2-shaped plugin an older tokless left in the v1 dir, and leaves the v2 lane's own copy alone.
func TestProjectmemV1WriteRemovesStaleV2PluginInV1Dir(t *testing.T) {
	tmp := opencode2TestHome(t)
	openCodeSeparateV2PluginsDir(t, tmp)
	v1Stale := filepath.Join(util.OpenCodePathsResolved().PluginsDir, openCodeProjectmemV2File)
	v2File := filepath.Join(util.OpenCodePathsV2().PluginsDir, openCodeProjectmemV2File)
	stale := fmt.Sprintf(openCodeProjectmemV2Plugin, `"tokless"`)
	for _, path := range []string{v1Stale, v2File} {
		if err := util.WriteFile(path, stale); err != nil {
			t.Fatal(err)
		}
	}
	if !writeProjectmemExtension("opencode") {
		t.Fatal("writeProjectmemExtension(opencode) = false")
	}
	if util.Exists(v1Stale) {
		t.Fatalf("stale v2 plugin survived the v1 write: %s", v1Stale)
	}
	raw, ok := util.ReadFileSafe(v2File)
	if !ok || raw != stale {
		t.Fatalf("v2 lane plugin changed: %s", v2File)
	}
	if !util.Exists(projectmemExtensionPath("opencode")) {
		t.Fatalf("v1 write did not write %s", projectmemExtensionPath("opencode"))
	}
}

// TestProjectmemV1RemovalRemovesStaleV2PluginInV1Dir proves the v1 lane's removal drops the stale v2-shaped plugin in the v1 dir and keeps the v2 lane's copy.
func TestProjectmemV1RemovalRemovesStaleV2PluginInV1Dir(t *testing.T) {
	tmp := opencode2TestHome(t)
	openCodeSeparateV2PluginsDir(t, tmp)
	v1Stale := filepath.Join(util.OpenCodePathsResolved().PluginsDir, openCodeProjectmemV2File)
	v2File := filepath.Join(util.OpenCodePathsV2().PluginsDir, openCodeProjectmemV2File)
	stale := fmt.Sprintf(openCodeProjectmemV2Plugin, `"tokless"`)
	if !writeProjectmemExtension("opencode") {
		t.Fatal("writeProjectmemExtension(opencode) = false")
	}
	for _, path := range []string{v1Stale, v2File} {
		if err := util.WriteFile(path, stale); err != nil {
			t.Fatal(err)
		}
	}
	removeProjectmemExtension("opencode")
	if util.Exists(v1Stale) {
		t.Fatalf("stale v2 plugin survived the v1 removal: %s", v1Stale)
	}
	if util.Exists(projectmemExtensionPath("opencode")) {
		t.Fatalf("v1 plugin survived the v1 removal: %s", projectmemExtensionPath("opencode"))
	}
	raw, ok := util.ReadFileSafe(v2File)
	if !ok || raw != stale {
		t.Fatalf("v2 lane plugin changed: %s", v2File)
	}
}

// TestProjectmemV1WriteKeepsForeignV2NamedFile proves the v1 lane removes only a tokless-written file, never a foreign one that shares the name.
func TestProjectmemV1WriteKeepsForeignV2NamedFile(t *testing.T) {
	tmp := opencode2TestHome(t)
	openCodeSeparateV2PluginsDir(t, tmp)
	foreign := filepath.Join(util.OpenCodePathsResolved().PluginsDir, openCodeProjectmemV2File)
	if err := util.WriteFile(foreign, "// user plugin\n"); err != nil {
		t.Fatal(err)
	}
	if !writeProjectmemExtension("opencode") {
		t.Fatal("writeProjectmemExtension(opencode) = false")
	}
	raw, ok := util.ReadFileSafe(foreign)
	if !ok || raw != "// user plugin\n" {
		t.Fatalf("foreign file removed or changed: %s", foreign)
	}
}

// TestProjectmemV1WriteKeepsV2PluginInSharedDir proves the v1 lane leaves the v2-named file alone when both lanes share one plugins dir.
func TestProjectmemV1WriteKeepsV2PluginInSharedDir(t *testing.T) {
	opencode2TestHome(t)
	if openCodeV2PluginsDirIsSeparate() {
		t.Skip("opencode v2 has its own plugins dir in this environment")
	}
	shared := filepath.Join(util.OpenCodePathsResolved().PluginsDir, openCodeProjectmemV2File)
	stale := fmt.Sprintf(openCodeProjectmemV2Plugin, `"tokless"`)
	if err := util.WriteFile(shared, stale); err != nil {
		t.Fatal(err)
	}
	if !writeProjectmemExtension("opencode") {
		t.Fatal("writeProjectmemExtension(opencode) = false")
	}
	raw, ok := util.ReadFileSafe(shared)
	if !ok || raw != stale {
		t.Fatalf("shared v2 plugin removed or changed: %s", shared)
	}
}
