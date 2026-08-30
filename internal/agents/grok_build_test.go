package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func setGrokBuildTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GROK_HOME", "")
	return home
}

func TestGrokBuildHomeDirRespectsGROK_HOME(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	if got := grokBuildHomeDir(); got != dir {
		t.Fatalf("grokBuildHomeDir = %s, want %s", got, dir)
	}
	home := setGrokBuildTestHome(t)
	if got := grokBuildHomeDir(); got != filepath.Join(home, ".grok") {
		t.Fatalf("grokBuildHomeDir default = %s", got)
	}
}

func TestStripGrokBuildBlocksRemovesMarkerBlock(t *testing.T) {
	content := "before\n\n" + renderStripFixture() + "\nafter\n"
	got := stripGrokBuildBlocks(content)
	if strings.Contains(got, "headroom:grok-build") || strings.Contains(got, "127.0.0.1") {
		t.Fatalf("marker block survived strip:\n%s", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("user content lost:\n%s", got)
	}
}

func TestStripGrokBuildBlocksNoopWithoutMarkers(t *testing.T) {
	content := "[models]\ndefault = \"grok-4.6\"\n"
	if got := stripGrokBuildBlocks(content); got != content {
		t.Fatalf("strip changed clean config:\n%s", got)
	}
}

func TestStripGrokBuildBlocksRemovesBareTable(t *testing.T) {
	content := "[models]\ndefault = \"grok-4.6\"\n\n[model.grok-build]\nbase_url = \"http://127.0.0.1:8787/v1\"\n\n[model.other]\nname = \"keep\"\n"
	got := stripGrokBuildBlocks(content)
	if strings.Contains(got, "[model.grok-build]") || strings.Contains(got, "127.0.0.1") {
		t.Fatalf("bare legacy table survived strip:\n%s", got)
	}
	if !strings.Contains(got, "[model.other]") || !strings.Contains(got, "keep") {
		t.Fatalf("following table lost:\n%s", got)
	}
	if !strings.Contains(got, "[models]") {
		t.Fatalf("preceding table lost:\n%s", got)
	}
}

func TestStripGrokBuildBlocksRemovesBareTableAtEOF(t *testing.T) {
	content := "[models]\ndefault = \"grok-4.6\"\n\n[model.grok-build]\nbase_url = \"http://127.0.0.1:8787/v1\"\n"
	got := stripGrokBuildBlocks(content)
	if strings.Contains(got, "grok-build") || strings.Contains(got, "127.0.0.1") {
		t.Fatalf("EOF table survived strip:\n%s", got)
	}
	if !strings.Contains(got, "[models]") {
		t.Fatalf("preceding table lost:\n%s", got)
	}
}

func renderStripFixture() string {
	return grokBuildMarkerStart + "\n[model.grok-build]\nbase_url = \"http://127.0.0.1:8787/v1\"\n" + grokBuildMarkerEnd
}


func TestRemoveGrokBuildProxyStripsMarkersWithoutBackup(t *testing.T) {
	_ = setGrokBuildTestHome(t)
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	content := "[models]\ndefault = \"grok-4.6\"\n\n" + renderStripFixture() + "\n"
	if err := os.WriteFile(grokBuildConfigFile(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !RemoveGrokBuildProxy() {
		t.Fatal("remove failed")
	}
	raw, _ := os.ReadFile(grokBuildConfigFile())
	if strings.Contains(string(raw), "headroom:grok-build") {
		t.Fatalf("marker survived:\n%s", string(raw))
	}
}

func TestRemoveGrokBuildProxyNoopWhenAbsent(t *testing.T) {
	_ = setGrokBuildTestHome(t)
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	if RemoveGrokBuildProxy() {
		t.Fatal("remove must be noop when config absent")
	}
}

func TestGrokBuildDoesNotFollowTmpSymlink(t *testing.T) {
	_ = setGrokBuildTestHome(t)
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	if err := os.WriteFile(grokBuildConfigFile(), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("do-not-touch"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "config.toml.tmp")
	if err := os.Symlink(victim, tmp); err != nil {
		t.Skip("symlinks unsupported")
	}
	if err := util.WriteFileAtomic(grokBuildConfigFile(), "replaced\n", 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(victim)
	if string(raw) != "do-not-touch" {
		t.Fatal("atomic write followed tmp symlink")
	}
}

func TestGrokOAuthBaseURLDefaults(t *testing.T) {
	t.Setenv("TOKLESS_GROK_PROXY_PORT", "")
	if got := util.GrokOAuthProxyPort(); got != 8788 {
		t.Fatalf("default port = %d", got)
	}
	t.Setenv("TOKLESS_GROK_PROXY_PORT", "9100")
	if got := util.GrokOAuthProxyBaseURL(); got != "http://127.0.0.1:9100/v1" {
		t.Fatalf("base url = %s", got)
	}
}
