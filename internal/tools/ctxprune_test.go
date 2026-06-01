package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPruneOldContextModeCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	pkgs := filepath.Join(dir, ".cache", "opencode", "packages")
	mk := func(name string, withPkg bool) {
		d := filepath.Join(pkgs, name)
		if withPkg {
			os.MkdirAll(filepath.Join(d, "node_modules", "context-mode"), 0o755)
			os.WriteFile(filepath.Join(d, "node_modules", "context-mode", "package.json"), []byte(`{}`), 0o644)
		} else {
			os.MkdirAll(d, 0o755)
		}
	}
	mk("context-mode@1.0.136", true)
	mk("context-mode@1.0.146", true)
	mk("context-mode@1.0.151", true)
	mk("context-mode@latest", false)      // empty
	mk("context-mode@1.0.100", false)     // partial
	mk("oh-my-opencode-slim@1.1.1", true) // unrelated, must survive

	pruneOldContextModeCache()

	exists := func(n string) bool {
		_, err := os.Stat(filepath.Join(pkgs, n))
		return err == nil
	}
	if !exists("context-mode@1.0.151") {
		t.Fatal("newest version was removed")
	}
	for _, gone := range []string{"context-mode@1.0.136", "context-mode@1.0.146", "context-mode@latest", "context-mode@1.0.100"} {
		if exists(gone) {
			t.Fatalf("%s should have been pruned", gone)
		}
	}
	if !exists("oh-my-opencode-slim@1.1.1") {
		t.Fatal("unrelated plugin was wrongly pruned")
	}
}
