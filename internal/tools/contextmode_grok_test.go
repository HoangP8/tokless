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

func TestContextModeGrokWireAndUnwirePreserveOtherConfig(t *testing.T) {
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("HOME", home)
	t.Setenv("TOKLESS_TEST", "1")
	ConfigureInstructionConflicts(true)
	t.Cleanup(func() {
		util.SetHomeOverride("")
		ConfigureInstructionConflicts(false)
	})

	grokDir := filepath.Join(home, ".grok")
	if err := os.MkdirAll(filepath.Join(grokDir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(grokDir, "config.toml")
	if err := os.WriteFile(config, []byte("[mcp_servers.other]\ncommand = \"other\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	userRule := filepath.Join(grokDir, "rules", "user.md")
	if err := os.WriteFile(userRule, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := ctxWireGrok(core.RunOpts{})
	if err != nil || !ok {
		t.Fatalf("ctxWireGrok = %v, %v", ok, err)
	}
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "[mcp_servers.other]") || !agents.GrokContextModeMcpHas() || !ctxVerifyGrok() {
		t.Fatalf("Grok context-mode configuration invalid:\n%s", raw)
	}
	if !strings.Contains(string(raw), "--context-mode") || strings.Contains(string(raw), `"--agent", "grok"`) {
		t.Fatalf("Grok context-mode must use existing bounded proxy:\n%s", raw)
	}
	ok, err = ctxWireGrok(core.RunOpts{})
	if err != nil || !ok || !agents.GrokContextModeMcpHas() || !HasOwner("grok", "context-mode") {
		t.Fatalf("second ctxWireGrok = %v, %v", ok, err)
	}

	ok, err = ctxUnwireGrok(core.RunOpts{})
	if err != nil || !ok {
		t.Fatalf("ctxUnwireGrok = %v, %v", ok, err)
	}
	raw, err = os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "[mcp_servers.context-mode]") || !strings.Contains(string(raw), "[mcp_servers.other]") {
		t.Fatalf("Grok MCP removal changed unrelated config:\n%s", raw)
	}
	if got, err := os.ReadFile(userRule); err != nil || string(got) != "keep me\n" {
		t.Fatalf("unrelated Grok rule changed: %q, %v", got, err)
	}
}

func TestGrokCodegraphWireWritesOwnedNativeMcp(t *testing.T) {
	setupHome(t)
	if ok, err := codegraphWire("grok")(core.RunOpts{}); err != nil || !ok {
		t.Fatalf("codegraph wire: ok=%v err=%v", ok, err)
	}
	if !codegraphVerify("grok") {
		t.Fatal("Grok CodeGraph verification failed")
	}
}
