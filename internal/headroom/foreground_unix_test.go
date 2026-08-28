//go:build !windows

package headroom

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/HoangP8/tokless/internal/util"
)

func TestRunHeadroomForegroundExecFailureClearsState(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	oldExec := proxyExec
	t.Cleanup(func() { proxyExec = oldExec })
	proxyExec = func(string, []string, []string) error { return errors.New("exec failed") }

	err := runHeadroomForeground(filepath.Join(t.TempDir(), "headroom"), []string{"proxy", "--port", "8787"})
	if err == nil {
		t.Fatal("runHeadroomForeground unexpectedly succeeded")
	}
	if _, ok := util.ReadFileSafe(proxySupervisedFile()); ok {
		t.Fatal("supervised state survived exec failure")
	}
}

func TestRunHeadroomForegroundRecordsResolvedBinary(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	oldExec := proxyExec
	t.Cleanup(func() { proxyExec = oldExec })
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	proxyExec = func(gotBin string, gotArgs, _ []string) error {
		if gotBin != bin || !equalStrings(gotArgs, []string{bin, "proxy", "--port", "8787"}) {
			t.Fatalf("exec = %q %v", gotBin, gotArgs)
		}
		return errors.New("stop test exec")
	}
	if err := runHeadroomForeground(bin, []string{"proxy", "--port", "8787"}); err == nil {
		t.Fatal("runHeadroomForeground unexpectedly succeeded")
	}
}

func TestRunProxyForegroundPersistsRuntimeBeforeExec(t *testing.T) {
	util.SetHomeOverride(t.TempDir())
	t.Cleanup(func() { util.SetHomeOverride("") })
	t.Setenv("TOKLESS_HEADROOM_PROXY_PORT", "9123")
	t.Setenv("TOKLESS_HEADROOM_ANTHROPIC_URL", "")
	t.Setenv("TOKLESS_HEADROOM_OPENAI_URL", "")
	t.Setenv("TOKLESS_HEADROOM_GEMINI_URL", "")
	t.Setenv("TOKLESS_HEADROOM_CLOUDCODE_URL", "")
	bin := util.HeadroomBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("headroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExec := proxyExec
	t.Cleanup(func() { proxyExec = oldExec })
	proxyExec = func(gotBin string, gotArgs, _ []string) error {
		if gotBin != bin || !equalStrings(gotArgs, []string{bin, "proxy", "--port", "9123", "--no-cache", "--anthropic-api-url", "https://api.anthropic.com", "--openai-api-url", "https://api.openai.com"}) {
			t.Fatalf("exec = %q %v", gotBin, gotArgs)
		}
		return errors.New("stop test exec")
	}
	if err := RunProxyForeground(); err == nil {
		t.Fatal("RunProxyForeground unexpectedly succeeded")
	}
	st, ok := util.ReadProxyRuntime()
	if !ok || st.Port != 9123 {
		t.Fatalf("runtime = %+v (ok=%v), want port 9123", st, ok)
	}
}
