package tools

import (
	"testing"
)

func TestManifestNeedsGit(t *testing.T) {
	if !caveman.NeedsGit {
		t.Errorf("caveman.NeedsGit = false, want true")
	}
	if !caveman.NeedsNode {
		t.Errorf("caveman.NeedsNode = false, want true")
	}
	if ponytail.NeedsNode {
		t.Errorf("ponytail.NeedsNode = true, want false")
	}
	if codegraph.NeedsGit {
		t.Errorf("codegraph.NeedsGit = true, want false")
	}
}
