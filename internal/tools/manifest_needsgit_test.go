package tools

import (
	"testing"
)

func TestManifestNeedsGit(t *testing.T) {
	if caveman.NeedsGit {
		t.Errorf("caveman.NeedsGit = true, want false")
	}
	if caveman.NeedsNode {
		t.Errorf("caveman.NeedsNode = true, want false")
	}
	if ponytail.NeedsNode {
		t.Errorf("ponytail.NeedsNode = true, want false")
	}
	if codegraph.NeedsGit {
		t.Errorf("codegraph.NeedsGit = true, want false")
	}
}
