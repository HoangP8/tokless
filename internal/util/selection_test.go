package util

import (
	"os"
	"path/filepath"
	"testing"
)

func withSelectionHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := homeOverride
	homeOverride = dir
	t.Cleanup(func() { homeOverride = old })
	return dir
}

func TestSelectionRoundTrip(t *testing.T) {
	withSelectionHome(t)

	if LoadSelection() != nil {
		t.Fatal("expected no selection before save")
	}
	if err := SaveSelection("selected", []string{"rtk", "codegraph"}); err != nil {
		t.Fatalf("SaveSelection failed: %v", err)
	}

	store := LoadSelection()
	if store == nil {
		t.Fatal("LoadSelection returned nil")
	}
	if store.Mode != "selected" {
		t.Errorf("expected mode 'selected', got %q", store.Mode)
	}
	if len(store.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(store.Tools))
	}
	if !IsToolSelected("rtk") || !IsToolSelected("codegraph") {
		t.Error("saved tools should be selected")
	}
	if IsToolSelected("headroom") {
		t.Error("unsaved tool should not be selected")
	}
	if _, err := os.Stat(SelectionPath()); err != nil {
		t.Fatalf("selection.json not written under data dir: %v", err)
	}
}

func TestSelectionFullModeSelectsAll(t *testing.T) {
	withSelectionHome(t)

	if err := SaveSelection("full", nil); err != nil {
		t.Fatalf("SaveSelection failed: %v", err)
	}
	store := LoadSelection()
	if store == nil || store.Mode != "full" {
		t.Fatalf("expected full mode, got %+v", store)
	}
	for _, id := range []string{"rtk", "caveman", "codegraph", "context-mode", "headroom", "ponytail", "projectmem", "principles"} {
		if !IsToolSelected(id) {
			t.Errorf("%s should be selected in full mode", id)
		}
	}
}

func TestSelectionInvalidModeIsNoOp(t *testing.T) {
	withSelectionHome(t)

	if err := SaveSelection("invalid", nil); err != nil {
		t.Fatalf("SaveSelection unexpectedly failed: %v", err)
	}
	if LoadSelection() != nil {
		t.Error("invalid mode should not be persisted")
	}
}

func TestSelectionCorruptedFile(t *testing.T) {
	withSelectionHome(t)

	if err := os.MkdirAll(filepath.Dir(SelectionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SelectionPath(), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LoadSelection() != nil {
		t.Error("corrupted selection file should return nil")
	}
}
