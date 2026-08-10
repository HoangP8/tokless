//go:build linux

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHookProjectDirNormalizesWSLWindowsPath(t *testing.T) {
	binDir := t.TempDir()
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wslpath := "#!/bin/sh\nprintf '%s\\n' '" + project + "'\n"
	if err := os.WriteFile(filepath.Join(binDir, "wslpath"), []byte(wslpath), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")

	input := []byte(`{"workspacePaths":["C:\\src\\project"]}`)
	if got := resolveHookProjectDirFromInput(input); got != project {
		t.Fatalf("project dir = %q, want %q", got, project)
	}
}

func TestResolveHookProjectDirSkipsUnconvertibleWSLWindowsPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")

	input := []byte(`{"workspacePaths":["C:\\src\\project"]}`)
	if got := resolveHookProjectDirFromInput(input); got != "" {
		t.Fatalf("project dir = %q, want empty", got)
	}
}
