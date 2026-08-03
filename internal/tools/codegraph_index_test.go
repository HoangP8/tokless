//go:build !windows

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
)

func writeCodegraphIndexScript(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"echo \"$*\" >> \"$CODEGRAPH_LOG\"\n" + body + "\n"
	bin := filepath.Join(dir, "codegraph")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEGRAPH_LOG", log)
	t.Setenv("TOKLESS_TEST", "")
	return dir, log
}

func TestRunCodegraphIndexInitializesBeforeReturn(t *testing.T) {
	_, log := writeCodegraphIndexScript(t, "[ \"$1\" = init ] || exit 1\nmkdir -p .codegraph\ntouch .codegraph/codegraph.db")
	project := t.TempDir()

	ok, err := RunCodegraphIndex(project, core.RunOpts{})
	if err != nil || !ok {
		t.Fatalf("RunCodegraphIndex: ok=%v err=%v", ok, err)
	}
	if !codegraphLogHas(t, log, "init -i") || !dirExists(filepath.Join(project, ".codegraph")) {
		t.Fatalf("init did not complete before return: %q", readCodegraphLog(t, log))
	}
}

func TestRunCodegraphIndexIgnoresHostileCodegraphDir(t *testing.T) {
	_, log := writeCodegraphIndexScript(t, "[ \"$1\" = init ] || exit 1\n[ -z \"$CODEGRAPH_DIR\" ] || exit 2\nmkdir -p .codegraph\ntouch .codegraph/codegraph.db")
	t.Setenv("CODEGRAPH_DIR", ".custom-codegraph")
	project := t.TempDir()
	ok, err := RunCodegraphIndex(project, core.RunOpts{})
	if err != nil || !ok {
		t.Fatalf("RunCodegraphIndex: ok=%v err=%v", ok, err)
	}
	if !dirExists(filepath.Join(project, ".codegraph")) || dirExists(filepath.Join(project, ".custom-codegraph")) {
		t.Fatalf("wrong index directory: log=%q", readCodegraphLog(t, log))
	}
}

func TestRunCodegraphIndexSyncsExistingIndex(t *testing.T) {
	_, log := writeCodegraphIndexScript(t, "[ \"$1\" = sync ] || exit 1")
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".codegraph", "codegraph.db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := RunCodegraphIndex(project, core.RunOpts{})
	if err != nil || !ok {
		t.Fatalf("RunCodegraphIndex: ok=%v err=%v", ok, err)
	}
	if got := readCodegraphLog(t, log); got != "sync\n" {
		t.Fatalf("calls = %q, want sync only", got)
	}
}

func TestRunCodegraphIndexFallsBackToPlainInit(t *testing.T) {
	_, log := writeCodegraphIndexScript(t, "[ \"$1\" = init ] && [ \"$2\" != -i ] || exit 1\nmkdir -p .codegraph\ntouch .codegraph/codegraph.db")

	ok, err := RunCodegraphIndex(t.TempDir(), core.RunOpts{})
	if err != nil || !ok {
		t.Fatalf("RunCodegraphIndex: ok=%v err=%v", ok, err)
	}
	if got := readCodegraphLog(t, log); got != "init -i\ninit\n" {
		t.Fatalf("calls = %q, want init fallback", got)
	}
}

func TestRunCodegraphIndexInitializesEmptyIndexDir(t *testing.T) {
	_, log := writeCodegraphIndexScript(t, "[ \"$1\" = init ] || exit 1\ntouch .codegraph/codegraph.db")
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, err := RunCodegraphIndex(project, core.RunOpts{}); err != nil || !ok {
		t.Fatalf("RunCodegraphIndex: ok=%v err=%v", ok, err)
	}
	if got := readCodegraphLog(t, log); got != "init -i\n" {
		t.Fatalf("calls = %q, want init only", got)
	}
}

func TestHasCodegraphIndexHonorsOverride(t *testing.T) {
	project := t.TempDir()
	indexDir := filepath.Join(project, ".custom-codegraph")
	if err := os.Mkdir(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "codegraph.db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEGRAPH_DIR", ".custom-codegraph")
	if !HasCodegraphIndex(project) {
		t.Fatal("CODEGRAPH_DIR index not detected")
	}
}

func TestHasCodegraphIndexIgnoresInvalidOverride(t *testing.T) {
	project := t.TempDir()
	indexDir := filepath.Join(project, ".codegraph")
	if err := os.Mkdir(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "codegraph.db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{".", "..", ".codegraph..old", "nested/index", `nested\index`, "/absolute"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("CODEGRAPH_DIR", value)
			if !HasCodegraphIndex(project) {
				t.Fatalf("invalid CODEGRAPH_DIR %q did not fall back", value)
			}
		})
	}
}

func TestRunCodegraphIndexReturnsFailure(t *testing.T) {
	_, _ = writeCodegraphIndexScript(t, "echo failure >&2\nexit 1")

	ok, err := RunCodegraphIndex(t.TempDir(), core.RunOpts{})
	if ok || err == nil || !strings.Contains(err.Error(), "init failed") || !strings.Contains(err.Error(), "failure") {
		t.Fatalf("RunCodegraphIndex: ok=%v err=%v", ok, err)
	}
}

func readCodegraphLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func codegraphLogHas(t *testing.T, path, want string) bool {
	t.Helper()
	return strings.Contains(readCodegraphLog(t, path), want+"\n")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
