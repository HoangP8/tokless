package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func kiloToolProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return root
}

func TestKiloContextAndCodegraphWireVerify(t *testing.T) {
	kiloToolProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "global"))
	t.Setenv("TOKLESS_TEST", "1")
	binDir := t.TempDir()
	codegraphBin := filepath.Join(binDir, "codegraph")
	if err := os.WriteFile(codegraphBin, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo codegraph 1.0; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	ctx := contextMode.WireFor["kilo"]
	if ok, err := ctx(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("context Kilo wire = %v, %v", ok, err)
	}
	globalConfig, _ := util.ReadFileSafe(util.KiloPathsResolved().Config)
	if !agents.KiloInstructionsReferenceReady() || !strings.Contains(globalConfig, agents.KiloInstructionsPath()) {
		t.Fatalf("Kilo instructions reference missing: %s", globalConfig)
	}
	spawn := util.PickMcpSpawn("context-mode")
	if !agents.KiloMcpMatches("context-mode", append([]string{spawn.Command}, spawn.Args...)) || !kiloHasOwner("context-mode") {
		t.Fatal("context Kilo verify failed")
	}
	cg := codegraph.WireFor["kilo"]
	if ok, err := cg(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("codegraph Kilo wire = %v, %v", ok, err)
	}
	expectedCG := util.WrapAutoIndex("kilo", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	if !codegraphVerify("kilo") || !agents.KiloMcpMatches("codegraph", append([]string{expectedCG.Command}, expectedCG.Args...)) {
		t.Fatal("codegraph Kilo exact verify failed")
	}
	instructions, err := os.ReadFile(agents.KiloInstructionsPath())
	if err != nil || strings.Contains(string(instructions), "run-mcp") {
		t.Fatal("MCP command leaked into instructions")
	}
}

func TestKiloContextVerifyRejectsMutatedValidEntry(t *testing.T) {
	kiloToolProject(t)
	t.Setenv("TOKLESS_TEST", "1")
	binDir := t.TempDir()
	rtk := filepath.Join(binDir, "rtk")
	if err := os.WriteFile(rtk, []byte("#!/bin/sh\necho rtk 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if ok, err := contextMode.WireFor["kilo"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Kilo context wire = %v, %v", ok, err)
	}
	verify := contextMode.VerifyFor["kilo"]
	if result := verify(); result == nil || !*result {
		t.Fatal("valid Kilo context entry did not verify")
	}
	if changed, _ := agents.ConfigureKiloMcp("context-mode", []string{"other-server", "--valid"}); !changed {
		t.Fatal("failed to mutate Kilo context entry")
	}
	if result := verify(); result == nil || *result {
		t.Fatal("mutated Kilo context entry verified")
	}
}

func TestKiloStrictMCPVerificationRejectsMalformedCommands(t *testing.T) {
	kiloToolProject(t)
	spawn := util.McpSpawn{Command: "tokless", Args: []string{"run-mcp", "--context-mode", "context-mode"}}
	if _, _ = agents.ConfigureKiloMcp("context-mode", append([]string{spawn.Command}, spawn.Args...)); !agents.KiloMcpMatches("context-mode", append([]string{spawn.Command}, spawn.Args...)) {
		t.Fatal("proper Kilo context command rejected")
	}
	if _, _ = agents.ConfigureKiloMcp("context-mode", []string{"tokless", "run-mcp", "--context-mode", "nested", "run-mcp", "--context-mode", "context-mode"}); agents.KiloMcpMatches("context-mode", append([]string{spawn.Command}, spawn.Args...)) {
		t.Fatal("double-wrapped Kilo context command accepted")
	}
	if _, _ = agents.ConfigureKiloMcp("context-mode", []string{"wrong", "command"}); agents.KiloMcpMatches("context-mode", append([]string{spawn.Command}, spawn.Args...)) {
		t.Fatal("wrong Kilo context command accepted")
	}
}

func TestKiloCodegraphUnwireRemovesManagedReference(t *testing.T) {
	kiloToolProject(t)
	_ = util.WriteFile(util.KiloPathsResolved().Config, `{"instructions":["user.md"]}`)
	_ = util.WriteFile(agents.KiloInstructionsPath(), "## Code Index (codegraph)\nmanaged\n")
	_ = kiloWriteOwner("codegraph")
	agents.SyncKiloInstructionsReference()
	globalConfig, _ := util.ReadFileSafe(util.KiloPathsResolved().Config)
	if !strings.Contains(globalConfig, agents.KiloInstructionsPath()) {
		t.Fatal("managed Kilo reference missing")
	}
	if ok, err := codegraph.UnwireFor["kilo"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Kilo codegraph unwire = %v, %v", ok, err)
	}
	globalConfig, _ = util.ReadFileSafe(util.KiloPathsResolved().Config)
	if strings.Contains(globalConfig, agents.KiloInstructionsPath()) || !strings.Contains(globalConfig, "user.md") {
		t.Fatalf("Kilo instruction reference cleanup damaged config: %s", globalConfig)
	}
}

func TestKiloRTKPluginDryRunAndForeignPreservation(t *testing.T) {
	root := kiloToolProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(root, "global-kilo"))
	plugin := filepath.Join(util.KiloPathsResolved().PluginsDir, "rtk.ts")
	if ok, err := kiloRtkWire(core.RunOpts{DryRun: true}); err != nil || !ok {
		t.Fatalf("Kilo RTK dry-run = %v, %v", ok, err)
	}
	if _, err := os.Stat(plugin); err == nil {
		t.Fatal("Kilo RTK dry-run wrote plugin")
	}
	if err := util.WriteFile(plugin, "export const foreign = true\n"); err != nil {
		t.Fatal(err)
	}
	if ok, err := kiloRtkWire(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("foreign Kilo plugin wire = %v, %v", ok, err)
	}
	backup, err := os.ReadFile(plugin + ".foreign-backup")
	if err != nil || string(backup) != "export const foreign = true\n" {
		t.Fatalf("foreign Kilo plugin backup changed: %q, %v", backup, err)
	}
	if raw, _ := util.ReadFileSafe(plugin); !strings.Contains(raw, kiloRtkMarker) {
		t.Fatal("Tokless Kilo plugin not installed")
	}
}

