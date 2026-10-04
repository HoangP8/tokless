package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// chdirTemp moves cwd out of the package dir so tests that write
// project-local files land in a temp dir, and restores the original cwd.
func chdirTemp(t *testing.T) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func TestUnwireRemovesStaleRuleFile(t *testing.T) {
	dir := t.TempDir()
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	chdirTemp(t)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	tm := core.GetTool("projectmem")
	fn, ok := tm.UnwireFor["grok"]
	if !ok {
		t.Fatal("no grok unwire")
	}
	rf := filepath.Join(dir, ".grok", "rules", "projectmem-context.md")
	if err := util.WriteFile(rf, "## projectmem context (budget: 800 tokens)\nfrozen\n"); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, ".clinerules", "projectmem-context.md")
	_ = util.EnsureDir(filepath.Dir(foreign))
	if err := util.WriteFile(foreign, "user's own notes, no marker\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := fn(core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if util.Exists(rf) {
		t.Fatal("stale rule file survived unwire")
	}
	if !util.Exists(foreign) {
		t.Fatal("foreign unmarked file was deleted")
	}
}

func TestProjectmemSessionTextNoMem(t *testing.T) {
	if got := ProjectmemSessionText(t.TempDir()); got != "" {
		t.Fatalf("got %q", got)
	}
}
