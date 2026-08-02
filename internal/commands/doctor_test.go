package commands

import (
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
)

func TestDoctorSummaryLineSeparatesLongestAgentName(t *testing.T) {
	line := doctorSummaryLine(agentReport{label: "GitHub Copilot", installed: true, wired: true})
	if !strings.Contains(line, "GitHub Copilot  all tools wired") {
		t.Fatalf("agent status must have two spaces after longest name: %q", line)
	}
}

func TestDoctorMissingToolsExcludesInstructionOnly(t *testing.T) {
	missing := doctorMissingTools([]*core.ToolManifest{
		{
			Label:           "Caveman",
			InstructionOnly: true,
			VerifyFor: map[string]core.VerifyFn{
				"agent": func() *bool { return core.BoolPtr(false) },
			},
		},
		{
			Label: "RTK",
			VerifyFor: map[string]core.VerifyFn{
				"agent": func() *bool { return core.BoolPtr(false) },
			},
		},
	}, "agent")
	if len(missing) != 1 || missing[0] != "RTK" {
		t.Fatalf("missing = %v", missing)
	}
}
