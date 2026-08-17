package main

import "testing"

func TestIsSessionBootArg(t *testing.T) {
	yes := [][]string{
		{"run-mcp", "claude", "codegraph"},
		{"rtk-hook", "codex"},
		{"rtk-hook", "copilot"},
		{"rtk", "hook", "cursor"},
		{"codex-perm", "codex"},
		{"agy-hook", "codegraph-index"},
		{"cursor-hook", "codegraph-index"},
		{"copilot-hook", "codegraph-index"},
		{"cursor-hook", "project-rules"},
	}
	for _, args := range yes {
		if !isSessionBootArg(args) {
			t.Errorf("isSessionBootArg(%v) = false, want true", args)
		}
	}

	no := [][]string{
		{"proxy", "up"},
		{"proxy", "down"},
		{"proxy", "status"},
		{"index"},
		{"doctor"},
		{"init"},
		{"update"},
		{"disable"},
		{"uninstall"},
		{"self-update"},
		{"version"},
		{"help"},
		{},
		{"rtk"},
		{"run-mcp"}, // dispatcher itself requires >=3 args; bare form is not a session boot
	}
	for _, args := range no {
		if isSessionBootArg(args) {
			t.Errorf("isSessionBootArg(%v) = true, want false", args)
		}
	}
}
