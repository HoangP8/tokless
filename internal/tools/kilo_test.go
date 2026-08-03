package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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

func kiloExecutableFixture(t *testing.T, dir, name, kind string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if runtime.GOOS != "windows" {
		body := "echo rtk 1.0\n"
		if kind == "codegraph" {
			body = "if [ \"$1\" = \"--version\" ]; then echo codegraph 1.0; fi\n"
		} else if kind == "rtk-rewrite" {
			body = "if [ \"$1\" = rewrite ] && [ \"$2\" = \"git status\" ]; then printf 'rtk git status'; exit 3; fi\nexit 3\n"
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}

	path += ".exe"
	src := filepath.Join(t.TempDir(), "main.go")
	program := `package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if ` + strconv.Quote(kind) + ` == "codegraph" {
		if len(args) == 1 && args[0] == "--version" {
			fmt.Println("codegraph 1.0")
		}
		return
	}
	if ` + strconv.Quote(kind) + ` == "rtk-rewrite" {
		if len(args) == 2 && args[0] == "rewrite" && args[1] == "git status" {
			fmt.Print("rtk git status")
		}
		os.Exit(3)
	}
	fmt.Println("rtk 1.0")
}
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "build", "-o", path, src).CombinedOutput(); err != nil {
		t.Fatalf("go build fake %s: %v: %s", name, err, out)
	}
	return path
}

func TestKiloContextAndCodegraphWireVerify(t *testing.T) {
	kiloToolProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "global"))
	t.Setenv("TOKLESS_TEST", "1")
	binDir := t.TempDir()
	kiloExecutableFixture(t, binDir, "codegraph", "codegraph")
	t.Setenv("PATH", binDir)
	globalConfigPath := util.KiloPathsResolved().Config
	globalConfig := "{\"provider\":\"user\"}\n"
	if err := util.WriteFile(globalConfigPath, globalConfig); err != nil {
		t.Fatal(err)
	}
	ctx := contextMode.WireFor["kilo"]
	if ok, err := ctx(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("context Kilo wire = %v, %v", ok, err)
	}
	got, _ := util.ReadFileSafe(util.KiloPathsResolved().Config)
	if !strings.Contains(got, `"context-mode"`) || !strings.Contains(got, `"provider": "user"`) {
		t.Fatalf("Kilo global MCP not written/preserved: %s", got)
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
	if !strings.Contains(string(instructions), "## Context Tools (context-mode)") || !strings.Contains(string(instructions), "## Code Index (codegraph)") {
		t.Fatalf("Kilo AGENTS.md missing owners: %s", instructions)
	}
	if got, _ := util.ReadFileSafe(globalConfigPath); !strings.Contains(got, `"context-mode"`) || !strings.Contains(got, `"codegraph"`) {
		t.Fatalf("Kilo global MCP entries missing: %s", got)
	}
}

func TestKiloContextVerifyRejectsMutatedValidEntry(t *testing.T) {
	kiloToolProject(t)
	t.Setenv("TOKLESS_TEST", "1")
	binDir := t.TempDir()
	kiloExecutableFixture(t, binDir, "rtk", "rtk")
	t.Setenv("PATH", binDir)
	if ok, err := contextMode.WireFor["kilo"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Kilo context wire = %v, %v", ok, err)
	}
	verify := contextMode.VerifyFor["kilo"]
	if result := verify(); result == nil || !*result {
		t.Fatal("valid Kilo context entry did not verify")
	}
	if changed, _, _ := agents.ConfigureKiloMcpSafe("context-mode", []string{"other-server", "--valid"}); changed {
		t.Fatal("failed to mutate Kilo context entry")
	}
	if result := verify(); result == nil || !*result {
		t.Fatal("failed safe mutation rejection")
	}
}

func TestKiloStrictMCPVerificationRejectsMalformedCommands(t *testing.T) {
	kiloToolProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "global"))
	spawn := util.McpSpawn{Command: "tokless", Args: []string{"run-mcp", "--context-mode", "context-mode"}}
	if _, _, _ = agents.ConfigureKiloMcpSafe("context-mode", append([]string{spawn.Command}, spawn.Args...)); !agents.KiloMcpMatches("context-mode", append([]string{spawn.Command}, spawn.Args...)) {
		t.Fatal("proper Kilo context command rejected")
	}
	if _, _, _ = agents.ConfigureKiloMcpSafe("context-mode", []string{"tokless", "run-mcp", "--context-mode", "nested", "run-mcp", "--context-mode", "context-mode"}); agents.KiloMcpMatches("context-mode", []string{"tokless", "run-mcp", "--context-mode", "nested", "run-mcp", "--context-mode", "context-mode"}) {
		t.Fatal("double-wrapped Kilo context command accepted")
	}
	if changed, _, _ := agents.ConfigureKiloMcpSafe("context-mode", []string{"wrong", "command"}); changed {
		t.Fatal("wrong Kilo context command accepted")
	}
}

func TestKiloCodegraphWireUnwireUpdatesGlobalConfig(t *testing.T) {
	kiloToolProject(t)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "global"))
	globalConfigPath := util.KiloPathsResolved().Config
	globalConfig := "{\"provider\":\"user\"}\n"
	_ = util.WriteFile(globalConfigPath, globalConfig)
	if ok, err := codegraph.WireFor["kilo"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Kilo codegraph wire = %v, %v", ok, err)
	}
	if raw, _ := util.ReadFileSafe(agents.KiloInstructionsPath()); !strings.Contains(raw, "## Code Index (codegraph)") {
		t.Fatalf("Kilo AGENTS.md owner missing: %s", raw)
	}
	if ok, err := codegraph.UnwireFor["kilo"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Kilo codegraph unwire = %v, %v", ok, err)
	}
	got, _ := util.ReadFileSafe(globalConfigPath)
	if strings.Contains(got, `"codegraph"`) || !strings.Contains(got, `"provider": "user"`) {
		t.Fatalf("Kilo global config removal damaged user config: %s", got)
	}
	if raw, _ := util.ReadFileSafe(agents.KiloInstructionsPath()); strings.Contains(raw, "## Code Index (codegraph)") {
		t.Fatalf("Kilo AGENTS.md owner remains: %s", raw)
	}
}

func TestKiloWindowsAutoIndexSpawnShape(t *testing.T) {
	kiloToolProject(t)
	orig := util.IsWin
	defer func() { util.IsWin = orig }()
	util.IsWin = true
	spawn := util.WrapAutoIndex("kilo", util.McpSpawn{Command: "cmd", Args: []string{"/c", `C:\\tools\\codegraph.cmd`, "serve", "--mcp"}})
	want := []string{"run-mcp", "--agent", "kilo", "cmd", "/c", `C:\\tools\\codegraph.cmd`, "serve", "--mcp"}
	if !reflect.DeepEqual(spawn.Args, want) {
		t.Fatalf("Windows Kilo auto-index argv = %#v, want %#v", spawn.Args, want)
	}
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "global"))
	if _, _, err := agents.ConfigureKiloMcpSafe("codegraph", append([]string{spawn.Command}, spawn.Args...)); err != nil {
		t.Fatalf("Windows Kilo auto-index command rejected: %v", err)
	}
}

func TestKiloGlobalWireAndCodegraphIndexCreatesNoProjectKiloConfig(t *testing.T) {
	root := kiloToolProject(t)
	t.Setenv("TOKLESS_TEST", "1")
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(t.TempDir(), "global"))
	t.Setenv("CODEGRAPH_DIR", ".custom-codegraph")
	if ok, err := codegraph.WireFor["kilo"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Kilo codegraph wire = %v, %v", ok, err)
	}
	if ok, err := RunCodegraphIndex(root, core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Codegraph index = %v, %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".kilo")); !os.IsNotExist(err) {
		t.Fatalf("global Kilo wiring created project .kilo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".codegraph")); err != nil {
		t.Fatalf("CodeGraph index missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".custom-codegraph")); !os.IsNotExist(err) {
		t.Fatalf("hostile CODEGRAPH_DIR was used: %v", err)
	}
}

func TestKiloRTKPluginDryRunAndForeignPreservation(t *testing.T) {
	root := kiloToolProject(t)
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("HOME", home)
	t.Setenv("KILO_CONFIG_DIR", filepath.Join(root, "global-kilo"))
	binDir := t.TempDir()
	kiloExecutableFixture(t, binDir, "rtk", "rtk")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
	kiloExecutableFixture(t, binDir, "rtk", "rtk")
	t.Setenv("PATH", binDir)
	if ok, err := kiloRtkWire(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("Kilo RTK wire = %v, %v", ok, err)
	}
	raw, _ := util.ReadFileSafe(filepath.Join(util.KiloPathsResolved().PluginsDir, "rtk.ts"))
	absRTK, _ := filepath.Abs(util.ResolveRtkBin())
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
	kiloExecutableFixture(t, root, "rtk", "rtk")
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
	kiloExecutableFixture(t, binDir, "rtk", "rtk")
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
	rtk := kiloExecutableFixture(t, binDir, "rtk", "rtk-rewrite")
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
	kiloExecutableFixture(t, binDir, "rtk", "rtk")
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
