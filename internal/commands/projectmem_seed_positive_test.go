package commands

import (
	"os"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func TestProjectmemSeedProjectCreatesMem(t *testing.T) {
	if util.Which("pjm") == "" {
		t.Skip("pjm not installed")
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	os.WriteFile("go.mod", []byte("module probe\n"), 0o644)
	t.Setenv("TOKLESS_TEST", "")
	t.Setenv("PROJECTMEM_HOME", t.TempDir())
	projectmemSeedProject(InitOptions{}, []*core.ToolManifest{{ID: "projectmem"}})
	if _, err := os.Stat(".projectmem"); err != nil {
		t.Fatalf("seed must create .projectmem: %v", err)
	}
}
