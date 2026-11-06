package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestContextModeServerArgs(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	for _, tc := range []struct {
		name, agent, ctx, ws string
		want                 []string
	}{
		{"ctx-agent wins", "claude", "codex", "", []string{"server", "--agent", "codex"}},
		{"old entry falls back to agent", "claude", "", "", []string{"server", "--agent", "claude-code"}},
		{"old entry without agent", "", "", "", []string{"server", "--agent", "gemini"}},
		{"unknown ctx-agent ignored", "", "bogus", "", []string{"server", "--agent", "gemini"}},
		{"vscode with workspace", "", "vscode", "/w/p", []string{"server", "--agent", "vscode", "--workspace-folder", "/w/p"}},
		{"vscode with unexpanded placeholder", "", "vscode", "${workspaceFolder}", []string{"server", "--agent", "gemini"}},
		{"vscode without workspace", "", "vscode", "", []string{"server", "--agent", "gemini"}},
		{"workspace ignored for other ids", "", "codex", "/w/p", []string{"server", "--agent", "codex"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextModeServerArgs(tc.agent, tc.ctx, tc.ws); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// --ctx-agent reaches the upstream server and never starts the headroom proxy.
func TestRunMcpCtxAgentPassesIdWithoutProxy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake node is unix only")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "argv")
	node := filepath.Join(dir, "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\necho \"$@\" > \"$ARGV_OUT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldResolve, oldEnsure := resolveContextMode, ensureProxyUp
	started := false
	ensureProxyUp = func() error { started = true; return nil }
	resolveContextMode = func() (util.CtxRuntime, error) {
		return util.CtxRuntime{Node: node, CLI: "cli.js", Major2: true}, nil
	}
	t.Cleanup(func() { resolveContextMode, ensureProxyUp = oldResolve, oldEnsure })
	t.Setenv("ARGV_OUT", out)
	t.Setenv("TOKLESS_TEST", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	for _, tc := range []struct{ args, want string }{
		{"--context-mode --ctx-agent codex context-mode", "cli.js server --agent codex"},
		{"--context-mode --ctx-agent vscode --workspace /w/p context-mode", "cli.js server --agent vscode --workspace-folder /w/p"},
		{"--context-mode --ctx-agent vscode --workspace ${workspaceFolder} context-mode", "cli.js server --agent gemini"},
		{"--context-mode context-mode", "cli.js server --agent gemini"},
	} {
		if code := RunMcp(strings.Fields(tc.args)); code != 0 {
			t.Fatalf("%s: RunMcp = %d", tc.args, code)
		}
		got, err := os.ReadFile(out)
		if err != nil || strings.TrimSpace(string(got)) != tc.want {
			t.Fatalf("%s: argv = %q (%v), want %q", tc.args, got, err, tc.want)
		}
	}
	if started {
		t.Fatal("--ctx-agent started the headroom proxy")
	}
}
