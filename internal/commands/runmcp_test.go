//go:build !windows

package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HoangP8/tokless/internal/util"
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

// waitUntil polls cond; background readiness work finishes after RunMcp returns.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

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

func TestRunMcpStartsMcpWithoutWaitingForCodegraphIndex(t *testing.T) {
	binDir := t.TempDir()
	project := tempProjectDir(t)
	log := filepath.Join(binDir, "calls")
	indexing := filepath.Join(project, "indexing")
	complete := filepath.Join(project, "complete")
	mcp := filepath.Join(project, "mcp")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"if [ \"$1\" = status ]; then echo '{\"initialized\":true,\"index\":{\"state\":\"complete\",\"pendingRefs\":0}}'; exit 0; fi\n" +
		"echo \"$*\" >> \"$CODEGRAPH_LOG\"\n" +
		"if [ \"$1\" = init ]; then touch \"$CODEGRAPH_INDEXING\"; sleep 1; mkdir -p .codegraph; touch .codegraph/codegraph.db \"$CODEGRAPH_COMPLETE\"; exit 0; fi\n" +
		"touch \"$CODEGRAPH_MCP\"\n" +
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
	t.Setenv("CODEGRAPH_MCP", mcp)
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
	if _, err := os.Stat(mcp); err != nil {
		t.Fatalf("MCP child did not start: %v", err)
	}
	if fileExists(complete) {
		t.Fatal("MCP start waited for the slow CodeGraph index; handshake must not block on it")
	}
	waitUntil(t, "background CodeGraph index", func() bool { return fileExists(complete) })
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !strings.Contains(string(got), "init\n") || !strings.Contains(string(got), "serve --path "+project+"\n") {
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
	empty, cleanup := createTempOutsideTmpDir(t)
	if err := os.Chdir(empty); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldDir)
		cleanup()
	})

	argv := []string{"codegraph", "serve", "--mcp"}
	got := injectCodegraphPath(argv)
	if len(got) != len(argv) {
		t.Fatalf("argv mutated in non-project dir: got %v, want %v", got, argv)
	}
}

func createTempOutsideTmpDir(t *testing.T) (dir string, cleanup func()) {
	t.Helper()
	candidates := []string{
		filepath.Join(os.TempDir(), "tokless-test-nonproject"),
		filepath.Join(os.Getenv("HOME"), ".cache", "tokless-test-nonproject"),
		filepath.Join(os.Getenv("HOME"), ".local", "share", "tokless-test-nonproject"),
	}
	var root string
	for _, c := range candidates {
		if !isCleanAncestor(c) {
			continue
		}
		if err := os.MkdirAll(c, 0o755); err != nil {
			continue
		}
		root = c
		break
	}
	if root == "" {
		t.Skipf("no clean writable temp base found (ancestor .projectmem/.git pollution or permissions)")
	}
	sub, err := os.MkdirTemp(root, "empty-*")
	if err != nil {
		os.RemoveAll(root)
		t.Fatal(err)
	}
	return sub, func() { os.RemoveAll(root) }
}

