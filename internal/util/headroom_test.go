package util

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHeadroomPathsEnvAndSpawnAreToklessOwned(t *testing.T) {
	home := t.TempDir()
	SetHomeOverride(home)
	t.Cleanup(func() { SetHomeOverride("") })
	p := HeadroomPathsResolved()
	root := filepath.Join(home, ".local", "share", "tokless", "headroom")
	for _, path := range []string{p.Root, p.UV, p.Tools, p.Bin, p.Python, HeadroomBin()} {
		if !strings.HasPrefix(path, root) {
			t.Fatalf("path outside Tokless root: %q", path)
		}
	}
	for _, env := range HeadroomEnv() {
		if !strings.Contains(env, root) && env != "UV_MANAGED_PYTHON=1" {
			t.Fatalf("environment outside Tokless root: %q", env)
		}
	}
	spawn := McpSpawnFor("headroom")
	want := []string{"run-mcp", "--tool", "headroom", HeadroomBin(), "mcp", "serve"}
	if spawn.Command == HeadroomBin() || !reflect.DeepEqual(spawn.Args, want) {
		t.Fatalf("spawn = %q %v, want wrapped %v", spawn.Command, spawn.Args, want)
	}
}
