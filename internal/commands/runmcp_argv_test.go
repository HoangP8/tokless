package commands

import (
	"reflect"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestParseRunMcpArgv(t *testing.T) {
	cases := []struct {
		name      string
		argv      []string
		wantAgent string
		wantCtx   bool
		wantRoot  bool
		wantRest  []string
		wantOK    bool
	}{
		{"codegraph", []string{"--agent", "claude", "codegraph", "serve", "--mcp"},
			"claude", false, false, []string{"codegraph", "serve", "--mcp"}, true},
		{"context-mode", []string{"--context-mode", "context-mode"},
			"", true, false, []string{"context-mode"}, true},
		{"projectmem", []string{"--root-cwd", "/venv/bin/python", "-m", "projectmem.mcp_server"},
			"", false, true, []string{"/venv/bin/python", "-m", "projectmem.mcp_server"}, true},
		{"plain", []string{"headroom", "mcp", "serve"},
			"", false, false, []string{"headroom", "mcp", "serve"}, true},
		{"flag without value", []string{"--agent"}, "", false, false, nil, false},
		{"nothing to run", []string{"--root-cwd"}, "", false, false, nil, false},
		{"empty", nil, "", false, false, nil, false},
	}
	for _, c := range cases {
		agent, ctx, root, rest, ok := parseRunMcpArgv(c.argv)
		if ok != c.wantOK || agent != c.wantAgent || ctx != c.wantCtx || root != c.wantRoot ||
			!reflect.DeepEqual(rest, c.wantRest) {
			t.Errorf("%s: parseRunMcpArgv(%v) = (%q,%v,%v,%v,%v), want (%q,%v,%v,%v,%v)",
				c.name, c.argv, agent, ctx, root, rest, ok, c.wantAgent, c.wantCtx, c.wantRoot, c.wantRest, c.wantOK)
		}
	}
}

// Doctor has to see past the wrapper to the real command, or it can't tell
// that a tool was uninstalled behind its back.
func TestUnwrapRunMcpHandlesEverySpawnShape(t *testing.T) {
	for _, toolID := range []string{"codegraph", "context-mode", "headroom", "projectmem"} {
		spawn := util.SpawnForTool("claude", toolID)
		cmd, args := unwrapRunMcp(spawn.Command, spawn.Args)
		if cmd == spawn.Command && len(spawn.Args) > 0 && spawn.Args[0] == "run-mcp" {
			t.Errorf("%s: wrapper not unwrapped, still %q %v", toolID, cmd, args)
		}
		if cmd == "run-mcp" || cmd == "--root-cwd" || cmd == "--context-mode" || cmd == "--agent" {
			t.Errorf("%s: unwrapped to a flag, not a command: %q", toolID, cmd)
		}
	}
}

func TestUnwrapRunMcpLeavesOtherCommandsAlone(t *testing.T) {
	cmd, args := unwrapRunMcp("/usr/bin/headroom", []string{"mcp", "serve"})
	if cmd != "/usr/bin/headroom" || !reflect.DeepEqual(args, []string{"mcp", "serve"}) {
		t.Errorf("unwrapped a non-tokless command: %q %v", cmd, args)
	}
}
