package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGitForTools(t *testing.T) {
	temp := t.TempDir()
	gitPath := filepath.Join(temp, "git")
	err := os.WriteFile(gitPath, []byte("#!/bin/sh\necho git"), 0755)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", temp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := EnsureDeps(DepNeeds{Git: true}); !got.Git {
		t.Errorf("EnsureDeps(DepNeeds{Git:true}).Git = false, want true when git is in PATH")
	}
}
