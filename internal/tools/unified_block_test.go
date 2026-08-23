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

// Keep tests independent of package init order.
func init() {
	Register()
	agents.Register()
}

func TestUnifiedBody_WiresAllOwnersAcrossAllAgents(t *testing.T) {
	setupHome(t)

	agentsList := []string{"claude", "opencode", "codex", "antigravity", "grok"}
	wireOrder := []string{"caveman", "codegraph", "context-mode", "ponytail"}
	expectedOrder := []string{
		util.SectionsByOwner["principles"],
		util.SectionsByOwner["caveman"],
		util.SectionsByOwner["ponytail"],
		util.SectionsByOwner["codegraph"],
		util.SectionsByOwner["context-mode"],
	}

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
			if !strings.Contains(body, "# Agent Instructions") {
				t.Errorf("overview heading missing:\n%s", body)
			}
			if strings.Count(body, "# Agent Instructions") != 1 {
				t.Errorf("overview heading duplicated:\n%s", body)
			}
			prev := -1
			for _, want := range expectedOrder {
				idx := strings.Index(body, want)
				if idx < 0 {
					t.Errorf("missing %q in body:\n%s", want, body)
					continue
				}
				if idx <= prev {
					t.Errorf("heading %q out of order:\n%s", want, body)
				}
				prev = idx
			}
			if !strings.Contains(body, "keep me") {
				t.Errorf("user notes lost:\n%s", body)
			}
			for _, want := range []string{"### 1. Think Before Coding", "### 2. Simplicity First", "### 3. Surgical Changes", "### 4. Goal-Driven Execution"} {
				if !strings.Contains(body, want) {
					t.Errorf("missing principle subheading %q:\n%s", want, body)
				}
			}
			for _, want := range []string{"CodeGraph FIRST", "blast radius", "tokless index", "do not retry"} {
				if !strings.Contains(body, want) {
					t.Errorf("missing Codegraph instruction %q:\n%s", want, body)
				}
			}
			if !strings.Contains(body, "call path") && !strings.Contains(body, "call paths") {
				t.Errorf("missing Codegraph instruction %q:\n%s", "call path(s)", body)
			}
		})
	}
}

func TestUnifiedBody_PerToolUnwire(t *testing.T) {
	setupHome(t)

	agentsList := []string{"claude", "opencode", "codex", "antigravity", "grok"}
	wireOrder := []string{"caveman", "codegraph", "context-mode", "ponytail"}

	for _, agent := range agentsList {
		t.Run(agent, func(t *testing.T) {
			path := agentInstructionPath(t, agent)
			_ = util.EnsureDir(filepath.Dir(path))
			_ = util.WriteFile(path, "")
			for _, tool := range wireOrder {
				tm := lookupTool(t, tool)
				_, _ = tm.WireFor[agent](core.RunOpts{})
			}
			for _, tool := range wireOrder {
				tm := lookupTool(t, tool)
				_, _ = tm.UnwireFor[agent](core.RunOpts{})
				raw, _ := os.ReadFile(path)
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

func TestUnifiedBody_PerToolUnwirePreservesOtherOwners(t *testing.T) {
	setupHome(t)
	for _, agent := range []string{"claude", "opencode", "codex", "antigravity", "grok"} {
		for _, tool := range []string{"caveman", "ponytail"} {
			t.Run(agent+"/"+tool, func(t *testing.T) {
				for _, owner := range []string{"codegraph", "context-mode"} {
					if !WriteOwner(agent, owner) {
						t.Fatalf("write %s owner", owner)
					}
				}
				tm := lookupTool(t, tool)
				if ok, err := tm.WireFor[agent](core.RunOpts{}); err != nil || !ok {
					t.Fatalf("wire %s/%s: ok=%v err=%v", agent, tool, ok, err)
				}
				if ok, err := tm.UnwireFor[agent](core.RunOpts{}); err != nil || !ok {
					t.Fatalf("unwire %s/%s: ok=%v err=%v", agent, tool, ok, err)
				}
				raw, err := os.ReadFile(agentInstructionPath(t, agent))
				if err != nil {
					t.Fatal(err)
				}
				for _, owner := range []string{"codegraph", "context-mode"} {
					if !strings.Contains(string(raw), util.SectionsByOwner[owner]) {
						t.Errorf("%s removed by %s/%s unwire", owner, agent, tool)
					}
				}
			})
		}
	}
}

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
	if !strings.Contains(body, "## Response Style (caveman)") {
		t.Errorf("unified Voice section missing:\n%s", body)
	}
	if !strings.Contains(body, "# User notes") {
		t.Errorf("user notes lost:\n%s", body)
	}
}

func TestUnifiedBody_AcceptsLegacyArrowHeadings(t *testing.T) {
	setupHome(t)
	path := filepath.Join(util.Home(), ".codex", "AGENTS.md")
	_ = util.EnsureDir(filepath.Dir(path))
	_ = util.WriteFile(path, "# User notes\n\n## Style\nold body\n")

	WriteOwner("codex", "context-mode")
	after, _ := os.ReadFile(path)
	body := string(after)
	if strings.Count(body, "## Response Style (caveman)") != 1 {
		t.Fatalf("legacy heading duplicated:\n%s", body)
	}
	if strings.Contains(body, "## Style") {
		t.Fatalf("legacy arrow heading kept:\n%s", body)
	}
	if !strings.Contains(body, "## Context Tools (context-mode)") {
		t.Fatalf("new owner missing:\n%s", body)
	}
}

func TestUnifiedBody_PiAndDroidInstructionPaths(t *testing.T) {
	setupHome(t)
	piDir := filepath.Join(util.Home(), "custom-pi")
	t.Setenv("PI_CODING_AGENT_DIR", piDir)

	for _, tc := range []struct {
		agent string
		tool  string
		path  string
	}{
		{"pi", "caveman", filepath.Join(piDir, "AGENTS.md")},
		{"droid", "ponytail", filepath.Join(util.Home(), ".factory", "AGENTS.md")},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			ok, err := lookupTool(t, tc.tool).WireFor[tc.agent](core.RunOpts{})
			if err != nil || !ok {
				t.Fatalf("wire %s/%s: ok=%v err=%v", tc.agent, tc.tool, ok, err)
			}
			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), util.SectionsByOwner[tc.tool]) {
				t.Fatalf("missing %s section:\n%s", tc.tool, raw)
			}
		})
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
	case "grok":
		return filepath.Join(util.Home(), ".grok", "rules", "AGENTS.md")
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
