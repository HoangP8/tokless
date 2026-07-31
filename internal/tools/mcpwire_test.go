package tools

import (
	"testing"

	"github.com/HoangP8/tokless/internal/core"
)

// headroom and projectmem share one wiring path, so this covers both them and
// the helpers every future MCP tool will use.
func TestMcpToolsWireAndUnwireOnEveryAgent(t *testing.T) {
	for _, toolID := range []string{"headroom", "projectmem"} {
		for _, agent := range wiredAgents {
			t.Run(toolID+"/"+agent, func(t *testing.T) {
				setupHome(t)
				tm := lookupTool(t, toolID)

				ok, err := tm.WireFor[agent](core.RunOpts{})
				if err != nil || !ok {
					t.Fatalf("wire: ok=%v err=%v", ok, err)
				}
				if !mcpEntryPresent(agent, toolID) {
					t.Error("no MCP server entry written")
				}
				if !HasOwner(agent, toolID) {
					t.Error("no instruction section written")
				}
				if v := tm.VerifyFor[agent](); v == nil || !*v {
					t.Error("verify says not wired right after wiring")
				}

				if _, err := tm.UnwireFor[agent](core.RunOpts{}); err != nil {
					t.Fatalf("unwire: %v", err)
				}
				if mcpEntryPresent(agent, toolID) {
					t.Error("MCP server entry survived unwire")
				}
				if HasOwner(agent, toolID) {
					t.Error("instruction section survived unwire")
				}
			})
		}
	}
}

func TestSkillsWireAndUnwireOnEveryAgent(t *testing.T) {
	for _, toolID := range []string{"principles", "caveman", "ponytail"} {
		for _, agent := range wiredAgents {
			t.Run(toolID+"/"+agent, func(t *testing.T) {
				setupHome(t)
				tm := lookupTool(t, toolID)

				ok, err := tm.WireFor[agent](core.RunOpts{})
				if err != nil || !ok {
					t.Fatalf("wire: ok=%v err=%v", ok, err)
				}
				// A skill is instructions only — it must not add a server entry.
				if mcpEntryPresent(agent, toolID) {
					t.Error("skill wrote an MCP server entry")
				}
				if v := tm.VerifyFor[agent](); v == nil || !*v {
					t.Error("verify says not wired right after wiring")
				}

				if _, err := tm.UnwireFor[agent](core.RunOpts{}); err != nil {
					t.Fatalf("unwire: %v", err)
				}
				if HasOwner(agent, toolID) {
					t.Error("instruction section survived unwire")
				}
			})
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	setupHome(t)
	for _, toolID := range []string{"headroom", "projectmem", "caveman"} {
		tm := lookupTool(t, toolID)
		for _, agent := range wiredAgents {
			if _, err := tm.WireFor[agent](core.RunOpts{DryRun: true}); err != nil {
				t.Fatalf("%s/%s dry run: %v", toolID, agent, err)
			}
			if HasOwner(agent, toolID) || mcpEntryPresent(agent, toolID) {
				t.Errorf("%s/%s dry run wrote config", toolID, agent)
			}
		}
	}
}
