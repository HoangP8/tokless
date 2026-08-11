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

func piRtkTestHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("HOME", tmp)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("TOKLESS_TEST", "1")
}

func TestRtkPiWireUnwire(t *testing.T) {
	piRtkTestHome(t)
	wire, unwire := rtk.WireFor["pi"], rtk.UnwireFor["pi"]
	if wire == nil || unwire == nil {
		t.Fatal("missing pi wire/unwire")
	}
	ran, err := wire(core.RunOpts{})
	if err != nil || !ran {
		t.Fatalf("wire: ran=%v err=%v", ran, err)
	}
	if !agents.HasPiRtkExtension() {
		t.Fatal("expected rtk.ts after wire")
	}
	if v := rtk.VerifyFor["pi"]; v == nil || v() == nil || !*v() {
		t.Fatal("verify failed")
	}
	if _, err := unwire(core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if agents.HasPiRtkExtension() {
		t.Fatal("rtk.ts should be gone after unwire")
	}
}

func TestRtkPiDryRun(t *testing.T) {
	piRtkTestHome(t)
	ran, err := rtk.WireFor["pi"](core.RunOpts{DryRun: true})
	if err != nil || !ran {
		t.Fatalf("dry-run: ran=%v err=%v", ran, err)
	}
	if util.Exists(filepath.Join(agents.PiAgentDirResolved(), "extensions", "rtk.ts")) {
		t.Error("dry-run must not write rtk.ts")
	}
}

func TestNormalizePiRtkExtension(t *testing.T) {
	piRtkTestHome(t)
	path := filepath.Join(agents.PiAgentDirResolved(), "extensions", "rtk.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const source = `import type { ExtensionAPI } from "@earendil-works/pi-coding-agent"
import { isToolCallEventType } from "@earendil-works/pi-coding-agent"
function rewrite(pi: ExtensionAPI) {}
export default function (pi: ExtensionAPI) {
  if (!isToolCallEventType("bash", event)) return
      if (cmd.startsWith("rtk ")) return
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if !normalizePiRtkExtension() {
		t.Fatal("normalize failed")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, unwanted := range []string{"pi-coding-agent", "ExtensionAPI", "isToolCallEventType", `cmd.startsWith("rtk ")`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("extension retains %q: %s", unwanted, got)
		}
	}
	if !strings.Contains(got, `event.toolName !== "bash"`) {
		t.Errorf("extension did not use stable bash check: %s", got)
	}
}

func TestRtkPiLiveInit(t *testing.T) {
	if os.Getenv("PI_RTK_LIVE") != "1" {
		t.Skip("set PI_RTK_LIVE=1")
	}
	if util.ResolveRtkBin() == "" {
		t.Skip("rtk missing")
	}
	home := t.TempDir()
	agent := filepath.Join(home, ".pi", "agent")
	_ = os.MkdirAll(filepath.Join(agent, "extensions"), 0o755)
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("HOME", home)
	t.Setenv("PI_CODING_AGENT_DIR", agent)
	t.Setenv("TOKLESS_TEST", "")

	ran, err := rtk.WireFor["pi"](core.RunOpts{})
	if err != nil || !ran {
		t.Fatalf("live wire: ran=%v err=%v", ran, err)
	}
	if !agents.HasPiRtkExtension() {
		t.Fatal("rtk init did not create rtk.ts")
	}
}
