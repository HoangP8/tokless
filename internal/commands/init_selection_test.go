package commands

import (
	"testing"

	"github.com/HoangP8/tokless/internal/core"
)

func TestHasToolForAgent(t *testing.T) {
	tools := []*core.ToolManifest{
		{WireFor: map[string]core.AgentFn{"claude": nil}},
		{WireFor: map[string]core.AgentFn{"codex": nil}},
	}
	if !hasToolForAgent(tools, "claude") {
		t.Fatal("supported agent was excluded")
	}
	if hasToolForAgent(tools, "cursor") {
		t.Fatal("unsupported agent was included")
	}
}
