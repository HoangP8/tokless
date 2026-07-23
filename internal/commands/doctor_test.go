package commands

import (
	"strings"
	"testing"
)

func TestDoctorSummaryLineSeparatesLongestAgentName(t *testing.T) {
	line := doctorSummaryLine(agentReport{label: "GitHub Copilot", installed: true, wired: true})
	if !strings.Contains(line, "GitHub Copilot  all tools wired") {
		t.Fatalf("agent status must have two spaces after longest name: %q", line)
	}
}
