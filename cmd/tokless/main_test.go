package main

import (
	"testing"
)

func TestParseArgs_Command(t *testing.T) {
	p := parseArgs([]string{"doctor"})
	if p.cmd != "doctor" {
		t.Errorf("cmd = %q, want %q", p.cmd, "doctor")
	}
}

func TestParseArgs_EmptyDefaultsInit(t *testing.T) {
	p := parseArgs([]string{})
	if p.cmd != "" {
		t.Errorf("cmd = %q, want empty", p.cmd)
	}
}

func TestParseArgs_BoolFlag(t *testing.T) {
	p := parseArgs([]string{"--verbose", "--dry-run"})
	if !p.bools["verbose"] {
		t.Error("expected verbose=true")
	}
	if !p.bools["dry-run"] {
		t.Error("expected dry-run=true")
	}
}

func TestParseArgs_ShortBool(t *testing.T) {
	p := parseArgs([]string{"-v"})
	if !p.bools["v"] {
		t.Error("expected v=true")
	}
}

func TestParseArgs_FlagWithEquals(t *testing.T) {
	p := parseArgs([]string{"--agents=claude,opencode"})
	if p.flags["agents"] != "claude,opencode" {
		t.Errorf("agents = %q, want %q", p.flags["agents"], "claude,opencode")
	}
}

func TestParseArgs_FlagWithSpace(t *testing.T) {
	p := parseArgs([]string{"--agents", "claude,opencode"})
	if p.flags["agents"] != "claude,opencode" {
		t.Errorf("agents = %q, want %q", p.flags["agents"], "claude,opencode")
	}
}

func TestParseArgs_CommandWithFlags(t *testing.T) {
	p := parseArgs([]string{"update", "--verbose", "--agents", "claude"})
	if p.cmd != "update" {
		t.Errorf("cmd = %q, want %q", p.cmd, "update")
	}
	if !p.bools["verbose"] {
		t.Error("expected verbose=true")
	}
	if p.flags["agents"] != "claude" {
		t.Errorf("agents = %q, want %q", p.flags["agents"], "claude")
	}
}

func TestParseList_Valid(t *testing.T) {
	allowed := []string{"claude", "opencode", "codex"}
	items, err := parseList("claude,opencode", true, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0] != "claude" || items[1] != "opencode" {
		t.Errorf("items = %v, want [claude opencode]", items)
	}
}

func TestParseList_Invalid(t *testing.T) {
	allowed := []string{"claude", "opencode", "codex"}
	_, err := parseList("claude,invalid", true, allowed)
	if err == nil {
		t.Error("expected error for invalid value")
	}
}

func TestParseList_NotProvided(t *testing.T) {
	items, err := parseList("", false, []string{"claude"})
	if err != nil || items != nil {
		t.Errorf("expected nil,nil; got %v,%v", items, err)
	}
}

func TestParseList_TrimWhitespace(t *testing.T) {
	allowed := []string{"claude", "opencode"}
	items, err := parseList(" claude , opencode ", true, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("items = %v, want 2 items", items)
	}
}
