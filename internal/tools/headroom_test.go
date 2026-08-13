package tools

import (
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func setupHeadroomHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(home)
	ConfigureInstructionConflicts(true)
	t.Cleanup(func() { util.SetHomeOverride(""); agents.SetIdeProjectRoot(""); ConfigureInstructionConflicts(false) })
	return home
}

func TestHeadroomMapsEveryRegisteredAgent(t *testing.T) {
	for _, agent := range core.AgentIDs() {
		if _, ok := headroom.WireFor[agent]; !ok {
			t.Errorf("missing WireFor mapping for registered agent %q", agent)
		}
		if _, ok := headroom.UnwireFor[agent]; !ok {
			t.Errorf("missing UnwireFor mapping for registered agent %q", agent)
		}
		if _, ok := headroom.VerifyFor[agent]; !ok {
			t.Errorf("missing VerifyFor mapping for registered agent %q", agent)
		}
	}
}

func TestHeadroomWiresEverySupportedAgentIdempotently(t *testing.T) {
	setupHeadroomHome(t)
	for _, agent := range []string{"claude", "opencode", "codex", "cursor", "antigravity", "copilot", "droid", "grok", "pi", "omp", "kilo", "cline"} {
		t.Run(agent, func(t *testing.T) {
			for i := 0; i < 2; i++ {
				ok, err := headroom.WireFor[agent](core.RunOpts{})
				if err != nil || !ok || !headroomVerify(agent) {
					t.Fatalf("wire %d = %v, %v; verify=%v", i+1, ok, err, headroomVerify(agent))
				}
			}
			ok, err := headroom.UnwireFor[agent](core.RunOpts{})
			if err != nil || !ok || headroomVerify(agent) {
				t.Fatalf("unwire = %v, %v; verify=%v", ok, err, headroomVerify(agent))
			}
		})
	}
}

func TestHeadroomRefusesDirectUserServer(t *testing.T) {
	setupHeadroomHome(t)
	p := util.ClaudeCodePaths()
	if err := util.WriteFile(p.GlobalJSON, `{"mcpServers":{"headroom":{"type":"stdio","command":"headroom","args":["mcp","serve"]}}}`); err != nil {
		t.Fatal(err)
	}
	ok, err := headroom.WireFor["claude"](core.RunOpts{})
	if err == nil || ok || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("wire direct user server = %v, %v", ok, err)
	}
	raw, _ := util.ReadFileSafe(p.GlobalJSON)
	if !strings.Contains(raw, `"command":"headroom"`) || HasOwner("claude", "headroom") {
		t.Fatalf("user server changed or ownership claimed: %s", raw)
	}
}

func TestHeadroomVerifierRejectsUnboundedServer(t *testing.T) {
	setupHeadroomHome(t)
	if ok, err := headroom.WireFor["claude"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("wire = %v, %v", ok, err)
	}
	p := util.ClaudeCodePaths()
	if err := util.WriteFile(p.GlobalJSON, `{"mcpServers":{"headroom":{"type":"stdio","command":"headroom","args":["mcp","serve"]}}}`); err != nil {
		t.Fatal(err)
	}
	if headroomVerify("claude") {
		t.Fatal("direct headroom server must not verify as Tokless-managed")
	}
	if ok, err := headroom.UnwireFor["claude"](core.RunOpts{}); err != nil || ok {
		t.Fatalf("unwire unbounded server = %v, %v", ok, err)
	}
	raw, _ := util.ReadFileSafe(p.GlobalJSON)
	if !strings.Contains(raw, `"command":"headroom"`) {
		t.Fatalf("unwire removed user server: %s", raw)
	}
}
