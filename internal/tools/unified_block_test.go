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

// TestUnifiedBody_WiresAllOwnersAcrossAllAgents confirms writing each
// owner across all four agents produces a body containing every section
// heading and preserves the seeded user content.
// Block-level registration so tests are independent of package init order
// in this file (some packages compile without TestMain in some configs).
func init() {
	Register()
	agents.Register()
}

func TestUnifiedBody_WiresAllOwnersAcrossAllAgents(t *testing.T) {
	setupHome(t)

	agentsList := []string{"claude", "opencode", "codex", "antigravity"}
	wireOrder := []string{"caveman", "codegraph", "context-mode", "ponytail", "karpathy"}

	for _, agent := range agentsList {
		t.Run(agent, func(t *testing.T) {
			path := agentInstructionPath(t, agent)
			_ = util.EnsureDir(filepath.Dir(path))
			_ = util.WriteFile(path, "# Notes\n\nkeep me\n")

			for _, tool := range wireOrder {
				tm := lookupTool(t, tool)
				fn := tm.WireFor[agent]
				ok, err := fn(core.RunOpts{})
				if err != nil || !ok {
					t.Fatalf("wire %s/%s failed: %v ok=%v", agent, tool, err, ok)
				}
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			body := string(raw)
			for _, want := range []string{"## Talking (caveman) →", "## Building (ponytail) →", "## Search Code (codegraph) →", "## Context Window (context-mode) →", "## Engineering (karpathy) →"} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q in body:\n%s", want, body)
				}
			}
			if !strings.Contains(body, "keep me") {
				t.Errorf("user notes lost:\n%s", body)
			}
		})
	}
}

// TestUnifiedBody_PerToolUnwire confirms per-tool unwire removes only its
// section and leaves the body intact until the last owner exits.
func TestUnifiedBody_PerToolUnwire(t *testing.T) {
	setupHome(t)

	agentsList := []string{"claude", "opencode", "codex", "antigravity"}
	wireOrder := []string{"rtk", "caveman", "codegraph", "context-mode", "ponytail", "karpathy"}

	for _, agent := range agentsList {
		t.Run(agent, func(t *testing.T) {
			path := agentInstructionPath(t, agent)
			_ = util.EnsureDir(filepath.Dir(path))
			_ = util.WriteFile(path, "# Notes\n\nkeep me\n")
			for _, tool := range wireOrder {
				tm := lookupTool(t, tool)
				_, _ = tm.WireFor[agent](core.RunOpts{})
			}
			for _, tool := range wireOrder {
				tm := lookupTool(t, tool)
				_, _ = tm.UnwireFor[agent](core.RunOpts{})
				raw, _ := os.ReadFile(path)
				if os.IsNotExist(os.ErrNotExist) && raw == nil {
					raw = nil
				}
				body := string(raw)
				section := util.SectionsByOwner[tool]
				if section != "" && strings.Contains(body, section) {
					t.Errorf("%s/%s still present after unwire:\n%s", agent, tool, body)
				}
			}
			if _, err := os.Stat(path); err == nil {
				t.Errorf("file not removed when last owner unwired: %s", path)
			} else if !os.IsNotExist(err) {
				t.Errorf("stat: %v", err)
			}
		})
	}
}

// TestUnifiedBody_StripsLegacyFences seeds a file with pre-unification
// fences and verifies they are removed when an owner rewrites the body.
func TestUnifiedBody_StripsLegacyFences(t *testing.T) {
	setupHome(t)
	path := filepath.Join(util.Home(), ".codex", "AGENTS.md")
	_ = util.EnsureDir(filepath.Dir(path))
	legacy := "# User notes\n\n" +
		"<!-- caveman-begin -->\nold caveman body\n<!-- caveman-end -->\n\n" +
		"<!-- CONTEXT-MODE_START -->\nold routing\n<!-- CONTEXT-MODE_END -->\n\n" +
		"<!-- CODEGRAPH_START -->\nold codegraph\n<!-- CODEGRAPH_END -->\n"

	_ = util.WriteFile(path, legacy)
	WriteOwner("codex", "caveman")
	after, _ := os.ReadFile(path)
	body := string(after)
	for _, marker := range []string{"<!-- caveman-begin -->", "<!-- CONTEXT-MODE_START -->", "<!-- CODEGRAPH_START -->"} {
		if strings.Contains(body, marker) {
			t.Errorf("legacy fence %q not stripped:\n%s", marker, body)
		}
	}
	if !strings.Contains(body, "## Talking (caveman) →") {
		t.Errorf("unified Talking section missing:\n%s", body)
	}
	if !strings.Contains(body, "# User notes") {
		t.Errorf("user notes lost:\n%s", body)
	}
}

func setupHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("TOKLESS_TEST", "1")
	util.SetHomeOverride(tmp)
	t.Cleanup(func() { util.SetHomeOverride("") })
}

func agentInstructionPath(t *testing.T, agent string) string {
	t.Helper()
	switch agent {
	case "claude":
		return filepath.Join(util.Home(), ".claude", "CLAUDE.md")
	case "opencode":
		return filepath.Join(util.Home(), ".config", "opencode", "AGENTS.md")
	case "codex":
		return filepath.Join(util.Home(), ".codex", "AGENTS.md")
	case "antigravity":
		return filepath.Join(util.Home(), ".gemini", "GEMINI.md")
	}
	t.Fatalf("unknown agent %q", agent)
	return ""
}

func lookupTool(t *testing.T, id string) *core.ToolManifest {
	t.Helper()
	for _, tm := range core.ListTools() {
		if tm.ID == id {
			return tm
		}
	}
	t.Fatalf("tool %q not registered", id)
	return nil
}
