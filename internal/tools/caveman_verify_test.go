package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeCavemanInstalled(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("CLAUDE_CONFIG_DIR", dir)
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")

	if claudeCavemanInstalled() {
		t.Fatal("empty claude dir should be NOT installed")
	}
	if err := os.WriteFile(filepath.Join(dir, ".caveman-active"), []byte("full"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !claudeCavemanInstalled() {
		t.Fatal("flag file present → should be installed")
	}
	os.Remove(filepath.Join(dir, ".caveman-active"))
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"hooks":{"x":"caveman-activate.js"}}`), 0o644)
	if !claudeCavemanInstalled() {
		t.Fatal("settings.json with caveman → should be installed")
	}
}

func TestCodexCavemanInstalled(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("CODEX_HOME", dir)
	defer os.Unsetenv("CODEX_HOME")
	if codexCavemanInstalled() {
		t.Fatal("empty codex → not installed")
	}
	os.MkdirAll(filepath.Join(dir, "skills", "caveman"), 0o755)
	if !codexCavemanInstalled() {
		t.Fatal("skills/caveman dir → installed")
	}
}

func TestRegisterCavemanOpencodeIdempotent(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", dir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	ocDir := filepath.Join(dir, "opencode")
	os.MkdirAll(ocDir, 0o755)
	cfg := filepath.Join(ocDir, "opencode.json")
	os.WriteFile(cfg, []byte(`{"plugin":["keep@1.0.0"]}`), 0o644)

	registerCavemanOpencode()
	if !opencodePluginInstalled() {
		t.Fatal("after register, caveman should verify as installed")
	}
	first, _ := os.ReadFile(cfg)
	registerCavemanOpencode()
	second, _ := os.ReadFile(cfg)
	if string(first) != string(second) {
		t.Fatalf("not idempotent:\n1: %s\n2: %s", first, second)
	}
	if !strings.Contains(string(second), "keep@1.0.0") {
		t.Fatal("existing plugin entry was lost")
	}
	if !strings.Contains(string(second), "caveman/plugin.js") {
		t.Fatal("caveman plugin entry not registered")
	}
	// caveman-shrink is a proxy needing a manual upstream; registering it bare
	// causes "-32000 Connection closed", so we must NOT write it.
	if strings.Contains(string(second), "caveman-shrink") {
		t.Fatal("caveman-shrink must NOT be registered (breaks with -32000)")
	}
}

func TestCavemanAgentsMdIdempotentPreserves(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", dir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	ocDir := filepath.Join(dir, "opencode")
	os.MkdirAll(ocDir, 0o755)
	md := filepath.Join(ocDir, "AGENTS.md")
	os.WriteFile(md, []byte("# Mine\n\nkeep this\n"), 0o644)

	writeCavemanAgentsMd(ocDir)
	b, _ := os.ReadFile(md)
	if !strings.Contains(string(b), "keep this") {
		t.Fatal("user content lost")
	}
	if !strings.Contains(string(b), cavemanAgentsBegin) || !strings.Contains(string(b), "Respond terse") {
		t.Fatal("caveman ruleset not written")
	}
	writeCavemanAgentsMd(ocDir)
	b2, _ := os.ReadFile(md)
	if strings.Count(string(b2), cavemanAgentsBegin) != 1 {
		t.Fatalf("not idempotent: %d blocks", strings.Count(string(b2), cavemanAgentsBegin))
	}
}

func TestCavemanRulesetAllAgents(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("# user\n\nkeep me\n"), 0o644)
		writeCavemanRuleset(p)
		b, _ := os.ReadFile(p)
		if !strings.Contains(string(b), "keep me") {
			t.Fatalf("%s: user content lost", name)
		}
		if !cavemanRulesetActive(p) {
			t.Fatalf("%s: ruleset not active after write", name)
		}
		writeCavemanRuleset(p) // idempotent
		b2, _ := os.ReadFile(p)
		if strings.Count(string(b2), cavemanAgentsBegin) != 1 {
			t.Fatalf("%s: not idempotent", name)
		}
		removeCavemanRuleset(p)
		b3, _ := os.ReadFile(p)
		if cavemanRulesetActive(p) {
			t.Fatalf("%s: ruleset not removed", name)
		}
		if !strings.Contains(string(b3), "keep me") {
			t.Fatalf("%s: unwire clobbered user content", name)
		}
	}
	// fresh file (no prior content) write + remove deletes empty file
	p := filepath.Join(dir, "fresh.md")
	writeCavemanRuleset(p)
	if !cavemanRulesetActive(p) {
		t.Fatal("fresh: not written")
	}
	removeCavemanRuleset(p)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("fresh: empty file not removed on unwire")
	}
}

func TestCavemanMemoryPaths(t *testing.T) {
	os.Setenv("CODEX_HOME", "/tmp/cxh")
	defer os.Unsetenv("CODEX_HOME")
	if got := codexCavemanMemory(); got != "/tmp/cxh/AGENTS.md" {
		t.Fatalf("codex memory path wrong: %s", got)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", "/tmp/clh")
	defer os.Unsetenv("CLAUDE_CONFIG_DIR")
	if got := claudeCavemanMemory(); got != "/tmp/clh/CLAUDE.md" {
		t.Fatalf("claude memory path wrong: %s", got)
	}
}
