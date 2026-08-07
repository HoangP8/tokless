package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func grokTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("TOKLESS_TEST", "1")
	return home
}

func TestGrokRtkHelperContent(t *testing.T) {
	s := grokRtkHelper("/opt/bin/rtk")
	for _, want := range []string{grokRtkMarker, "rtk rewrite", "/opt/bin/rtk", "command -v"} {
		if !strings.Contains(s, want) {
			t.Fatalf("helper missing %q:\n%s", want, s)
		}
	}
	for _, c := range grokRtkCmds {
		if !strings.Contains(s, c+"() { _tokless_rtk "+c) {
			t.Fatalf("helper missing wrapper function for %s:\n%s", c, s)
		}
	}
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("helper syntax: %v\n%s", err, out)
	}
}

func TestGrokRtkWireUnwireUsesConfig(t *testing.T) {
	home := grokTestHome(t)
	if ok, err := rtk.WireFor["grok"](core.RunOpts{}); err != nil || !ok {
		t.Fatalf("wire = %v, %v", ok, err)
	}
	if !grokRtkVerify() {
		t.Fatal("verify failed after wire")
	}
	if util.Exists(filepath.Join(home, ".zshrc")) || util.Exists(filepath.Join(home, ".bashrc")) {
		t.Fatal("wire modified a shell rc file")
	}
	if !util.Exists(grokRtkHelperPath()) {
		t.Fatal("helper not written")
	}
	raw, ok := util.ReadFileSafe(grokRtkConfig())
	if !ok || !strings.Contains(raw, "[toolset.bash]") || !strings.Contains(raw, "cmd_prefix") || !strings.Contains(raw, "source ") {
		t.Fatalf("config missing managed cmd_prefix:\n%s", raw)
	}
	if ok, _ := rtk.UnwireFor["grok"](core.RunOpts{}); !ok || grokRtkVerify() {
		t.Fatalf("unwire = %v, verify = %v", ok, grokRtkVerify())
	}
	if util.Exists(grokRtkHelperPath()) {
		t.Fatal("helper not removed on unwire")
	}
}

func TestGrokRtkCmdPrefixSourcingRunsRtk(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "rtk")
	src := "#!/bin/sh\n" +
		"if [ \"$1\" = rewrite ]; then echo 'rtk git --compact'; exit 3; fi\n" +
		"if [ \"$1\" = git ] && [ \"$2\" = --compact ]; then echo COMPACTED; exit 0; fi\n" +
		"exit 1\n"
	if err := os.WriteFile(fake, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "grok-rtk.sh")
	if err := os.WriteFile(helper, []byte(grokRtkHelper(fake)), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", "source "+shQuote(helper)+" && git status")
	cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "COMPACTED") {
		t.Fatalf("sourced helper output = %q, err = %v", out, err)
	}
}

func TestGrokRtkCmdPrefixFallsThroughToRealCommand(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "rtk")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "grok-rtk.sh")
	if err := os.WriteFile(helper, []byte(grokRtkHelper(fake)), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", "source "+shQuote(helper)+" && git --version")
	cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "git version") {
		t.Fatalf("fallback output = %q, err = %v", out, err)
	}
}
