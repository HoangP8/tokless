package util

import (
	"encoding/json"
	"path/filepath"
)

// SelectionStore stores the selected tool mode and IDs.
type SelectionStore struct {
	Mode   string   // "full" or "selected"
	Tools  []string // selected tool IDs (when Mode == "selected")
}

// SelectionPath returns the selection file path.
func SelectionPath() string {
	return filepath.Join(ToklessDataDir(), "selection.json")
}

// LoadSelection reads persisted selection; returns nil if file missing or invalid.
func LoadSelection() *SelectionStore {
	data, ok := ReadFileSafe(SelectionPath())
	if !ok {
		return nil
	}
	var store SelectionStore
	if err := json.Unmarshal([]byte(data), &store); err != nil {
		return nil
	}
	if store.Mode != "full" && store.Mode != "selected" {
		return nil
	}
	return &store
}

// SaveSelection writes mode and tool IDs to disk.
func SaveSelection(mode string, tools []string) error {
	if mode != "full" && mode != "selected" {
		return nil // ignore invalid
	}
	store := SelectionStore{Mode: mode, Tools: tools}
	b, err := json.Marshal(store)
	if err != nil {
		return err
	}
	if err := EnsureDir(ToklessDataDir()); err != nil {
		return err
	}
	return WriteFile(SelectionPath(), string(b))
}

// IsToolSelected reports whether tool is selected; missing selection defaults to all.
func IsToolSelected(toolID string) bool {
	store := LoadSelection()
	if store == nil {
		return true
	}
	if store.Mode == "full" {
		return true
	}
	for _, id := range store.Tools {
		if id == toolID {
			return true
		}
	}
	return false
}