package main

import (
	"github.com/HoangP8/tokless/internal/util"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpListsPonytailTool(t *testing.T) {
	help := helpText()
	if !strings.Contains(help, "ponytail") {
		t.Fatalf("help missing ponytail:\n%s", help)
	}
	if strings.Contains(help, "upgrade the 4 tools") {
		t.Fatalf("help still says 4 tools:\n%s", help)
	}
	if strings.Contains(help, "principles") {
		t.Fatalf("help still lists principles:\n%s", help)
	}
}

func TestParseArgsRejectsEmptyAgentAndToolLists(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "bare agents", args: []string{"--agents"}},
		{name: "empty agents", args: []string{"--agents="}},
		{name: "bare tools", args: []string{"--tools"}},
		{name: "empty tools", args: []string{"--tools="}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			oldArgs := os.Args
			os.Args = append([]string{"tokless"}, tt.args...)
			defer func() { os.Args = oldArgs }()
			if code := run(); code != 2 {
				t.Fatalf("run exit = %d, want 2", code)
			}
		})
	}
}

func TestParseListAcceptsNonemptyLists(t *testing.T) {
	agents, err := parseList("claude,opencode", true, []string{"claude", "opencode"})
	if err != nil || len(agents) != 2 {
		t.Fatalf("valid agents list rejected: %v", err)
	}
	tools, err := parseList("rtk", true, []string{"rtk"})
	if err != nil || len(tools) != 1 {
		t.Fatalf("valid tools list rejected: %v", err)
	}
}

func TestHelpWinsOverIncompleteListFlag(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"tokless", "--help", "--agents"}
	t.Cleanup(func() { os.Args = oldArgs })
	captureStdout(t, run)
}

func TestInstallerRunImpliesUpgrade(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Setenv("TOKLESS_TEST_LATEST", "0.1.0")
	t.Setenv("TOKLESS_INSTALLER_RUN", "1")
	util.SetHomeOverride(tmp)
	t.Cleanup(func() { util.SetHomeOverride("") })
	oldVersion := util.Version
	util.Version = "0.0.0"
	t.Cleanup(func() { util.Version = oldVersion })
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Installer run should skip self-update entirely because the installer just
	// downloaded the latest binary.
	out := captureStdout(t, func() int {
		oldArgs := os.Args
		os.Args = []string{"tokless", "--tools", "rtk", "--agents", "claude"}
		defer func() { os.Args = oldArgs }()
		return run()
	})
	if strings.Contains(out, "tokless v0.0.0 → v0.1.0 updated") || strings.Contains(out, "tokless updating") {
		t.Fatalf("installer run should not self-update after installer downloaded latest binary:\n%s", out)
	}
	if strings.Contains(out, "global token-saver") {
		t.Fatalf("installer run with Upgrade=true still skipped legacy rtk install:\n%s", out)
	}
}

func TestDefaultRunChecksAndUpdatesToklessInTestMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Setenv("TOKLESS_TEST_LATEST", "0.1.0")
	util.SetHomeOverride(tmp)
	t.Cleanup(func() { util.SetHomeOverride("") })
	oldVersion := util.Version
	util.Version = "0.0.0"
	t.Cleanup(func() { util.Version = oldVersion })
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() int {
		oldArgs := os.Args
		os.Args = []string{"tokless", "--tools", "rtk", "--agents", "claude"}
		defer func() { os.Args = oldArgs }()
		return run()
	})
	for _, want := range []string{"Tools", "RTK", "Agents", "Claude Code", "rtk", "Tokless", "tokless v0.0.0 → v0.1.0 updated"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	for _, nope := range []string{"global token-saver", "updating tokless", "update?", "Principles"} {
		if strings.Contains(out, nope) {
			t.Fatalf("output contains %q:\n%s", nope, out)
		}
	}
	if strings.Index(out, "Tokless") > strings.Index(out, "Tools") {
		t.Fatalf("self-update did not run before tool flow:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func() int) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	code := fn()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	r.Close()
	if code != 0 {
		t.Fatalf("run exit %d:\n%s", code, out)
	}
	return string(out)
}
