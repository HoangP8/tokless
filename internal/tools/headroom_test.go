package tools

import (
	"path/filepath"
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
	// codex/opencode/omp proxy wiring requires an existing config file (the
	// agents package refuses to create absent files); seed minimal configs.
	if err := util.WriteFile(util.CodexPathsResolved().Config, ""); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(util.OpenCodePathsResolved().Config, `{"$schema": "https://opencode.ai/config.json", "theme": "dark"}
`); err != nil {
		t.Fatal(err)
	}
	ompModels := filepath.Join(agents.OmpAgentDirResolved(), "models.yml")
	if ompModels != "" {
		if err := util.WriteFile(ompModels, "models:\n  claude-sonnet:\n    id: claude-sonnet\n"); err != nil {
			t.Fatal(err)
		}
	}
	for _, agent := range []string{"claude", "codex", "opencode", "omp", "kilo", "pi", "droid", "antigravity"} {
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

// Manual agents have no config tokless writes; wiring is a no-op and verify
// always passes (nothing observable to check).
func TestHeadroomManualAgentsAreNoOps(t *testing.T) {
	setupHeadroomHome(t)
	for _, agent := range []string{"grok", "copilot", "cline", "cursor"} {
		ok, err := headroom.WireFor[agent](core.RunOpts{})
		if err != nil || !ok {
			t.Fatalf("wire %s = %v, %v", agent, ok, err)
		}
		if !headroomVerify(agent) {
			t.Fatalf("verify %s = false", agent)
		}
		ok, err = headroom.UnwireFor[agent](core.RunOpts{})
		if err != nil || ok {
			t.Fatalf("manual unwire %s = %v, %v (want false,no-op)", agent, ok, err)
		}
	}
}

func TestHeadroomRefusesDirectUserServer(t *testing.T) {
	setupHeadroomHome(t)
	p := util.ClaudeCodePaths()
	if err := util.WriteFile(p.Settings, `{"env":{"ANTHROPIC_BASE_URL":"http://user.example:9999"}}`); err != nil {
		t.Fatal(err)
	}
	ok, err := headroom.WireFor["claude"](core.RunOpts{})
	if err == nil || ok || !strings.Contains(err.Error(), "differing existing config value") {
		t.Fatalf("wire direct user value = %v, %v", ok, err)
	}
	if agents.ClaudeProxyWired() {
		t.Fatalf("user ANTHROPIC_BASE_URL must not be claimed as wired")
	}
	raw, _ := util.ReadFileSafe(p.Settings)
	if !strings.Contains(raw, "http://user.example:9999") {
		t.Fatalf("user value clobbered: %s", raw)
	}
}

func TestHeadroomVerifierRejectsForeignEndpoint(t *testing.T) {
	setupHeadroomHome(t)
	if ok, err := headroom.WireFor["claude"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("wire = %v, %v", ok, err)
	}
	p := util.ClaudeCodePaths()
	if err := util.WriteFile(p.Settings, `{"env":{"ANTHROPIC_BASE_URL":"http://user.example:9999"}}`); err != nil {
		t.Fatal(err)
	}
	if headroomVerify("claude") {
		t.Fatal("foreign endpoint must not verify as Tokless-managed")
	}
	if ok, err := headroom.UnwireFor["claude"](core.RunOpts{}); err != nil || ok {
		t.Fatalf("unwire foreign endpoint = %v, %v", ok, err)
	}
	raw, _ := util.ReadFileSafe(p.Settings)
	if !strings.Contains(raw, "http://user.example:9999") {
		t.Fatalf("unwire removed user value: %s", raw)
	}
}