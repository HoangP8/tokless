package tools

import (
	"fmt"
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

func TestRtkOpenCodeWritesOnlyDetectedPlugin(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("PATH", filepath.Join(tmp, "empty"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })
	if _, err := rtkWireOpenCode()(core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if !util.Exists(openCodeRtkPluginPath()) || util.Exists(openCodeRtkV2PluginPath()) {
		t.Fatal("no detected runtime must install v1 fallback only")
	}
	if !rtkOpenCodeV1PluginValid() {
		t.Fatal("v1 fallback plugin should validate")
	}
}

func TestRtkOpenCodeWritesDetectedVersions(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })
	binDir := t.TempDir()
	for name, version := range map[string]string{"opencode": "1.9.0", "opencode2": "2.1.0"} {
		bin := filepath.Join(binDir, name)
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '"+version+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("TOKLESS_TEST", "1")
	if _, err := rtkWireOpenCode()(core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if !util.Exists(openCodeRtkPluginPath()) || !util.Exists(openCodeRtkV2PluginPath()) {
		t.Fatal("mixed install must write both plugins")
	}
	if !rtkOpenCodePluginValid() {
		t.Fatal("both detected plugins should validate")
	}
}

func TestRtkOpenCodeUnparsableV2ProbeDoesNotClaimV1Wired(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	t.Setenv("TOKLESS_TEST_OPENCODE_PROBE", "1")
	t.Cleanup(func() { util.SetHomeOverride("") })
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "opencode2")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'OpenCode version unknown\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if err := os.MkdirAll(filepath.Dir(openCodeRtkPluginPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(openCodeRtkPluginPath(), []byte(fmt.Sprintf(openCodeRtkPlugin, `"tokless"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	if rtkOpenCodePluginValid() {
		t.Fatal("unparsable opencode2 must not validate a v1-only plugin")
	}
	if ok, err := rtkWireOpenCode()(core.RunOpts{}); err != nil || ok {
		t.Fatalf("wire = %v, %v; want false without error", ok, err)
	}
}

func TestRtkOpenCodeWritesV1AndV2Plugins(t *testing.T) {
	tmp := t.TempDir()
	util.SetHomeOverride(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	binDir := t.TempDir()
	for name, version := range map[string]string{"opencode": "1.2.3", "opencode2": "2.1.0"} {
		bin := filepath.Join(binDir, name)
		if err := os.WriteFile(bin, []byte("#!/bin/sh\\nprintf '"+version+"'\\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Cleanup(func() { util.SetHomeOverride("") })
	if ran, err := rtk.WireFor["opencode2"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v2 wire = %v, %v", ran, err)
	}
	if !util.Exists(openCodeRtkV2PluginPath()) {
		t.Fatal("v2 wire did not create v2 plugin")
	}
	if ran, err := rtk.WireFor["opencode"](core.RunOpts{}); err != nil || !ran {
		t.Fatalf("v1 wire = %v, %v", ran, err)
	}
	for path, markers := range map[string][]string{openCodeRtkPluginPath(): {"TOKLESS_BIN", "tool.execute.before"}, openCodeRtkV2PluginPath(): {"export default", "async setup", `ctx.tool.hook("execute.before"`}} {
		raw, ok := util.ReadFileSafe(path)
		if !ok {
			t.Fatalf("missing plugin %s", path)
		}
		for _, marker := range markers {
			if !strings.Contains(raw, marker) {
				t.Errorf("%s missing %q", path, marker)
			}
		}
	}
	if ok, err := rtk.UnwireFor["opencode"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("v1 unwire = %v, %v", ok, err)
	}
	if util.Exists(openCodeRtkPluginPath()) {
		t.Fatal("v1 plugin survived v1 unwire")
	}
	if !util.Exists(openCodeRtkV2PluginPath()) {
		t.Fatal("v1 unwire removed v2 plugin")
	}
	if ok, err := rtk.UnwireFor["opencode2"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("v2 unwire = %v, %v", ok, err)
	}
	if util.Exists(openCodeRtkV2PluginPath()) {
		t.Fatal("v2 plugin survived v2 unwire")
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
  const result = await pi.exec("rtk", ["rewrite", cmd], {
    cwd,
    timeout: REWRITE_TIMEOUT_MS,
    signal,
  })
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
	for _, unwanted := range []string{"pi-coding-agent", "ExtensionAPI", "isToolCallEventType", `cmd.startsWith("rtk ")`, `"rtk", ["rewrite"`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("extension retains %q: %s", unwanted, got)
		}
	}
	if !strings.Contains(got, `event.toolName !== "bash"`) {
		t.Errorf("extension did not use stable bash check: %s", got)
	}
	if strings.Contains(got, "REWRITE_TIMEOUT_MS,") && strings.Index(got, "REWRITE_TIMEOUT_MS") != strings.LastIndex(got, "REWRITE_TIMEOUT_MS") {
		t.Errorf("rewrite exec still carries upstream timeout: %s", got)
	}
	if !strings.Contains(got, `["rtk-rewrite", "--", cmd]`) || !strings.Contains(got, "TOKLESS_BIN") {
		t.Errorf("extension did not delegate to tokless rtk-rewrite: %s", got)
	}
	before := got
	if !normalizePiRtkExtension() {
		t.Fatal("second normalize failed")
	}
	raw2, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw2) != before {
		t.Fatalf("second normalize changed file:\nbefore:\n%s\nafter:\n%s", before, string(raw2))
	}
}

func TestNormalizePiRtkExtensionAnchorMissing(t *testing.T) {
	piRtkTestHome(t)
	path := filepath.Join(agents.PiAgentDirResolved(), "extensions", "rtk.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	source := "export default function (pi) {}\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if normalizePiRtkExtension() {
		t.Fatal("want failure when upstream rewrite anchor is missing")
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
