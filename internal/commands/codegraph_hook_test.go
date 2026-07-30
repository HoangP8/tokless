//go:build !windows

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCodegraphIndexHookUsesSharedBlockingIndex(t *testing.T) {
	binDir := t.TempDir()
	project := t.TempDir()
	log := filepath.Join(binDir, "calls")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"echo \"$*\" >> \"$CODEGRAPH_LOG\"\n" +
		"[ \"$1\" = init ] || exit 1\nmkdir -p .codegraph\ntouch .codegraph/codegraph.db\n"
	if err := os.WriteFile(filepath.Join(binDir, "codegraph"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEGRAPH_LOG", log)
	t.Setenv("TOKLESS_TEST", "")

	input := `{"workspacePaths":["` + project + `"]}`
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.WriteString(input); err != nil {
		t.Fatal(err)
	}
	write.Close()
	oldStdin := os.Stdin
	os.Stdin = read
	t.Cleanup(func() { os.Stdin = oldStdin })

	if code := RunCodegraphIndexHook(); code != 0 {
		t.Fatalf("RunCodegraphIndexHook = %d", code)
	}
	if _, err := os.Stat(filepath.Join(project, ".codegraph")); err != nil {
		t.Fatalf("init did not finish before hook returned: %v", err)
	}
	if got, err := os.ReadFile(log); err != nil || string(got) != "init -i\n" {
		t.Fatalf("calls = %q, err = %v", got, err)
	}
}

func TestRunCodegraphIndexHookSkipsExistingIndex(t *testing.T) {
	binDir := t.TempDir()
	project := t.TempDir()
	log := filepath.Join(binDir, "calls")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "codegraph"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".codegraph", "codegraph.db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEGRAPH_LOG", log)
	t.Setenv("TOKLESS_TEST", "")
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = write.WriteString(`{"workspacePaths":["` + project + `"]}`)
	_ = write.Close()
	oldStdin := os.Stdin
	os.Stdin = read
	t.Cleanup(func() { os.Stdin = oldStdin })
	if code := RunCodegraphIndexHook(); code != 0 {
		t.Fatalf("RunCodegraphIndexHook = %d", code)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("existing index ran CodeGraph: %v", err)
	}
}

func TestRunCodegraphIndexHookFailsOpen(t *testing.T) {
	binDir := t.TempDir()
	project := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "codegraph"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TOKLESS_TEST", "")
	input := `{"workspacePaths":["` + project + `"]}`
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.WriteString(input); err != nil {
		t.Fatal(err)
	}
	write.Close()
	oldStdin := os.Stdin
	os.Stdin = read
	t.Cleanup(func() { os.Stdin = oldStdin })

	if code := RunCodegraphIndexHook(); code != 0 {
		t.Fatalf("RunCodegraphIndexHook = %d, want fail-open 0", code)
	}
}
