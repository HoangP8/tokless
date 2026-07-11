package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func codegraphEnsureInstalled(opts core.RunOpts) (bool, error) {
	if isTest() {
		return true, nil
	}
	opts.Reportf("checking", 0.1)
	if util.ResolveCodegraphBin() != "" && !opts.Upgrade {
		opts.Reportf("already installed", 1)
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would install @colbymchenry/codegraph globally")
		return true, nil
	}
	opts.Reportf("npm install -g", 0.4)
	_, ok, _ := util.NpmGlobalInstall("@colbymchenry/codegraph", "latest")
	ok = ok && util.ResolveCodegraphBin() != ""
	if ok {
		opts.Reportf("ready", 1)
	}
	return ok, nil
}

var (
	realInstallOnce sync.Once
	realInstallRes  bool
)

// codegraphRealInstall runs `codegraph install --target <agent>` per call.
func codegraphRealInstall(opts core.RunOpts, agent string) bool {
	if opts.DryRun {
		util.L.Sub("[dry-run] would run: codegraph install --yes")
		return true
	}
	bin := util.ResolveCodegraphBin()
	if bin == "" {
		return false
	}
	helpCtx, helpCancel := context.WithTimeout(context.Background(), 30*time.Second)
	help := util.Run(bin, []string{"install", "--help"}, util.RunOptions{Capture: true, Ctx: helpCtx})
	helpCancel()
	hasYes := strings.Contains(help.Stdout, "--yes") || strings.Contains(help.Stderr, "--yes")
	hasTarget := strings.Contains(help.Stdout, "--target") || strings.Contains(help.Stderr, "--target")
	args := []string{"install"}
	if hasYes {
		args = append(args, "--yes")
	}
	if hasTarget {
		target := agent
		if target == "antigravity" {
			target = "gemini"
		}
		if target == "" {
			target = "all"
		}
		args = append(args, "--target", target)
	}
	instCtx, instCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer instCancel()
	return util.Run(bin, args, util.RunOptions{Capture: true, Ctx: instCtx}).Code == 0
}

// codegraphConfigureMcp writes the MCP entry tokless-side.
func codegraphConfigureMcp(agent string) bool {
	switch agent {
	case "claude":
		agents.ConfigureClaudeMcp("codegraph")
	case "opencode":
		agents.ConfigureOpenCodeMcp("codegraph")
	case "codex":
		agents.ConfigureCodexMcp("codegraph")
	case "antigravity":
		agents.ConfigureAntigravityMcp("codegraph")
	case "copilot":
		agents.ConfigureCopilotMcp("codegraph")
	}
	return true
}

func codegraphVerify(agent string) bool {
	switch agent {
	case "claude":
		cp := util.ClaudeCodePaths()
		raw, ok := util.ReadFileSafe(cp.GlobalJSON)
		if !ok {
			return false
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			return false
		}
		if s, ok := cfg.Get("mcpServers"); ok {
			if sm, ok := s.(*util.OrderedMap); ok {
				_, has := sm.Get("codegraph")
				return has
			}
		}
		return false
	case "opencode":
		op := util.OpenCodePathsResolved()
		raw, ok := util.ReadFileSafe(op.Config)
		if !ok {
			return false
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			return false
		}
		if m, ok := cfg.Get("mcp"); ok {
			if mm, ok := m.(*util.OrderedMap); ok {
				_, has := mm.Get("codegraph")
				return has
			}
		}
		return false
	case "codex":
		cx := util.CodexPathsResolved()
		raw, _ := util.ReadFileSafe(cx.Config)
		return strings.Contains(raw, "[mcp_servers.codegraph]")
	case "antigravity":
		agents.CleanupDeadIdeHooks()
		return agents.AntigravityMcpHas("codegraph") && agents.HasAntigravityCodegraphIndexHook()
	case "copilot":
		return agents.CopilotMcpHas("codegraph") && agents.HasCopilotCodegraphIndexHook()
	}
	return false
}

func codegraphIndexProject(dir string, opts core.RunOpts) (bool, error) {
	if isTest() {
		_ = os.MkdirAll(filepath.Join(dir, ".codegraph"), 0o755)
		return true, nil
	}
	bin := util.ResolveCodegraphBin()
	if bin == "" {
		return false, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would run codegraph in " + dir)
		return true, nil
	}
	go codegraphSyncBackground(bin, dir)
	return true, nil
}

func codegraphSyncBackground(bin, dir string) {
	if util.Exists(filepath.Join(dir, ".codegraph")) {
		_ = util.Run(bin, []string{"sync"}, util.RunOptions{Cwd: dir, Quiet: true})
		return
	}
	if util.Run(bin, []string{"init", "-i"}, util.RunOptions{Cwd: dir, Quiet: true}).Code != 0 {
		_ = util.Run(bin, []string{"init"}, util.RunOptions{Cwd: dir, Quiet: true})
	}
}

