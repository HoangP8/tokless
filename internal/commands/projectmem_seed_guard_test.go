package commands

import (
	"os"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func TestProjectmemSeedProjectGuards(t *testing.T) {
	manifest := &core.ToolManifest{ID: "projectmem"}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	for _, tc := range []struct {
		name string
		opts InitOptions
		env  string
	}{
		{"dry-run", InitOptions{DryRun: true}, ""},
		{"test-guard", InitOptions{}, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			// Make it look like a project so only the guard prevents seeding.
			if err := os.WriteFile("go.mod", []byte("module probe\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TOKLESS_TEST", tc.env)
			projectmemSeedProject(tc.opts, []*core.ToolManifest{manifest})
			if util.Exists(".projectmem") {
				t.Fatalf("%s: .projectmem created", tc.name)
			}
		})
	}
}
