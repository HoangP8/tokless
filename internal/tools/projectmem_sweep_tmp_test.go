package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepProjectmemTemps(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".tokless-write-stale")
	fresh := filepath.Join(dir, ".tokless-write-fresh")
	keep := filepath.Join(dir, "projectmem-context.md")
	for _, p := range []string{stale, fresh, keep} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	sweepProjectmemTemps(dir)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale temp not removed")
	}
	for _, p := range []string{fresh, keep} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should be kept: %v", filepath.Base(p), err)
		}
	}
}
