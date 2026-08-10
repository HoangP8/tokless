//go:build !windows

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestInjectCodegraphPathUsesWorkspace(t *testing.T) {
	workspace := tempProjectDir(t)
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := injectCodegraphPath([]string{"codegraph", "serve"}, workspace)
	want := []string{"codegraph", "serve", "--path", workspace}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestRunMcpStartsProxyBeforeSlowCodegraphIndex(t *testing.T) {
	binDir := t.TempDir()
	project := tempProjectDir(t)
	log := filepath.Join(binDir, "calls")
	indexing := filepath.Join(project, "indexing")
	complete := filepath.Join(project, "complete")
	proxy := filepath.Join(project, "proxy")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"echo \"$*\" >> \"$CODEGRAPH_LOG\"\n" +
		"if [ \"$1\" = init ]; then touch \"$CODEGRAPH_INDEXING\"; sleep 1; mkdir -p .codegraph; touch .codegraph/codegraph.db \"$CODEGRAPH_COMPLETE\"; exit 0; fi\n" +
		"touch \"$CODEGRAPH_PROXY\"\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "codegraph"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEGRAPH_LOG", log)
	t.Setenv("CODEGRAPH_INDEXING", indexing)
	t.Setenv("CODEGRAPH_COMPLETE", complete)
	t.Setenv("CODEGRAPH_PROXY", proxy)
	t.Setenv("TOKLESS_TEST", "")
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	started := time.Now()
	if code := RunMcp([]string{"codegraph", "serve"}); code != 0 {
		t.Fatalf("RunMcp = %d", code)
	}
	if elapsed := time.Since(started); elapsed >= 900*time.Millisecond {
		t.Fatalf("RunMcp blocked for slow CodeGraph index: %s", elapsed)
	}
	if _, err := os.Stat(proxy); err != nil {
		t.Fatalf("proxy did not start: %v", err)
	}
	if _, err := os.Stat(complete); !os.IsNotExist(err) {
		t.Fatalf("CodeGraph index completed before proxy returned: err=%v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(complete); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("CodeGraph index did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !strings.Contains(string(got), "init -i\n") || !strings.Contains(string(got), "serve --path "+project+"\n") {
		t.Fatalf("calls = %q; want index and valid proxy launch", got)
	}
}

func TestInjectCodegraphPathPinsProjectRoot(t *testing.T) {
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := tempProjectDir(t)
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

func TestInjectCodegraphPathDoesNotTreatUnrelatedShortFlagsAsPath(t *testing.T) {
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := tempProjectDir(t)
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	for _, flag := range []string{"-progress", "-pretty"} {
		got := injectCodegraphPath([]string{"codegraph", "serve", flag})
		if len(got) != 5 || got[3] != "--path" || got[4] != project {
			t.Fatalf("flag %q suppressed path injection: got %v", flag, got)
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

func TestRunMcpDoesNotSyncExistingCodegraph(t *testing.T) {
	binDir := t.TempDir()
	project := tempProjectDir(t)
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
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if strings.Contains(string(got), "sync\n") || !strings.Contains(string(got), "serve --path "+project+"\n") {
		t.Fatalf("calls = %q; want proxy launch without external sync", got)
	}
}
