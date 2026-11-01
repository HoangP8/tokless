package main

import (
	"reflect"
	"testing"
)

func TestIsSessionBootArg(t *testing.T) {
	yes := [][]string{
		{"run-mcp", "claude", "codegraph"},
		{"rtk-hook", "codex"},
		{"rtk-hook", "copilot"},
		{"grok-hook", "session-start"},
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

func TestParseArgsCapturesPositionals(t *testing.T) {
	cases := []struct {
		argv []string
		cmd  string
		args []string
		no   bool
		flag string
	}{
		{[]string{"proxy", "autostart", "off"}, "proxy", []string{"proxy", "autostart", "off"}, false, ""},
		{[]string{"autostart", "on", "--dry-run"}, "autostart", []string{"autostart", "on"}, false, ""},
		{[]string{"--no-autostart", "init"}, "init", []string{"init"}, true, ""},
		{[]string{"init", "--no-autostart"}, "init", []string{"init"}, true, ""},
		{[]string{"--dry-run", "update"}, "update", []string{"update"}, false, "dry-run"},
		{[]string{"--dry-run", "init"}, "init", []string{"init"}, false, "dry-run"},
	}
	for _, tc := range cases {
		p := parseArgs(tc.argv)
		if p.cmd != tc.cmd || !reflect.DeepEqual(p.args, tc.args) || p.bools["no-autostart"] != tc.no || (tc.flag != "" && !p.bools[tc.flag]) {
			t.Errorf("parseArgs(%v) = cmd %q args %v no-autostart %v", tc.argv, p.cmd, p.args, p.bools["no-autostart"])
		}
	}
}

func TestRunProxyCliAutostartRejectsBadArgs(t *testing.T) {
	for _, argv := range [][]string{{"autostart"}, {"autostart", "maybe"}, {"autostart", "on", "off"}} {
		if got := runProxyCli(argv); got != 2 {
			t.Errorf("runProxyCli(%v) = %d, want 2", argv, got)
		}
	}
}
