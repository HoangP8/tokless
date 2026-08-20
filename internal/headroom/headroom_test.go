package headroom

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func TestHeadroomInstallArgsAndNativeRisk(t *testing.T) {
	if got, want := headroomPythonInstallArgs(), []string{"python", "install", "3.13"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Python install args = %v, want %v", got, want)
	}
	if got, want := headroomInstallArgs(false), []string{"tool", "install", "--python", "3.13", "headroom-ai[proxy]"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("install args = %v, want %v", got, want)
	}
	if got, want := headroomInstallArgs(true), []string{"tool", "install", "--upgrade", "--python", "3.13", "headroom-ai[proxy]"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade args = %v, want %v", got, want)
	}
	if !headroomNativeBuildRiskFor("windows", "amd64") || !headroomNativeBuildRiskFor("darwin", "amd64") || headroomNativeBuildRiskFor("darwin", "arm64") || headroomNativeBuildRiskFor("linux", "amd64") {
		t.Fatal("native build risk classification is incorrect")
	}
}

func TestHeadroomUVBootstrapCmdAllOS(t *testing.T) {
	cmd, args, err := headroomUVBootstrapCmd(true, false, false)
	if err != nil || cmd != "powershell" {
		t.Fatalf("windows = %q %v err=%v", cmd, args, err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "install.ps1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("windows args missing %q: %v", want, args)
		}
	}
	// UV_INSTALL_DIR comes from process env, not the PS one-liner.
	if strings.Contains(joined, "TOKLESS_HEADROOM_UV") || strings.Contains(joined, "$env:UV_INSTALL_DIR") {
		t.Fatalf("windows must not re-set UV_INSTALL_DIR in script: %v", args)
	}

	cmd, args, err = headroomUVBootstrapCmd(false, true, true)
	if err != nil || cmd != "sh" || !strings.Contains(strings.Join(args, " "), "curl -LsSf") {
		t.Fatalf("unix curl preferred = %q %v err=%v", cmd, args, err)
	}
	cmd, args, err = headroomUVBootstrapCmd(false, false, true)
	if err != nil || cmd != "sh" || !strings.Contains(strings.Join(args, " "), "wget -qO-") {
		t.Fatalf("unix wget fallback = %q %v err=%v", cmd, args, err)
	}
	if _, _, err := headroomUVBootstrapCmd(false, false, false); err == nil || !strings.Contains(err.Error(), "curl or wget") {
		t.Fatalf("unix neither = %v", err)
	}
}

func TestHeadroomUVBootstrapEnvPinsInstallDir(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	p := util.HeadroomPathsResolved()
	env := util.HeadroomUVBootstrapEnv()
	wantInstall := "UV_INSTALL_DIR=" + filepath.Dir(p.UV)
	wantNoPath := "UV_NO_MODIFY_PATH=1"
	var gotInstall, gotNoPath bool
	for _, e := range env {
		if e == wantInstall {
			gotInstall = true
		}
		if e == wantNoPath {
			gotNoPath = true
		}
	}
	if !gotInstall || !gotNoPath {
		t.Fatalf("bootstrap env = %v want %q and %q", env, wantInstall, wantNoPath)
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
			if reflect.DeepEqual(args, []string{"--version"}) {
				return util.ExecResult{}
			}
			return util.ExecResult{Code: 1, Stderr: "unexpected bin args"}
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
	if ok, err := EnsureInstalled(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("install = %v, %v", ok, err)
	}
	if len(calls) != 4 || !reflect.DeepEqual(calls[0], []string{p.UV, "--version"}) || !reflect.DeepEqual(calls[1], append([]string{p.UV}, headroomPythonInstallArgs()...)) || !reflect.DeepEqual(calls[2], append([]string{p.UV}, headroomInstallArgs(false)...)) {
		t.Fatalf("install call = %v", calls)
	}
	if ok, err := EnsureInstalled(core.RunOpts{Upgrade: true}); err != nil || !ok {
		t.Fatalf("upgrade = %v, %v", ok, err)
	}
	if len(calls) != 8 || !reflect.DeepEqual(calls[6], append([]string{p.UV}, headroomInstallArgs(true)...)) {
		t.Fatalf("upgrade calls = %v", calls)
	}
}

func TestHeadroomVersionProbeRequiresWorkingBinary(t *testing.T) {
	old := runHeadroom
	t.Cleanup(func() { runHeadroom = old })
	runHeadroom = func(string, []string, []string, context.Context) util.ExecResult {
		return util.ExecResult{Code: 127, Stderr: "bad entry point"}
	}
	if err := headroomVersionProbe(); err == nil || !strings.Contains(err.Error(), "executable verification") {
		t.Fatalf("immediate failure = %v", err)
	}
	runHeadroom = func(_ string, _ []string, _ []string, _ context.Context) util.ExecResult {
		return util.ExecResult{}
	}
	if err := headroomVersionProbe(); err != nil {
		t.Fatalf("version probe = %v", err)
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
	upgradeCalled := false
	runHeadroom = func(_ string, args, _ []string, _ context.Context) util.ExecResult {
		if reflect.DeepEqual(args, []string{"--version"}) {
			return util.ExecResult{}
		}
		if reflect.DeepEqual(args, headroomPythonInstallArgs()) {
			return util.ExecResult{}
		}
		if reflect.DeepEqual(args, headroomInstallArgs(true)) {
			upgradeCalled = true
			return util.ExecResult{Code: 1, Stderr: "build failed"}
		}
		t.Fatalf("unexpected Headroom call args: %v", args)
		return util.ExecResult{Code: 1, Stderr: "build failed"}
	}
	if ok, err := EnsureInstalled(core.RunOpts{Upgrade: true}); err == nil || ok || !strings.Contains(err.Error(), "package install") {
		t.Fatalf("failed upgrade = %v, %v", ok, err)
	}
	if !upgradeCalled {
		t.Fatal("upgrade call was not made")
	}
	if got, err := os.ReadFile(util.HeadroomBin()); err != nil || string(got) != "working" {
		t.Fatalf("existing executable changed: %q, %v", got, err)
	}
}

func TestHeadroomFailureIncludesRemediation(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })

	err := headroomFailureFor("package install", util.ExecResult{Code: 1, Stderr: "first detail\nsecond detail"}, true)
	got := err.Error()
	if !strings.Contains(got, "headroom package install failed: first detail") {
		t.Fatalf("failure detail = %q", got)
	}
	if !strings.Contains(got, "Rust/native toolchain may be required on this platform") {
		t.Fatalf("native warning missing: %q", got)
	}
	wantCommand := strings.Join(append([]string{util.HeadroomPathsResolved().UV}, headroomInstallArgs(false)...), " ")
	if !strings.Contains(got, "Manual: "+wantCommand) {
		t.Fatalf("managed install command missing: %q", got)
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
