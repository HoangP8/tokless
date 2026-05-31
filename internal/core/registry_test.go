package core

import (
	"testing"
)

func TestRegisterAndList(t *testing.T) {
	// registry is global package state; register with unique ids unlikely to collide.
	a1 := &AgentManifest{
		ID:    "test_agent_1",
		Label: "Test Agent 1",
		Detect: func() Detection {
			return Detection{Installed: true, Source: "config"}
		},
	}
	a2 := &AgentManifest{
		ID:    "test_agent_2",
		Label: "Test Agent 2",
		Detect: func() Detection {
			return Detection{Installed: false, Source: ""}
		},
	}

	t1 := &ToolManifest{
		ID:    "test_tool_1",
		Label: "Test Tool 1",
	}
	t2 := &ToolManifest{
		ID:    "test_tool_2",
		Label: "Test Tool 2",
	}

	RegisterAgent(a1)
	RegisterAgent(a2)
	RegisterTool(t1)
	RegisterTool(t2)

	// Test GetAgent / GetTool
	if got := GetAgent("test_agent_1"); got != a1 {
		t.Errorf("expected %v, got %v", a1, got)
	}
	if got := GetAgent("unknown_agent"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
	if got := GetTool("test_tool_1"); got != t1 {
		t.Errorf("expected %v, got %v", t1, got)
	}
	if got := GetTool("unknown_tool"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	// Verify order in AgentIDs and ToolIDs
	agentIDs := AgentIDs()
	toolIDs := ToolIDs()

	// Verify that the registered items are in the lists and preserve order relative to each other
	// (since other items might already be registered)
	checkOrder := func(ids []string, id1, id2 string) {
		idx1, idx2 := -1, -1
		for i, id := range ids {
			if id == id1 {
				idx1 = i
			}
			if id == id2 {
				idx2 = i
			}
		}
		if idx1 == -1 || idx2 == -1 {
			t.Fatalf("expected both %s and %s in ids list: %v", id1, id2, ids)
		}
		if idx1 >= idx2 {
			t.Errorf("expected %s before %s in list, but indices were %d and %d", id1, id2, idx1, idx2)
		}
	}

	checkOrder(agentIDs, "test_agent_1", "test_agent_2")
	checkOrder(toolIDs, "test_tool_1", "test_tool_2")
}
