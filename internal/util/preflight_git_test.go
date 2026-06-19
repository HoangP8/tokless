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

	if _, gitOK := EnsureDeps(false, true, 0); !gitOK {
		t.Errorf("EnsureDeps(false, true) gitOK = false, want true when git is in PATH")
	}
}
