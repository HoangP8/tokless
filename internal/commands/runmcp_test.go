//go:build !windows

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// One global config entry serves every project because tokless appends the
// project path at launch.
func TestRunMcpRootCwdAppendsProjectPath(t *testing.T) {
	binDir := t.TempDir()
	project := t.TempDir()
	log := filepath.Join(binDir, "args")
	server := filepath.Join(binDir, "fake-python")
	script := "#!/bin/sh\necho \"$*\" > \"$FAKE_LOG\"\n"
	if err := os.WriteFile(server, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", log)
	t.Setenv("TOKLESS_TEST", "")
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	RunMcp([]string{"--root-cwd", server, "-m", "projectmem.mcp_server"})

	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("server never ran: %v", err)
	}
	// Getwd resolves symlinks (macOS /var), so compare against that.
	cwd, _ := os.Getwd()
	want := "-m projectmem.mcp_server --root " + cwd + "\n"
	if string(got) != want {
		t.Errorf("server args = %q, want %q", got, want)
	}
}

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
	if err != nil || string(got) != "init -i\nserve\n" {
		t.Fatalf("calls = %q, err = %v", got, err)
	}
}
