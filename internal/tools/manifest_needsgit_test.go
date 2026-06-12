package tools

import (
	"testing"
)

func TestManifestNeedsGit(t *testing.T) {
	if !caveman.NeedsGit {
		t.Errorf("caveman.NeedsGit = false, want true")
	}
	if codegraph.NeedsGit {
		t.Errorf("codegraph.NeedsGit = true, want false")
	}
}
