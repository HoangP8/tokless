package util

import (
	"slices"
	"testing"
)

func TestContextModeAgentFor(t *testing.T) {
	for harness, want := range map[string]string{
		"claude": "claude-code", "codex": "codex", "copilot": "copilot-cli",
		"copilot-ide": "vscode", "cursor": "vscode", "zcode": "zcode",
		"opencode": "opencode", "opencode2": "opencode",
		// no upstream id: resolve the project from the spawn dir
		"antigravity": "gemini", "droid": "gemini", "grok": "gemini", "pi": "gemini",
		"omp": "gemini", "kilo": "gemini", "cline": "gemini", "amp": "gemini",
		"kimi": "gemini", "cmd": "gemini", "dsh": "gemini", "cursor-wsl": "gemini", "": "gemini",
	} {
		if got := ContextModeAgentFor(harness); got != want {
			t.Errorf("%q: got %q want %q", harness, got, want)
		}
		if !slices.Contains(ContextModeAgentIDs, ContextModeAgentFor(harness)) {
			t.Errorf("%q maps to an id upstream does not know", harness)
		}
	}
}

func TestContextModeHostFlags(t *testing.T) {
	if got := ContextModeHostFlags("codex"); !slices.Equal(got, []string{"--ctx-agent", "codex"}) {
		t.Fatalf("codex flags = %v", got)
	}
	// IDE hosts also pass the workspace placeholder; others never do.
	want := []string{"--ctx-agent", "vscode", "--workspace", "${workspaceFolder}"}
	for _, h := range []string{"cursor", "copilot-ide"} {
		if got := ContextModeHostFlags(h); !slices.Equal(got, want) {
			t.Fatalf("%s flags = %v", h, got)
		}
	}
	for _, h := range []string{"cursor-wsl", "pi", "droid", "omp", "antigravity", "grok", ""} {
		if got := ContextModeHostFlags(h); got != nil {
			t.Fatalf("%q maps to gemini and must carry no flags: %v", h, got)
		}
	}
}

func TestContextModeArgsSplitStripCurrent(t *testing.T) {
	cur := append([]string{"run-mcp", "--context-mode"}, append(ContextModeHostFlags("cursor"), "/bin/context-mode")...)
	flags, target, ok := SplitContextModeArgs(cur)
	if !ok || len(flags) != 4 || !slices.Equal(target, []string{"/bin/context-mode"}) {
		t.Fatalf("split = %v %v %v", flags, target, ok)
	}
	old := []string{"run-mcp", "--context-mode", "/bin/context-mode"}
	if got := StripContextModeFlags(cur); !slices.Equal(got, old) {
		t.Fatalf("strip = %v", got)
	}
	plain := []string{"run-mcp", "--context-mode", "/bin/context-mode"}
	if !ContextModeFlagsCurrent("droid", plain) || ContextModeFlagsCurrent("droid", cur) {
		t.Fatal("gemini-mapped harness is current only without flags")
	}
	if !ContextModeFlagsCurrent("cursor", cur) || ContextModeFlagsCurrent("claude", cur) || ContextModeFlagsCurrent("cursor", old) {
		t.Fatal("ContextModeFlagsCurrent must demand the exact flags for the harness")
	}
	if !SameModuloContextModeFlags(append([]string{"tokless"}, cur...), append([]string{"tokless"}, old...)) {
		t.Fatal("old and new shapes must compare equal modulo host flags")
	}
}

func TestContextModeSpawnForKeepsTarget(t *testing.T) {
	spawn := ContextModeSpawnFor("codex")
	if len(spawn.Args) < 5 || spawn.Args[0] != "run-mcp" || spawn.Args[1] != "--context-mode" || spawn.Args[2] != CtxAgentFlag || spawn.Args[3] != "codex" {
		t.Fatalf("spawn = %v", spawn.Args)
	}
	if got := McpSpawnForAgent("codex", "projectmem"); slices.Contains(got.Args, CtxAgentFlag) {
		t.Fatalf("non context-mode tool got host flags: %v", got.Args)
	}
}
