package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func codegraphEnsureInstalled(opts core.RunOpts) (bool, error) {
	if isTest() {
		dest := filepath.Join(util.Home(), ".local", "bin")
		_ = os.MkdirAll(dest, 0o755)
		cgPath := filepath.Join(dest, "codegraph")
		_ = os.Remove(cgPath)
		_ = os.WriteFile(cgPath, []byte("#!/bin/sh\necho ok"), 0o755)
		sep := ":"
		if util.IsWin {
			sep = ";"
		}
		cur := os.Getenv("PATH")
		if !strings.Contains(sep+cur+sep, sep+dest+sep) {
			os.Setenv("PATH", dest+sep+cur)
		}
		return true, nil
	}
	opts.Reportf("checking", 0.1)
	if util.Which("codegraph") != "" && !opts.Upgrade {
		opts.Reportf("already installed", 1)
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would install @colbymchenry/codegraph globally")
		return true, nil
	}
	opts.Reportf("npm install -g", 0.4)
	_, ok := util.NpmGlobalInstall("@colbymchenry/codegraph", "latest")
	if ok {
		opts.Reportf("ready", 1)
	}
	return ok, nil
}

var (
	realInstallOnce sync.Once
	realInstallRes  bool
)

// callRealInstall runs `codegraph install` once, probing supported flags.
func codegraphRealInstall(opts core.RunOpts) bool {
	if opts.DryRun {
		util.L.Sub("[dry-run] would run: codegraph install --yes")
		return true
	}
	realInstallOnce.Do(func() {
		help := util.Run("codegraph", []string{"install", "--help"}, util.RunOptions{Capture: true})
		hasYes := strings.Contains(help.Stdout, "--yes") || strings.Contains(help.Stderr, "--yes")
		hasTarget := strings.Contains(help.Stdout, "--target") || strings.Contains(help.Stderr, "--target")
		args := []string{"install"}
		if hasYes {
			args = append(args, "--yes")
		}
		if hasTarget {
			args = append(args, "--target", "all")
		}
		realInstallRes = util.Run("codegraph", args, util.RunOptions{Capture: true}).Code == 0
	})
	return realInstallRes
}

func codegraphTestShim(agent string) bool {
	switch agent {
	case "claude":
		agents.ConfigureClaudeMcp("codegraph")
	case "opencode":
		agents.ConfigureOpenCodeMcp("codegraph")
	case "codex":
		agents.ConfigureCodexMcp("codegraph")
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
	}
	return false
}

func codegraphWire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if isTest() {
			return codegraphTestShim(agent), nil
		}
		if opts.DryRun {
			return codegraphRealInstall(opts), nil
		}
		ran := codegraphRealInstall(opts)
		return ran && codegraphVerify(agent), nil
	}
}

var codegraph = &core.ToolManifest{
	ID:          "codegraph",
	Label:       "CodeGraph",
	Description: "MCP server that lets agents query a code knowledge graph instead of reading raw files.",
	Homepage:    "https://github.com/colbymchenry/codegraph",
	InstallHint: "npm i -g @colbymchenry/codegraph",
	Channel:     core.ChannelNpm,
	Install:     codegraphEnsureInstalled,
	WireFor: map[string]core.AgentFn{
		"claude":   codegraphWire("claude"),
		"opencode": codegraphWire("opencode"),
		"codex":    codegraphWire("codex"),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":   func(core.RunOpts) (bool, error) { agents.RemoveClaudeMcp("codegraph"); return true, nil },
		"opencode": func(core.RunOpts) (bool, error) { agents.RemoveOpenCodeMcp("codegraph"); return true, nil },
		"codex": func(core.RunOpts) (bool, error) {
			cx := util.CodexPathsResolved()
			raw, ok := util.ReadFileSafe(cx.Config)
			if !ok {
				return true, nil
			}
			next := util.RemoveBlock(raw, "mcp_servers.codegraph")
			if next != raw {
				_ = util.WriteFile(cx.Config, next)
			}
			return true, nil
		},
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":   func() *bool { return core.BoolPtr(codegraphVerify("claude")) },
		"opencode": func() *bool { return core.BoolPtr(codegraphVerify("opencode")) },
		"codex":    func() *bool { return core.BoolPtr(codegraphVerify("codex")) },
	},
}
