package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func seedAntigravityBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(util.Home(), ".local", "bin", "agy")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("\x7fELFfake-agy"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestAntigravityShimLifecycle(t *testing.T) {
	setTestHome(t)
	bin := seedAntigravityBinary(t)
	changed, err := installAntigravityShim()
	if err != nil || !changed || !AntigravityShimWired() {
		t.Fatalf("install: changed=%v wired=%v err=%v", changed, AntigravityShimWired(), err)
	}
	if changed, err = installAntigravityShim(); err != nil || changed {
		t.Fatalf("second install: changed=%v err=%v", changed, err)
	}
	if !removeAntigravityShim() {
		t.Fatal("remove failed")
	}
	raw, err := os.ReadFile(bin)
	if err != nil || string(raw) != "\x7fELFfake-agy" {
		t.Fatalf("real binary not restored: %q err=%v", raw, err)
	}
}

func TestAntigravityShimRefusesForeignWrapper(t *testing.T) {
	setTestHome(t)
	bin := seedAntigravityBinary(t)
	foreign := "#!/bin/sh\nexec /vendor/agy \"$@\"\n"
	if err := os.WriteFile(bin, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installAntigravityShim(); err == nil {
		t.Fatal("foreign wrapper accepted")
	}
	raw, _ := os.ReadFile(bin)
	if string(raw) != foreign {
		t.Fatal("foreign wrapper changed")
	}
}

func TestAntigravityShimRefusesForeignRealStash(t *testing.T) {
	setTestHome(t)
	seedAntigravityBinary(t)
	if err := os.WriteFile(antigravityRealBinFile(), []byte("foreign-stash"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installAntigravityShim(); err == nil {
		t.Fatal("foreign real stash accepted")
	}
	real, _ := os.ReadFile(antigravityRealBinFile())
	if string(real) != "foreign-stash" {
		t.Fatal("foreign real stash changed")
	}
}

func TestAntigravityShimAdoptsUpgradedBinary(t *testing.T) {
	setTestHome(t)
	bin := seedAntigravityBinary(t)
	if _, err := installAntigravityShim(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("\x7fELFupgraded-agy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installAntigravityShim(); err != nil {
		t.Fatal(err)
	}
	real, _ := os.ReadFile(antigravityRealBinFile())
	if string(real) != "\x7fELFupgraded-agy" || !AntigravityShimWired() {
		t.Fatalf("upgrade not adopted: %q", real)
	}
}

func TestAntigravityShimRefusesForeignRealOnUpgrade(t *testing.T) {
	setTestHome(t)
	bin := seedAntigravityBinary(t)
	if _, err := installAntigravityShim(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(antigravityRealBinFile(), []byte("foreign-real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("\x7fELFnew-vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installAntigravityShim(); err == nil {
		t.Fatal("foreign real was replaced")
	}
}

func TestAntigravityShimRoutesPlainCLI(t *testing.T) {
	setTestHome(t)
	bin := seedAntigravityBinary(t)
	if _, err := installAntigravityShim(); err != nil {
		t.Fatal(err)
	}
	ensure := filepath.Join(util.Home(), "ensure-proxy")
	if err := os.WriteFile(ensure, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"$GOOGLE_GEMINI_BASE_URL\" \"$CLOUD_CODE_URL\" \"$*\"\n"
	if err := os.WriteFile(antigravityRealBinFile(), []byte(real), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(bin)
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "TOKLESS=") {
			lines[i] = "TOKLESS=" + shQuote(ensure)
		}
	}
	if err := os.WriteFile(bin, []byte(strings.Join(lines, "\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "prompt", "with spaces").CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v: %s", err, out)
	}
	want := proxyTestURL + "\n" + proxyTestURL + "\nprompt with spaces\n"
	if string(out) != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestAntigravityShimFailsClosed(t *testing.T) {
	setTestHome(t)
	bin := seedAntigravityBinary(t)
	if _, err := installAntigravityShim(); err != nil {
		t.Fatal(err)
	}
	ensure := filepath.Join(util.Home(), "ensure-proxy")
	if err := os.WriteFile(ensure, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(bin)
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "TOKLESS=") {
			lines[i] = "TOKLESS=" + shQuote(ensure)
		}
	}
	if err := os.WriteFile(bin, []byte(strings.Join(lines, "\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "prompt").CombinedOutput(); err == nil || !strings.Contains(string(out), "Headroom proxy unavailable") {
		t.Fatalf("run must fail closed: err=%v output=%s", err, out)
	}
}

func TestAntigravityIDEProxyLifecycle(t *testing.T) {
	setTestHome(t)
	settings := filepath.Join(util.Home(), ".config", "Antigravity IDE", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("{\n  \"editor.fontSize\": 14\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := configureAntigravityIDE()
	if err != nil || !changed || !AntigravityIDEProxyWired() {
		t.Fatalf("configure: changed=%v wired=%v err=%v", changed, AntigravityIDEProxyWired(), err)
	}
	if !removeAntigravityIDE() {
		t.Fatal("remove failed")
	}
	raw, _ := os.ReadFile(settings)
	if strings.Contains(string(raw), antigravityIDEEndpointKey) || !strings.Contains(string(raw), "editor.fontSize") {
		t.Fatalf("settings not restored: %s", raw)
	}
}

func TestAntigravityIDEProxyPreservesForeignEndpoint(t *testing.T) {
	setTestHome(t)
	settings := filepath.Join(util.Home(), ".config", "Antigravity IDE", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "{\n  \"jetski.cloudCodeUrl\": \"https://user.example\"\n}\n"
	if err := os.WriteFile(settings, []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := configureAntigravityIDE(); err == nil || changed {
		t.Fatalf("foreign endpoint accepted: changed=%v err=%v", changed, err)
	}
	raw, _ := os.ReadFile(settings)
	if string(raw) != foreign {
		t.Fatal("foreign endpoint changed")
	}
}

func TestAntigravityIDESettingsCandidatesAllOS(t *testing.T) {
	setTestHome(t)
	old := goosForDetect
	t.Cleanup(func() { goosForDetect = old })
	for _, goos := range []string{"linux", "darwin", "windows"} {
		goosForDetect = goos
		paths := antigravityIDESettingsCandidates()
		if len(paths) != 2 || filepath.Base(paths[0]) != "settings.json" || !strings.Contains(paths[0], "Antigravity IDE") {
			t.Fatalf("%s candidates = %v", goos, paths)
		}
	}
}

func TestAntigravityWindowsShimLifecycle(t *testing.T) {
	setTestHome(t)
	oldWin, oldGOOS := util.IsWin, goosForDetect
	util.IsWin, goosForDetect = true, "windows"
	t.Cleanup(func() { util.IsWin, goosForDetect = oldWin, oldGOOS })
	t.Setenv("LOCALAPPDATA", filepath.Join(util.Home(), "AppData", "Local"))
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "agy", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	vendor := filepath.Join(dir, "agy.exe")
	if err := os.WriteFile(vendor, []byte("MZfake-agy"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := installAntigravityShim()
	if err != nil || !changed || !AntigravityShimWired() {
		t.Fatalf("install: changed=%v wired=%v err=%v", changed, AntigravityShimWired(), err)
	}
	if !util.Exists(filepath.Join(dir, "agy.cmd")) || !util.Exists(filepath.Join(dir, "agy.real.exe")) || util.Exists(vendor) {
		t.Fatal("Windows launcher files incorrect")
	}
	if !removeAntigravityShim() {
		t.Fatal("remove failed")
	}
	raw, _ := os.ReadFile(vendor)
	if string(raw) != "MZfake-agy" || util.Exists(filepath.Join(dir, "agy.cmd")) {
		t.Fatal("Windows vendor binary not restored")
	}
}
