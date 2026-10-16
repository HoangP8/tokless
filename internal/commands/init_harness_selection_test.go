package commands_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/commands"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/tools"
	"github.com/HoangP8/tokless/internal/util"
)

// seedAgentInstalled makes an agent detectable in the test home.
func seedAgentInstalled(t *testing.T, agentID string) {
	t.Helper()
	agent := core.GetAgent(agentID)
	if agent == nil {
		t.Fatalf("agent %q not registered", agentID)
	}
	if agent.ConfigDir != nil {
		if err := os.MkdirAll(agent.ConfigDir(), 0o755); err != nil {
			t.Fatalf("seed %s config dir: %v", agentID, err)
		}
	}
	if agentID == "cline" || agentID == "opencode" || agentID == "opencode2" {
		binDir := t.TempDir()
		bin := filepath.Join(binDir, agent.CLIBin)
		script := "#!/bin/sh\nexit 0\n"
		if agentID == "opencode" {
			script = "#!/bin/sh\necho opencode 1.2.3\n"
		} else if agentID == "opencode2" {
			script = "#!/bin/sh\necho opencode 2.0.6\n"
		}
		if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
			t.Fatalf("seed %s binary: %v", agentID, err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
}

func TestInitSelectedInstructionOnlyToolUpdatesAgentInstructions(t *testing.T) {
	agents.Register()
	tools.Register()
	home := t.TempDir()
	t.Setenv("TOKLESS_TEST", "1")
	t.Setenv("TOKLESS_INSTALLER_RUN", "1")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	seedAgentInstalled(t, "claude")

	if code := commands.RunInit(commands.InitOptions{Agents: []string{"claude"}, Tools: []string{"principles"}}); code != 0 {
		t.Fatalf("RunInit returned %d", code)
	}

	instructions, err := os.ReadFile(util.ClaudeCodePaths().Instructions)
	if err != nil {
		t.Fatal(err)
	}
	body := string(instructions)
	if !strings.Contains(body, "## Principles") {
		t.Fatal("selected principles section missing from Claude instructions")
	}
	if strings.Contains(body, "## Response Style (caveman)") {
		t.Fatal("excluded caveman section present in Claude instructions")
	}
}

func TestInitSelectedToolsAcrossRegisteredAgents(t *testing.T) {
	agents.Register()
	tools.Register()

	allAgents := core.ListAgents()
	if len(allAgents) == 0 {
		t.Fatal("no registered harnesses")
	}
	seen := make(map[string]bool, len(allAgents))
	for _, agent := range allAgents {
		if seen[agent.ID] {
			t.Fatalf("duplicate registered harness %q", agent.ID)
		}
		seen[agent.ID] = true
	}

	selectedCandidates := []string{"codegraph", "context-mode", "ponytail", "caveman"}
	excludedCandidates := []string{"context-mode", "ponytail", "caveman", "codegraph"}
	var covered []string

	for _, agent := range allAgents {
		agent := agent
		t.Run(agent.ID, func(t *testing.T) {
			var selected, excluded *core.ToolManifest
			for i, id := range selectedCandidates {
				candidate := core.GetTool(id)
				if candidate == nil || candidate.WireFor[agent.ID] == nil || candidate.VerifyFor[agent.ID] == nil {
					continue
				}
				other := core.GetTool(excludedCandidates[i])
				if other != nil && other.ID != candidate.ID && other.WireFor[agent.ID] != nil && other.VerifyFor[agent.ID] != nil {
					selected, excluded = candidate, other
					break
				}
			}
			if selected == nil {
				t.Skipf("no supported selected/excluded tool pair for %q", agent.ID)
			}

			home := t.TempDir()
			t.Setenv("TOKLESS_TEST", "1")
			t.Setenv("TOKLESS_INSTALLER_RUN", "1")
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			util.SetHomeOverride(home)
			t.Cleanup(func() { util.SetHomeOverride("") })
			seedAgentInstalled(t, agent.ID)

			if code := commands.RunInit(commands.InitOptions{Agents: []string{agent.ID}, Tools: []string{selected.ID}}); code != 0 {
				t.Fatalf("RunInit returned %d for selected tool %q on %q", code, selected.ID, agent.ID)
			}
			if wired := selected.VerifyFor[agent.ID](); wired == nil || !*wired {
				t.Errorf("selected tool %q not wired for %q", selected.ID, agent.ID)
			}
			if state := excluded.VerifyFor[agent.ID](); state != nil && *state {
				t.Errorf("excluded tool %q is wired for %q", excluded.ID, agent.ID)
			}
			covered = append(covered, agent.ID)
		})
	}
	t.Logf("covered %d registered harnesses: %v", len(covered), covered)
}
