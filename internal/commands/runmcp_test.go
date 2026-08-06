//go:build !windows

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunMcpInitializesCodegraphBeforeProxy(t *testing.T) {
	binDir := t.TempDir()
	project := t.TempDir()
	log := filepath.Join(binDir, "calls")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"echo \"$*\" >> \"$CODEGRAPH_LOG\"\n" +
		"if [ \"$1\" = init ]; then mkdir -p .codegraph; touch .codegraph/codegraph.db; exit 0; fi\n" +
		"[ -f .codegraph/codegraph.db ] || exit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "codegraph"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEGRAPH_LOG", log)
	t.Setenv("TOKLESS_TEST", "")
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	if code := RunMcp([]string{"codegraph", "serve"}); code != 0 {
		t.Fatalf("RunMcp = %d", code)
	}
	got, err := os.ReadFile(log)
	want := "init -i\nserve --path " + project + "\n"
	if err != nil || string(got) != want {
		t.Fatalf("calls = %q, err = %v; want %q", got, err, want)
	}
}

func TestInjectCodegraphPathPinsProjectRoot(t *testing.T) {
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	got := injectCodegraphPath([]string{"codegraph", "serve", "--mcp"})
	want := []string{"codegraph", "serve", "--mcp", "--path", project}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestInjectCodegraphPathKeepsExistingPathFlag(t *testing.T) {
	argv := []string{"codegraph", "serve", "--mcp", "--path", "/custom/root"}
	got := injectCodegraphPath(argv)
	if len(got) != len(argv) {
		t.Fatalf("argv mutated: got %v, want %v", got, argv)
	}
	for i := range argv {
		if got[i] != argv[i] {
			t.Fatalf("argv mutated: got %v, want %v", got, argv)
		}
	}
}

func TestInjectCodegraphPathKeepsEqualsFormAndShortCombined(t *testing.T) {
	cases := [][]string{
		{"codegraph", "serve", "--mcp", "--path=/custom/root"},
		{"codegraph", "serve", "--mcp", "-p/custom/root"},
	}
	for _, argv := range cases {
		got := injectCodegraphPath(argv)
		if len(got) != len(argv) {
			t.Fatalf("argv mutated for %v: got %v, want %v", argv, got, argv)
		}
		for i := range argv {
			if got[i] != argv[i] {
				t.Fatalf("argv mutated for %v: got %v, want %v", argv, got, argv)
			}
		}
	}
}

func TestInjectCodegraphPathSkipsNonProjectDir(t *testing.T) {
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	if err := os.Chdir(empty); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	argv := []string{"codegraph", "serve", "--mcp"}
	got := injectCodegraphPath(argv)
	if len(got) != len(argv) {
		t.Fatalf("argv mutated in non-project dir: got %v, want %v", got, argv)
	}
}

func TestRunMcpSyncsExistingCodegraphBeforeProxy(t *testing.T) {
	binDir := t.TempDir()
	project := t.TempDir()
	log := filepath.Join(binDir, "calls")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"echo \"$*\" >> \"$CODEGRAPH_LOG\"\n" +
		"[ -f .codegraph/codegraph.db ] || exit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "codegraph"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
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
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	if code := RunMcp([]string{"--agent", "omp", "codegraph", "serve"}); code != 0 {
		t.Fatalf("RunMcp = %d", code)
	}
	got, err := os.ReadFile(log)
	want := "sync\nserve --path " + project + "\n"
	if err != nil || string(got) != want {
		t.Fatalf("calls = %q, err = %v; want %q", got, err, want)
	}
}