func TestKiloRTKPluginAPI(t *testing.T) {
	if err := os.Mkdir(filepath.Join(t.TempDir(), ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := kiloToolProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(root, "global-kilo"))
	binDir := t.TempDir()
	rtk := filepath.Join(binDir, "rtk")
	if err := os.WriteFile(rtk, []byte("#!/bin/sh\necho rtk 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if ok, err := kiloRtkWire(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Kilo RTK wire = %v, %v", ok, err)
	}
	raw, _ := util.ReadFileSafe(filepath.Join(util.KiloPathsResolved().PluginsDir, "rtk.ts"))
	absRTK, _ := filepath.Abs(rtk)
	for _, want := range []string{"@kilocode/plugin", "Plugin", "const rtk = " + strconv.Quote(absRTK), "const server: Plugin = async ({ $ })", "tool.execute.before", "toLowerCase", "output.args", "${rtk} rewrite ${command}", ".quiet().nothrow()", "String(result.stdout).trim()", "rewritten !== command", `export default { id: "tokless-rtk", server }`, kiloRtkMarker} {
		if !strings.Contains(raw, want) {
			t.Fatalf("Kilo plugin missing %q: %s", want, raw)
		}
	}
	if strings.Contains(raw, `from "bun"`) {
		t.Fatal("Kilo RTK plugin imports Bun directly")
	}
	if strings.Contains(raw, absRTK+" rewrite") {
		t.Fatal("Kilo RTK plugin embeds unsafe raw executable command")
	}
	_ = root
}

func TestKiloGlobalRTKPathsOutsideGit(t *testing.T) {
	root := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv("KILO_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(root, "opencode"))
	if got := util.KiloPathsResolved().PluginsDir; got != filepath.Join(root, "xdg", "kilo", "plugin") {
		t.Fatalf("Kilo plugin path = %s", got)
	}
	bin := filepath.Join(root, "rtk")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho rtk 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	if ok, err := kiloRtkWire(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("global Kilo RTK wire = %v, %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(util.KiloPathsResolved().PluginsDir, "rtk.ts")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".kilo")); err == nil {
		t.Fatal("global RTK wire created project .kilo")
	}
}

func TestKiloRTKLegacyMigrationSafety(t *testing.T) {
	root := kiloToolProject(t)
	util.SetHomeOverride(filepath.Join(root, "home"))
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(root, "global-kilo"))
	legacy := agents.KiloProjectFile("plugin", "tokless-rtk.ts")
	global := filepath.Join(util.KiloPathsResolved().PluginsDir, "rtk.ts")
	if err := util.WriteFile(legacy, kiloRtkPluginSource("/tmp/rtk")); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(global, "foreign global\n"); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	rtk := filepath.Join(binDir, "rtk")
	if err := os.WriteFile(rtk, []byte("#!/bin/sh\necho rtk 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if ok, err := kiloRtkWire(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("foreign global migration = %v, %v", ok, err)
	}
	if _, err := os.Stat(legacy); err == nil {
		t.Fatal("managed legacy survived successful migration")
	}
	backup, err := os.ReadFile(global + ".foreign-backup")
	if err != nil || string(backup) != "foreign global\n" {
		t.Fatalf("foreign global backup not preserved: %q, %v", backup, err)
	}
	if _, err := os.Stat(global); err != nil {
		t.Fatal("global plugin missing after migration")
	}
}

func TestKiloRTKPluginRuntime(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun unavailable")
	}
	root := kiloToolProject(t)
	binDir := t.TempDir()
	rtk := filepath.Join(binDir, "rtk")
	if err := os.WriteFile(rtk, []byte("#!/bin/sh\nif [ \"$1\" = rewrite ] && [ \"$2\" = \"git status\" ]; then printf 'rtk git status'; exit 3; fi\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(t.TempDir(), "rtk.ts")
	if err := os.WriteFile(plugin, []byte(kiloRtkPluginSource(rtk)), 0o644); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(filepath.Dir(plugin), "node_modules", "@kilocode", "plugin")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"), []byte(`{"name":"@kilocode/plugin","types":"index.d.ts"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "index.d.ts"), []byte("export type Plugin = any\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(t.TempDir(), "runner.ts")
	script := `const p = (await import(process.argv[2])).default
const s = await p.server({ $: Bun.$ })
const hook = s["tool.execute.before"]
async function run(input, output) { await hook(input, output); return output }
for (const [name, input, output] of [
  ["bash", {tool:"bash"}, {args:{command:"git status"}}],
  ["shell", {tool:"ShElL"}, {args:{command:"find . -type f"}}],
  ["non", {tool:"python"}, {args:{command:"git status"}}],
  ["malformed", {tool:"bash"}, {args:null}],
  ["fail", {tool:"bash"}, {args:{command:"git status"}}],
]) console.log(name + ":" + JSON.stringify(await run(input, output)))
`
	if err := os.WriteFile(runner, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bun", runner, plugin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun runtime: %v\n%s", err, out)
	}
	raw := string(out)
	for _, want := range []string{`bash:{"args":{"command":"rtk git status"}}`, `shell:{"args":{"command":"find . -type f"}}`, `non:{"args":{"command":"git status"}}`, `malformed:{"args":null}`, `fail:{"args":{"command":"rtk git status"}}`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("runtime missing %q:\n%s", want, raw)
		}
	}
	_ = root
}

func TestKiloRTKUnwireManagedAndForeignFiles(t *testing.T) {
	root := kiloToolProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(root, "global-kilo"))
	legacy := agents.KiloProjectFile("plugin", "tokless-rtk.ts")
	global := filepath.Join(util.KiloPathsResolved().PluginsDir, "rtk.ts")
	if err := util.WriteFile(legacy, kiloRtkPluginSource("/tmp/rtk")); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(global, kiloRtkPluginSource("/tmp/rtk")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := kiloRtkUnwire(core.RunOpts{}); !ok {
		t.Fatal("managed unwire failed")
	}
	if _, err := os.Stat(legacy); err == nil {
		t.Fatal("managed legacy survived unwire")
	}
	if _, err := os.Stat(global); err == nil {
		t.Fatal("managed global survived unwire")
	}
	if err := util.WriteFile(legacy, "foreign legacy\n"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(global, "foreign global\n"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := kiloRtkUnwire(core.RunOpts{}); !ok {
		t.Fatal("foreign unwire failed")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("foreign legacy removed")
	}
	if _, err := os.Stat(global); err != nil {
		t.Fatal("foreign global removed")
	}
}

func TestKiloRTKOldGlobalCleanup(t *testing.T) {
	root := kiloToolProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(root, "global-kilo"))
	oldGlobal := filepath.Join(util.KiloPathsResolved().PluginsDir, "tokless-rtk.ts")
	newGlobal := filepath.Join(util.KiloPathsResolved().PluginsDir, "rtk.ts")
	if err := util.WriteFile(oldGlobal, kiloRtkPluginSource("/tmp/rtk")); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(newGlobal, kiloRtkPluginSource("/tmp/rtk")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := kiloRtkUnwire(core.RunOpts{}); !ok {
		t.Fatal("managed old global unwire failed")
	}
	if _, err := os.Stat(oldGlobal); err == nil {
		t.Fatal("managed old global survived unwire")
	}
	if err := util.WriteFile(oldGlobal, "foreign old global\n"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(newGlobal, kiloRtkPluginSource("/tmp/rtk")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := kiloRtkUnwire(core.RunOpts{}); !ok {
		t.Fatal("foreign old global unwire failed")
	}
	if raw, _ := util.ReadFileSafe(oldGlobal); raw != "foreign old global\n" {
		t.Fatal("foreign old global changed during unwire")
	}

	binDir := t.TempDir()
	rtk := filepath.Join(binDir, "rtk")
	if err := os.WriteFile(rtk, []byte("#!/bin/sh\necho rtk 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if err := util.WriteFile(oldGlobal, "foreign old global\n"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(newGlobal, kiloRtkPluginSource("/tmp/rtk")); err != nil {
		t.Fatal(err)
	}
	if ok, err := kiloRtkWire(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("foreign old global wire = %v, %v", ok, err)
	}
	if raw, _ := util.ReadFileSafe(oldGlobal); raw != "foreign old global\n" {
		t.Fatal("foreign old global changed during wire")
	}
	if err := util.WriteFile(oldGlobal, kiloRtkPluginSource("/tmp/rtk")); err != nil {
		t.Fatal(err)
	}
	if ok, err := kiloRtkWire(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("managed old global wire = %v, %v", ok, err)
	}
	if _, err := os.Stat(oldGlobal); err == nil {
		t.Fatal("managed old global survived wire")
	}
}
