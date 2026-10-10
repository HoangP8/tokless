package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestAmpRtkPermissions(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	config := util.AmpPathsResolved().Config
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	commands := []string{"git", "ls"}
	if _, file := configureAmpRtkPermissions(commands, true); file != config {
		t.Fatalf("config path %q", file)
	}
	if !AmpRtkPermissionsWiredWith(commands, true) {
		t.Fatal("expected matching rules wired")
	}
	if configure, _ := configureAmpRtkPermissions(commands, true); configure {
		t.Fatal("re-add was not idempotent")
	}
	raw, _ := util.ReadFileSafe(config)
	cfg := util.TryParseJsonc(raw)
	v, _ := cfg.Get("amp.permissions")
	rules := v.([]any)
	got := []string{}
	for _, item := range rules {
		m := item.(*util.OrderedMap)
		mm, _ := m.Get("matches")
		cmd, _ := anyString(asOrdered(mm), "cmd")
		got = append(got, cmd)
	}
	if !reflect.DeepEqual(got, []string{"rtk git *", "rtk ls *"}) {
		t.Fatalf("rules %v", got)
	}
	if err := util.WriteFile(config, `{"amp.mcpServers":{"codegraph":{"type":"stdio"}},"amp.permissions":[{"tool":"Bash","matches":{"cmd":"rtk *"},"action":"allow"},{"tool":"Bash","matches":{"cmd":"rtk git *"},"action":"allow"},{"tool":"Bash","matches":{"cmd":"other"},"action":"allow"}]}`); err != nil {
		t.Fatal(err)
	}
	if !RemoveAmpRtkPermissionsWith(commands) {
		t.Fatal("expected owned rules removed")
	}
	raw, _ = util.ReadFileSafe(config)
	if raw == "" || !strings.Contains(raw, "other") || strings.Contains(raw, "rtk *") || strings.Contains(raw, "rtk git *") {
		t.Fatalf("unexpected remaining rules: %s", raw)
	}
	_ = util.WriteFile(config, `{"amp":{"permissions":[]}}`)
	configureAmpRtkPermissions(nil, true)
	raw, _ = util.ReadFileSafe(config)
	if strings.Contains(raw, "rtk") {
		t.Fatalf("empty command list wrote rules: %s", raw)
	}
}

func TestAmpRtkPermissionsWiredRejectsStale(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	_, _ = configureAmpRtkPermissions([]string{"git"}, true)
	if AmpRtkPermissionsWiredWith([]string{"git", "ls"}, true) {
		t.Fatal("stale command list passed")
	}
}
