package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func setupHeadroomHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	agents.SetIdeProjectRoot(home)
	ConfigureInstructionConflicts(true)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		agents.SetIdeProjectRoot("")
		ConfigureInstructionConflicts(false)
	})
	return home
}

func TestHeadroomInstallArgsAndNativeRisk(t *testing.T) {
	if got, want := headroomPythonInstallArgs(), []string{"python", "install", "3.13"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Python install args = %v, want %v", got, want)
	}
	if got, want := headroomInstallArgs(false), []string{"tool", "install", "--python", "3.13", "headroom-ai[mcp]"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("install args = %v, want %v", got, want)
	}
	if got, want := headroomInstallArgs(true), []string{"tool", "install", "--upgrade", "--python", "3.13", "headroom-ai[mcp]"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade args = %v, want %v", got, want)
	}
	if !headroomNativeBuildRiskFor("windows", "amd64") || !headroomNativeBuildRiskFor("darwin", "amd64") || headroomNativeBuildRiskFor("darwin", "arm64") || headroomNativeBuildRiskFor("linux", "amd64") {
		t.Fatal("native build risk classification is incorrect")
	}
}

func TestHeadroomInstallUsesManagedUVState(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("TOKLESS_TEST", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.HeadroomPathsResolved()
	if err := os.MkdirAll(filepath.Dir(p.UV), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.UV, []byte("managed uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := runHeadroom
	t.Cleanup(func() { runHeadroom = old })
	var calls [][]string
	runHeadroom = func(command string, args, env []string, ctx context.Context) util.ExecResult {
		calls = append(calls, append([]string{command}, args...))
		if command == util.HeadroomBin() {
			<-ctx.Done()
			return util.ExecResult{Code: 1}
		}
		if command != p.UV {
			return util.ExecResult{Code: 1, Stderr: "unexpected uv"}
		}
		for _, item := range util.HeadroomEnv() {
			if !containsString(env, item) {
				return util.ExecResult{Code: 1, Stderr: "missing managed env"}
			}
		}
		if reflect.DeepEqual(args, headroomInstallArgs(false)) || reflect.DeepEqual(args, headroomInstallArgs(true)) {
			if err := os.MkdirAll(filepath.Dir(util.HeadroomBin()), 0o755); err != nil {
				return util.ExecResult{Code: 1, Stderr: err.Error()}
			}
			if err := os.WriteFile(util.HeadroomBin(), []byte("headroom"), 0o755); err != nil {
				return util.ExecResult{Code: 1, Stderr: err.Error()}
			}
		}
		return util.ExecResult{}
	}
	if ok, err := headroomEnsureInstalled(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("install = %v, %v", ok, err)
	}
	if len(calls) != 4 || !reflect.DeepEqual(calls[0], []string{p.UV, "--version"}) || !reflect.DeepEqual(calls[1], append([]string{p.UV}, headroomPythonInstallArgs()...)) || !reflect.DeepEqual(calls[2], append([]string{p.UV}, headroomInstallArgs(false)...)) {
		t.Fatalf("install call = %v", calls)
	}
	if ok, err := headroomEnsureInstalled(core.RunOpts{Upgrade: true}); err != nil || !ok {
		t.Fatalf("upgrade = %v, %v", ok, err)
	}
	if len(calls) != 8 || !reflect.DeepEqual(calls[6], append([]string{p.UV}, headroomInstallArgs(true)...)) {
		t.Fatalf("upgrade calls = %v", calls)
	}
}

func TestHeadroomServeProbeRequiresLiveServer(t *testing.T) {
	old := runHeadroom
	t.Cleanup(func() { runHeadroom = old })
	runHeadroom = func(string, []string, []string, context.Context) util.ExecResult {
		return util.ExecResult{Code: 127, Stderr: "bad entry point"}
	}
	if err := headroomServeProbe(); err == nil || !strings.Contains(err.Error(), "executable verification") {
		t.Fatalf("immediate failure = %v", err)
	}
	runHeadroom = func(_ string, _ []string, _ []string, ctx context.Context) util.ExecResult {
		<-ctx.Done()
		return util.ExecResult{Code: 1}
	}
	if err := headroomServeProbe(); err != nil {
		t.Fatalf("live server probe = %v", err)
	}
}

func TestHeadroomFailedUpgradeKeepsManagedExecutable(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("TOKLESS_TEST", "")
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.HeadroomPathsResolved()
	if err := os.MkdirAll(filepath.Dir(p.UV), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.UV, []byte("managed uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(util.HeadroomBin()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(util.HeadroomBin(), []byte("working"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := runHeadroom
	t.Cleanup(func() { runHeadroom = old })
	runHeadroom = func(_ string, args, _ []string, _ context.Context) util.ExecResult {
		if reflect.DeepEqual(args, []string{"--version"}) {
			return util.ExecResult{}
		}
		return util.ExecResult{Code: 1, Stderr: "build failed"}
	}
	if ok, err := headroomEnsureInstalled(core.RunOpts{Upgrade: true}); err == nil || ok || !strings.Contains(err.Error(), "managed Python") {
		t.Fatalf("failed upgrade = %v, %v", ok, err)
	}
	if got, err := os.ReadFile(util.HeadroomBin()); err != nil || string(got) != "working" {
		t.Fatalf("existing executable changed: %q, %v", got, err)
	}
}

func TestHeadroomFailureExplainsNativeBuildRisk(t *testing.T) {
	if !strings.Contains(headroomFailure("package install", util.ExecResult{Stderr: "build failed"}).Error(), "build failed") {
		t.Fatal("failure omitted process detail")
	}
	if !headroomNativeBuildRiskFor("windows", "amd64") {
		t.Fatal("expected native build warning platform")
	}
}

func TestHeadroomWiresEverySupportedAgentIdempotently(t *testing.T) {
	setupHeadroomHome(t)
	for _, agent := range []string{"claude", "opencode", "codex", "cursor", "antigravity", "copilot", "droid", "grok", "pi", "omp", "kilo", "cline"} {
		t.Run(agent, func(t *testing.T) {
			for i := 0; i < 2; i++ {
				ok, err := headroom.WireFor[agent](core.RunOpts{})
				if err != nil || !ok || !headroomVerify(agent) {
					t.Fatalf("wire %d = %v, %v; verify=%v", i+1, ok, err, headroomVerify(agent))
				}
			}
			ok, err := headroom.UnwireFor[agent](core.RunOpts{})
			if err != nil || !ok || headroomVerify(agent) {
				t.Fatalf("unwire = %v, %v; verify=%v", ok, err, headroomVerify(agent))
			}
		})
	}
}

func TestHeadroomRefusesDirectUserServer(t *testing.T) {
	setupHeadroomHome(t)
	p := util.ClaudeCodePaths()
	if err := util.WriteFile(p.GlobalJSON, `{"mcpServers":{"headroom":{"type":"stdio","command":"headroom","args":["mcp","serve"]}}}`); err != nil {
		t.Fatal(err)
	}
	ok, err := headroom.WireFor["claude"](core.RunOpts{})
	if err == nil || ok || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("wire direct user server = %v, %v", ok, err)
	}
	raw, _ := util.ReadFileSafe(p.GlobalJSON)
	if !strings.Contains(raw, `"command":"headroom"`) || HasOwner("claude", "headroom") {
		t.Fatalf("user server changed or ownership claimed: %s", raw)
	}
}

func TestHeadroomVerifierRejectsUnboundedServer(t *testing.T) {
	setupHeadroomHome(t)
	if ok, err := headroom.WireFor["claude"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("wire = %v, %v", ok, err)
	}
	p := util.ClaudeCodePaths()
	if err := util.WriteFile(p.GlobalJSON, `{"mcpServers":{"headroom":{"type":"stdio","command":"headroom","args":["mcp","serve"]}}}`); err != nil {
		t.Fatal(err)
	}
	if headroomVerify("claude") {
		t.Fatal("direct headroom server must not verify as Tokless-managed")
	}
	if ok, err := headroom.UnwireFor["claude"](core.RunOpts{}); err != nil || ok {
		t.Fatalf("unwire unbounded server = %v, %v", ok, err)
	}
	raw, _ := util.ReadFileSafe(p.GlobalJSON)
	if !strings.Contains(raw, `"command":"headroom"`) {
		t.Fatalf("unwire removed user server: %s", raw)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
