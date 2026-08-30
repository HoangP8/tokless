package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func seedGrokBinary(t *testing.T) string {
	t.Helper()
	home := setGrokBuildTestHome(t)
	dir := filepath.Join(home, ".grok", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "grok")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho real-grok \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func seedGrokBinaryInGrokHome(t *testing.T) string {
	t.Helper()
	home := setGrokBuildTestHome(t)
	dir := filepath.Join(home, ".grok")
	t.Setenv("GROK_HOME", dir)
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "grok"), []byte("#!/bin/sh\necho real-grok \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstallGrokShimStashesRealBinary(t *testing.T) {
	seedGrokBinary(t)
	changed, err := InstallGrokShim()
	if err != nil || !changed {
		t.Fatalf("install: changed=%v err=%v", changed, err)
	}
	raw, _ := os.ReadFile(grokBinFile())
	if !isGrokShim(string(raw)) || !strings.Contains(string(raw), grokShimPortLine()) {
		t.Fatalf("shim content wrong:\n%s", raw)
	}
	real, _ := os.ReadFile(grokRealBinFile())
	if !strings.Contains(string(real), "real-grok") {
		t.Fatalf("real binary not preserved: %q", string(real))
	}
	if !GrokShimWired() {
		t.Fatal("wired = false after install")
	}
}

func TestInstallGrokShimIdempotent(t *testing.T) {
	seedGrokBinary(t)
	if _, err := InstallGrokShim(); err != nil {
		t.Fatal(err)
	}
	changed, err := InstallGrokShim()
	if err != nil || changed {
		t.Fatalf("second install changed=%v err=%v", changed, err)
	}
}

func TestInstallGrokShimAdoptsUpgradedBinary(t *testing.T) {
	seedGrokBinary(t)
	if _, err := InstallGrokShim(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grokBinFile(), []byte("#!/bin/sh\necho upgraded \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallGrokShim(); err != nil {
		t.Fatal(err)
	}
	real, _ := os.ReadFile(grokRealBinFile())
	if !strings.Contains(string(real), "upgraded") {
		t.Fatalf("upgrade not adopted: %q", string(real))
	}
	if !GrokShimWired() {
		t.Fatal("not wired after upgrade adoption")
	}
}

func TestRemoveGrokShimRestoresRealBinary(t *testing.T) {
	seedGrokBinary(t)
	if _, err := InstallGrokShim(); err != nil {
		t.Fatal(err)
	}
	if !RemoveGrokShim() {
		t.Fatal("remove failed")
	}
	raw, _ := os.ReadFile(grokBinFile())
	if isGrokShim(string(raw)) {
		t.Fatalf("shim still present:\n%s", raw)
	}
	if !strings.Contains(string(raw), "real-grok") {
		t.Fatalf("real binary not restored: %q", string(raw))
	}
	if GrokShimWired() {
		t.Fatal("wired after remove")
	}
}

func TestRemoveGrokShimNoopOnForeignBinary(t *testing.T) {
	seedGrokBinary(t)
	if RemoveGrokShim() {
		t.Fatal("remove must be noop when no shim installed")
	}
	raw, _ := os.ReadFile(grokBinFile())
	if !strings.Contains(string(raw), "real-grok") {
		t.Fatal("foreign binary was modified")
	}
}

func TestConfigureGrokProxyInstallsShimForOAuthConfig(t *testing.T) {
	_ = seedGrokBinaryInGrokHome(t)
	if err := os.WriteFile(grokConfigFile(), []byte("[models]\ndefault = \"grok-4.6\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _ := ConfigureGrokProxy()
	if !changed || !GrokShimWired() {
		t.Fatalf("configure: changed=%v wired=%v", changed, GrokShimWired())
	}
	if _, err := os.Stat(grokRealBinFile()); err != nil {
		t.Fatal("real binary not stashed")
	}
	if !RemoveGrokProxy() {
		t.Fatal("remove failed")
	}
	raw, _ := os.ReadFile(grokBinFile())
	if isGrokShim(string(raw)) {
		t.Fatal("shim survived remove")
	}
}

func TestConfigureGrokProxyStripsLegacyMarkerAndInstallsShim(t *testing.T) {
	_ = seedGrokBinaryInGrokHome(t)
	content := "[models]\ndefault = \"grok-4.6\"\n\n" + renderStripFixture() + "\n"
	if err := os.WriteFile(grokConfigFile(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _ = ConfigureGrokProxy(); !GrokShimWired() {
		t.Fatal("shim not installed")
	}
	raw, _ := os.ReadFile(grokConfigFile())
	if strings.Contains(string(raw), "headroom:grok-build") {
		t.Fatalf("legacy marker survived configure:\n%s", raw)
	}
}

func TestDetectGrokProxyShimManaged(t *testing.T) {
	_ = seedGrokBinaryInGrokHome(t)
	if err := os.WriteFile(grokConfigFile(), []byte("[models]\ndefault = \"grok-4.6\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DetectProxy("grok"); got.State != ProxyStateUnconfigured {
		t.Fatalf("pre-install state = %s (%s)", got.State, got.Detail)
	}
	if _, _ = ConfigureGrokProxy(); !GrokShimWired() {
		t.Fatal("shim not installed")
	}
	if got := DetectProxy("grok"); got.State != ProxyStateManaged {
		t.Fatalf("post-install state = %s (%s)", got.State, got.Detail)
	}
	_ = RemoveGrokProxy()
}

func TestInstallGrokShimUpgradesStaleFormat(t *testing.T) {
	seedGrokBinary(t)
	if _, err := InstallGrokShim(); err != nil {
		t.Fatal(err)
	}
	stale := renderGrokShim()
	stale = strings.Replace(stale, "PORT=\"${TOKLESS_GROK_PROXY_PORT:", "PORT=\"${DISABLED_VAR:-", 1)
	if err := os.WriteFile(grokBinFile(), []byte(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := InstallGrokShim()
	if err != nil || !changed {
		t.Fatalf("upgrade: changed=%v err=%v", changed, err)
	}
	raw, _ := os.ReadFile(grokBinFile())
	if string(raw) != renderGrokShim() {
		t.Fatal("stale shim not upgraded to current render")
	}
}

func TestInstallGrokShimRefusesUnreadableBinary(t *testing.T) {
	seedGrokBinary(t)
	if err := os.Chmod(grokBinFile(), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(grokBinFile(), 0o755) })
	if _, err := InstallGrokShim(); err == nil {
		t.Fatal("unreadable binary must fail install")
	}
	raw, _ := os.ReadFile(grokBinFile())
	if strings.Contains(string(raw), "tokless:grok-launcher") {
		t.Fatal("unreadable binary was overwritten")
	}
}

func TestRenderGrokShimShellSafePath(t *testing.T) {
	home := setGrokBuildTestHome(t)
	dir := filepath.Join(home, ".grok", "b'in")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_HOME", dir)
	script := renderGrokShim()
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	if err := cmd.Run(); err != nil {
		t.Fatalf("rendered shim does not parse: %v\n%s", err, script)
	}
	if !strings.Contains(script, "TOKLESS_GROK_PROXY_PORT") {
		t.Fatal("shim missing runtime port override")
	}
}

func TestShimRequiresHeadroomIdentityBody(t *testing.T) {
	script := renderGrokShim()
	line := ""
	for _, l := range strings.Split(script, "\n") {
		if strings.Contains(l, "/livez") {
			line = l
		}
	}
	if !strings.Contains(line, `"service":"headroom-proxy"`) {
		t.Fatalf("livez check not exact-JSON field: %s", line)
	}
	probe := func(body string) bool {
		replaced := strings.Replace(line, "curl -sfm 1 \"http://127.0.0.1:${PORT}/livez\"", "printf '%s' '"+body+"'", 1)
		cond := strings.TrimSuffix(strings.TrimPrefix(replaced, "if "), "; then")
		return exec.Command("sh", "-c", cond).Run() == nil
	}
	if probe(`{"service":"not-headroom-proxy"}`) {
		t.Fatal("accepted body with foreign service value")
	}
	if !probe(`{"service":"headroom-proxy","status":"healthy"}`) {
		t.Fatal("rejected genuine headroom body")
	}
}

func TestInstallGrokShimRefusesForeignMarkerShape(t *testing.T) {
	seedGrokBinary(t)
	foreign := "#!/bin/sh\n# tokless:grok-launcher\necho something-else\n"
	if err := os.WriteFile(grokBinFile(), []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallGrokShim(); err == nil {
		t.Fatal("marker-bearing foreign file must not be overwritten")
	}
	raw, _ := os.ReadFile(grokBinFile())
	if string(raw) != foreign {
		t.Fatal("foreign marker file was modified")
	}
}