func isCleanAncestor(path string) bool {
	dir := filepath.Clean(path)
	for {
		if looksLikeProject(dir) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return true
}

func TestRunMcpDoesNotSyncExistingCodegraph(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	binDir := t.TempDir()
	project := tempProjectDir(t)
	log := filepath.Join(binDir, "calls")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"if [ \"$1\" = status ]; then echo '{\"initialized\":true,\"index\":{\"state\":\"complete\",\"pendingRefs\":0}}'; exit 0; fi\n" +
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
	origEnsureProxyUp := ensureProxyUp
	ensureProxyUp = func() error { return nil }
	t.Cleanup(func() { ensureProxyUp = origEnsureProxyUp })
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

func TestCodegraphWorkspaceResolvesCursorVariable(t *testing.T) {
	project := tempProjectDir(t)
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	if got := codegraphWorkspace("${workspaceFolder}"); got != project {
		t.Fatalf("workspace = %q, want %q", got, project)
	}
	got := injectCodegraphPath([]string{"codegraph", "serve", "--mcp"}, "${workspaceFolder}")
	if !strings.Contains(strings.Join(got, " "), "--path "+project) {
		t.Fatalf("args = %q, want workspace path", got)
	}
}

func TestRunMcpRepairsUnhealthyExistingCodegraphBeforeLaunch(t *testing.T) {
	binDir := t.TempDir()
	project := tempProjectDir(t)
	log := filepath.Join(binDir, "calls")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
		"echo \"$*\" >> \"$CODEGRAPH_LOG\"\n" +
		"if [ \"$1\" = status ]; then exit 1; fi\n" +
		"if [ \"$1\" = index ]; then mkdir -p .codegraph; touch .codegraph/codegraph.db; exit 0; fi\n" +
		"exit 0\n"
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

	if code := RunMcp([]string{"codegraph", "serve"}); code != 0 {
		t.Fatalf("RunMcp = %d", code)
	}
	waitUntil(t, "background index repair", func() bool {
		b, _ := os.ReadFile(log)
		return strings.Contains(string(b), "index --quiet\n")
	})
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "status --json\n") || !strings.Contains(string(got), "index --quiet\n") || !strings.Contains(string(got), "serve --path "+project+"\n") {
		t.Fatalf("calls = %q; want health probe and repair", got)
	}
}

func TestRunMcpRejectsRetiredHeadroomTool(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	bin := filepath.Join(t.TempDir(), "headroom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch \"$RUN_MCP_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUN_MCP_MARKER", marker)
	if code := RunMcp([]string{"--tool", "headroom", "--workspace", t.TempDir(), bin, "mcp", "serve"}); code != 2 {
		t.Fatalf("retired headroom --tool = %d, want 2", code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child ran despite retired headroom --tool: %v", err)
	}
}

func TestRunMcpRejectsMalformedToolBeforeLaunch(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	bin := filepath.Join(t.TempDir(), "child")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch \"$RUN_MCP_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUN_MCP_MARKER", marker)
	if code := RunMcp([]string{"--tool", "--context-mode", bin}); code == 0 {
		t.Fatal("malformed --tool succeeded")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child ran despite malformed --tool: %v", err)
	}
}

func TestRunMcpDoesNotStartProxyBeforeCommandValidation(t *testing.T) {
	started := false
	oldEnsure := ensureProxyUp
	ensureProxyUp = func() error { started = true; return nil }
	t.Cleanup(func() { ensureProxyUp = oldEnsure })
	if code := RunMcp([]string{"--agent", "omp", "command-that-does-not-exist"}); code == 0 {
		t.Fatal("missing MCP command succeeded")
	}
	if started {
		t.Fatal("proxy started before MCP command validation completed")
	}
}

func TestRunMcpStartsChildEvenWhenProxyStartupFails(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "child")
	marker := filepath.Join(t.TempDir(), "started")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch \"$RUN_MCP_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUN_MCP_MARKER", marker)
	oldEnsure := ensureProxyUp
	ensureProxyUp = func() error { return errors.New("proxy unavailable") }
	t.Cleanup(func() { ensureProxyUp = oldEnsure })
	if code := RunMcp([]string{"--agent", "omp", bin}); code != 0 {
		t.Fatalf("RunMcp = %d, want 0 despite proxy startup failure", code)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("MCP child did not run: %v", err)
	}
}

// TestRunMcpCodegraphBootPerAgent pins that a wrapped codegraph MCP boot
// launches the server at once and builds the per-project index beside it,
// for every agent tokless wires through run-mcp.
func TestRunMcpCodegraphBootPerAgent(t *testing.T) {
	agents := []string{"claude", "codex", "opencode", "omp", "kilo", "droid", "antigravity", "grok", "copilot", "cline", "cursor"}
	for _, agent := range agents {
		t.Run(agent, func(t *testing.T) {
			binDir := t.TempDir()
			project := tempProjectDir(t)
			log := filepath.Join(binDir, "calls")
			script := "#!/bin/sh\n" +
				"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n" +
				"if [ \"$1\" = status ]; then echo '{\"initialized\":true,\"index\":{\"state\":\"complete\",\"pendingRefs\":0}}'; exit 0; fi\n" +
				"echo \"$*\" >> \"$CODEGRAPH_LOG\"\n" +
				"if [ \"$1\" = init ]; then mkdir -p .codegraph; touch .codegraph/codegraph.db; exit 0; fi\n" +
				"exit 0\n"
			if err := os.WriteFile(filepath.Join(binDir, "codegraph"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("CODEGRAPH_LOG", log)
			t.Setenv("TOKLESS_TEST", "")
			var proxyCalls atomic.Int32
			oldEnsure := ensureProxyUp
			ensureProxyUp = func() error { proxyCalls.Add(1); return nil }
			t.Cleanup(func() { ensureProxyUp = oldEnsure })
			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(project); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(oldDir) })

			if code := RunMcp([]string{"--agent", agent, "codegraph", "serve", "--mcp"}); code != 0 {
				t.Fatalf("RunMcp --agent %s = %d, want 0", agent, code)
			}
			waitUntil(t, "agent "+agent+" index", func() bool {
				return fileExists(filepath.Join(project, ".codegraph", "codegraph.db"))
			})
			got, err := os.ReadFile(log)
			if err != nil {
				t.Fatalf("agent %s: read calls: %v", agent, err)
			}
			if !strings.Contains(string(got), "serve --mcp --path "+project+"\n") {
				t.Fatalf("agent %s: MCP child not launched with project path: %q", agent, got)
			}
			if agent == "cursor" {
				if n := proxyCalls.Load(); n != 0 {
					t.Fatalf("cursor: proxy started despite manual wiring: %d", n)
				}
				return
			}
			waitUntil(t, "agent "+agent+" proxy startup", func() bool { return proxyCalls.Load() >= 1 })
			if n := proxyCalls.Load(); n != 1 {
				t.Fatalf("agent %s: proxy startups = %d, want 1", agent, n)
			}
		})
	}
}
