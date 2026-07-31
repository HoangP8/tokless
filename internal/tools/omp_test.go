package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func ompTestHome(t *testing.T) {
	t.Helper()
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("TOKLESS_TEST", "1")
}

func TestOmpToolWiring(t *testing.T) {
	ompTestHome(t)
	for _, id := range []string{"caveman", "ponytail", "codegraph", "context-mode", "rtk"} {
		tool := lookupTool(t, id)
		if tool.WireFor["omp"] == nil || tool.UnwireFor["omp"] == nil || tool.VerifyFor["omp"] == nil {
			t.Fatalf("%s missing OMP binding", id)
		}
		ok, err := tool.WireFor["omp"](core.RunOpts{})
		if err != nil || !ok {
			t.Fatalf("wire %s: %v %v", id, ok, err)
		}
	}
	if !agents.OmpMcpHas("codegraph") || !agents.OmpMcpHas("context-mode") || !agents.HasOmpRtkExtension() {
		t.Fatal("OMP wiring missing")
	}
	ext, err := os.ReadFile(filepath.Join(agents.OmpAgentDirResolved(), "extensions", "tokless-rtk.ts"))
	if err != nil || !strings.Contains(string(ext), `event?.toolName !== "bash"`) || !strings.Contains(string(ext), "event.input.command") || !strings.Contains(string(ext), `tool_name: "Bash"`) || !strings.Contains(string(ext), "try {") || !strings.Contains(string(ext), "} catch {") || !strings.Contains(string(ext), `return { input: { ...event.input, command: rewritten } }`) {
		t.Fatalf("bad RTK extension: %v %s", err, ext)
	}
	if strings.Contains(string(ext), "Object.assign(event") || strings.Contains(string(ext), "return { hookSpecificOutput") {
		t.Fatalf("extension uses Claude hook result: %s", ext)
	}
	agentsPath := filepath.Join(agents.OmpAgentDirResolved(), "AGENTS.md")
	body, _ := os.ReadFile(agentsPath)
	if strings.Contains(string(body), "rtk-hook") {
		t.Fatal("RTK instruction leaked into OMP instructions")
	}
	if _, err := rtk.UnwireFor["omp"](core.RunOpts{}); err != nil || agents.HasOmpRtkExtension() {
		t.Fatal("RTK extension not removed")
	}
}

func TestOmpContextModeWireFailsWhenInstructionsConflict(t *testing.T) {
	ompTestHome(t)
	path := filepath.Join(agents.OmpAgentDirResolved(), "AGENTS.md")
	_ = util.EnsureDir(filepath.Dir(path))
	_ = util.WriteFile(path, "# User instructions\n")
	ConfigureInstructionConflicts(false)
	instructionConflict.skipped[path] = true
	t.Cleanup(func() { ConfigureInstructionConflicts(false) })

	ok, err := contextMode.WireFor["omp"](core.RunOpts{})
	if err != nil || ok {
		t.Fatalf("wire: ok=%v err=%v", ok, err)
	}
	if got := contextMode.VerifyFor["omp"](); got == nil || *got {
		t.Fatal("verification must fail without managed instructions")
	}
}

func TestOmpContextModeRepeatWireSucceeds(t *testing.T) {
	ompTestHome(t)
	for i := 0; i < 2; i++ {
		ok, err := contextMode.WireFor["omp"](core.RunOpts{})
		if err != nil || !ok {
			t.Fatalf("wire %d: ok=%v err=%v", i+1, ok, err)
		}
	}
}