func codegraphWire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if isTest() {
			codegraphConfigureMcp(agent)
			WriteOwner(agent, "codegraph")
			if agent == "antigravity" {
				agents.InstallAntigravityCodegraphIndexHook()
				agents.CleanupDeadIdeHooks()
			}
		if agent == "copilot" {
			agents.InstallCopilotCodegraphIndexHook()
			agents.InstallCopilotIdeCodegraphIndexHook()
		}
			return codegraphVerify(agent), nil
		}
		if opts.DryRun {
			return codegraphRealInstall(opts, agent), nil
		}
		if ran := codegraphRealInstall(opts, agent); !ran {
			util.L.Debug("codegraph's own installer failed; writing MCP entry directly")
		}
		codegraphConfigureMcp(agent)
		writeCodegraphBlock(agent)
		unwireAutoIndex(agent)
		if agent == "antigravity" {
			agents.InstallAntigravityCodegraphIndexHook()
			agents.CleanupDeadIdeHooks()
		}
		if agent == "copilot" {
			agents.InstallCopilotCodegraphIndexHook()
			agents.InstallCopilotIdeCodegraphIndexHook()
			agents.ConfigureCopilotIdeMcp("codegraph")
			agents.SyncCopilotIdeInstructions()
		}
		return codegraphVerify(agent), nil
	}
}

// writeCodegraphBlock writes the unified TOKLESS block with codegraph as one
// of its owners.
func writeCodegraphBlock(agent string) bool {
	return WriteOwner(agent, "codegraph")
}

func unwireAutoIndex(agent string) {
	switch agent {
	case "claude":
		unwireClaudeAutoIndex()
	case "codex":
		unwireCodexAutoIndex()
	case "opencode":
		unwireOpencodeAutoIndex()
	case "antigravity":
		unwireGeminiAutoIndex()
	case "copilot":
	}
}

var codegraph = &core.ToolManifest{
	ID:           "codegraph",
	Label:        "CodeGraph",
	Description:  "MCP server that lets agents query a code knowledge graph instead of reading raw files.",
	Homepage:     "https://github.com/colbymchenry/codegraph",
	InstallHint:  "npm i -g @colbymchenry/codegraph",
	Channel:      core.ChannelNpm,
	Install:      codegraphEnsureInstalled,
		IndexProject: codegraphIndexProject,
	IndexReady:   func() bool { return isTest() || util.ResolveCodegraphBin() != "" },
	WireFor: map[string]core.AgentFn{
		"claude":      codegraphWire("claude"),
		"opencode":    codegraphWire("opencode"),
		"codex":       codegraphWire("codex"),
		"antigravity": codegraphWire("antigravity"),
		"copilot":     codegraphWire("copilot"),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude": func(core.RunOpts) (bool, error) {
			agents.RemoveClaudeMcp("codegraph")
			unwireAutoIndex("claude")
			RemoveOwner("claude", "codegraph")
			return true, nil
		},
		"opencode": func(core.RunOpts) (bool, error) {
			agents.RemoveOpenCodeMcp("codegraph")
			unwireAutoIndex("opencode")
			RemoveOwner("opencode", "codegraph")
			return true, nil
		},
		"codex": func(core.RunOpts) (bool, error) {
			cx := util.CodexPathsResolved()
			raw, ok := util.ReadFileSafe(cx.Config)
			if ok {
				next := util.RemoveBlock(raw, "mcp_servers.codegraph")
				if next != raw {
					_ = util.WriteFile(cx.Config, next)
				}
			}
			unwireAutoIndex("codex")
			RemoveOwner("codex", "codegraph")
			return true, nil
		},
		"antigravity": func(core.RunOpts) (bool, error) {
			agents.RemoveAntigravityMcp("codegraph")
			agents.RemoveAntigravityCodegraphIndexHook()
			unwireAutoIndex("antigravity")
			agents.CleanupDeadIdeHooks()
			agents.RemoveAntigravityCodegraphToolDefs()
			RemoveOwner("antigravity", "codegraph")
			return true, nil
		},
		"copilot": func(core.RunOpts) (bool, error) {
			agents.RemoveCopilotMcp("codegraph")
			agents.RemoveCopilotCodegraphIndexHook()
			agents.RemoveCopilotIdeMcp("codegraph")
			agents.RemoveCopilotIdeCodegraphIndexHook()
			unwireAutoIndex("copilot")
			RemoveOwner("copilot", "codegraph")
			return true, nil
		},
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":      func() *bool { return core.BoolPtr(codegraphVerify("claude")) },
		"opencode":    func() *bool { return core.BoolPtr(codegraphVerify("opencode")) },
		"codex":       func() *bool { return core.BoolPtr(codegraphVerify("codex")) },
		"antigravity": func() *bool { return core.BoolPtr(codegraphVerify("antigravity")) },
		"copilot":     func() *bool { return core.BoolPtr(codegraphVerify("copilot")) },
	},
}
