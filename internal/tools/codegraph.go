package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

const codegraphIndexTimeout = 60 * time.Second

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
	case "droid":
		agents.ConfigureDroidMcp("codegraph")
	case "pi":
		agents.ConfigurePiMcp("codegraph")
	case "omp":
		changed, _ := agents.ConfigureOmpMcp("codegraph")
		return changed || agents.OmpMcpHas("codegraph")
	case "grok":
		_, _, err := agents.ConfigureGrokMcp("codegraph")
		return err == nil
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
		return agents.CopilotMcpHas("codegraph") && agents.HasCopilotCodegraphIndexHook() &&
			agents.CopilotIdeMcpHas("codegraph") && agents.HasCopilotIdeCodegraphIndexHook()
	case "droid":
		return agents.DroidMcpHas("codegraph") && agents.HasDroidCodegraphIndexHook()
	case "pi":
		return agents.PiMcpHas("codegraph") && piCodegraphIndexExtensionPresent()
	case "omp":
		return agents.OmpMcpHas("codegraph") && HasOwner("omp", "codegraph")
	case "grok":
		return agents.GrokMcpHas("codegraph") && HasOwner("grok", "codegraph")
	case "kilo":
		expected := util.WrapAutoIndex("kilo", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
		return agents.KiloMcpMatches("codegraph", append([]string{expected.Command}, expected.Args...)) && kiloHasOwner("codegraph")
	}
	return false
}

func codegraphIndexProject(dir string, opts core.RunOpts) (bool, error) {
	return RunCodegraphIndex(dir, opts)
}

// HasCodegraphIndex reports whether CodeGraph has an initialized project database.
func HasCodegraphIndex(dir string) bool {
	indexDir := strings.TrimSpace(os.Getenv("CODEGRAPH_DIR"))
	if indexDir == "" || indexDir == "." || strings.Contains(indexDir, "..") || strings.ContainsAny(indexDir, `/\`) {
		indexDir = ".codegraph"
	}
	return util.Exists(filepath.Join(dir, indexDir, "codegraph.db"))
}

// RunCodegraphIndex initializes or synchronizes CodeGraph before returning.
func RunCodegraphIndex(dir string, opts core.RunOpts) (bool, error) {
	if isTest() {
		_ = os.MkdirAll(filepath.Join(dir, ".codegraph"), 0o755)
		return true, nil
	}
	bin := util.ResolveCodegraphBin()
	if bin == "" {
		return false, fmt.Errorf("codegraph executable not found")
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would run codegraph in " + dir)
		return true, nil
	}
	return codegraphSync(bin, dir)
}

func codegraphSync(bin, dir string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), codegraphIndexTimeout)
	defer cancel()
	run := func(args ...string) util.ExecResult {
		command, commandArgs := codegraphRunCommand(bin, args...)
		return util.Run(command, commandArgs, util.RunOptions{Cwd: dir, Capture: true, Ctx: ctx})
	}
	if HasCodegraphIndex(dir) {
		if result := run("sync"); result.Code != 0 {
			return false, fmt.Errorf("codegraph sync failed%s", codegraphFailure(result.Stderr))
		}
		return true, nil
	}
	if result := run("init", "-i"); result.Code == 0 {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, fmt.Errorf("codegraph init failed: timed out")
	}
	if result := run("init"); result.Code != 0 {
		return false, fmt.Errorf("codegraph init failed%s", codegraphFailure(result.Stderr))
	}
	return true, nil
}

func codegraphRunCommand(bin string, args ...string) (string, []string) {
	if util.IsWin {
		ext := strings.ToLower(filepath.Ext(bin))
		if ext == ".cmd" || ext == ".bat" {
			return "cmd", append([]string{"/c", bin}, args...)
		}
	}
	return bin, args
}

func codegraphFailure(stderr string) string {
	if stderr = clip(stderr); stderr != "" {
		return ": " + stderr
	}
	return ""
}

// Pi auto-indexes once at session start without delaying the session.
var piCodegraphIndexTs = `import type { ExtensionAPI } from "@earendil-works/pi-coding-agent"
const TOKLESS = %s

export default function (pi: ExtensionAPI) {
  pi.on("session_start", () => {
    void pi.exec(TOKLESS, ["index", "--auto"], { timeout: 60_000 }).catch(() => {})
  })
}
`

func piCodegraphIndexPath() string {
	return filepath.Join(agents.PiAgentDirResolved(), "extensions", "codegraph-index.ts")
}

func writePiCodegraphIndexExtension() {
	_ = os.MkdirAll(filepath.Dir(piCodegraphIndexPath()), 0o755)
	_ = util.WriteFile(piCodegraphIndexPath(), piCodegraphIndexSource(util.ToklessAbs()))
}

func piCodegraphIndexSource(tokless string) string {
	encoded, _ := json.Marshal(tokless)
	return fmt.Sprintf(piCodegraphIndexTs, encoded)
}

func piCodegraphIndexExtensionPresent() bool {
	return util.Exists(piCodegraphIndexPath())
}

func codegraphWire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if agent == "kilo" {
			if opts.DryRun {
				return true, nil
			}
			if !agents.KiloProjectAvailable() {
				return false, nil
			}
			spawn := util.WrapAutoIndex("kilo", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
			agents.ConfigureKiloMcp("codegraph", append([]string{spawn.Command}, spawn.Args...))
			kiloWriteOwner("codegraph")
			return codegraphVerify("kilo"), nil
		}
		if isTest() {
			if !codegraphConfigureMcp(agent) {
				return false, nil
			}
			WriteOwner(agent, "codegraph")
			if agent == "grok" && !HasOwner(agent, "codegraph") {
				_, _ = agents.RemoveGrokMcp("codegraph")
				return false, nil
			}
			if agent == "antigravity" {
				agents.InstallAntigravityCodegraphIndexHook()
				agents.CleanupDeadIdeHooks()
			}
			if agent == "copilot" {
				agents.InstallCopilotCodegraphIndexHook()
				agents.InstallCopilotIdeCodegraphIndexHook()
				agents.ConfigureCopilotIdeMcp("codegraph")
			}
			if agent == "droid" {
				agents.InstallDroidCodegraphIndexHook()
			}
			return codegraphVerify(agent), nil
		}
		if opts.DryRun {
			return codegraphRealInstall(opts, agent), nil
		}
		if agent == "omp" {
			if !codegraphConfigureMcp(agent) {
				return false, nil
			}
			writeCodegraphBlock(agent)
			return codegraphVerify(agent), nil
		}
		if ran := codegraphRealInstall(opts, agent); !ran {
			util.L.Debug("codegraph's own installer failed; writing MCP entry directly")
		}
		if agent == "grok" {
			if !codegraphConfigureMcp(agent) {
				return false, nil
			}
		} else {
			codegraphConfigureMcp(agent)
		}
		writeCodegraphBlock(agent)
		if agent == "grok" && !HasOwner(agent, "codegraph") {
			_, _ = agents.RemoveGrokMcp("codegraph")
			return false, nil
		}
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
		if agent == "droid" {
			agents.InstallDroidCodegraphIndexHook()
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
	case "droid":
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
		"droid":       codegraphWire("droid"),
		"grok":        codegraphWire("grok"),
		"pi": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				util.L.Sub("[dry-run] would: purge legacy pi-codegraph pkgs, install pi-mcp-adapter, mcp.json + codegraph-index.ts")
				return true, nil
			}
			agents.PiPurgeCodegraphPackages()
			if !agents.PiInstallSource(agents.PiSrcMcpAdapter) {
				return false, nil
			}
			codegraphConfigureMcp("pi")
			writePiCodegraphIndexExtension()
			WriteOwner("pi", "codegraph")
			return codegraphVerify("pi"), nil
		},
		"omp":  codegraphWire("omp"),
		"kilo": codegraphWire("kilo"),
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
		"droid": func(core.RunOpts) (bool, error) {
			agents.RemoveDroidMcp("codegraph")
			agents.RemoveDroidCodegraphIndexHook()
			RemoveOwner("droid", "codegraph")
			return true, nil
		},
		"grok": func(core.RunOpts) (bool, error) {
			if _, err := agents.RemoveGrokMcp("codegraph"); err != nil {
				return false, err
			}
			RemoveOwner("grok", "codegraph")
			return true, nil
		},
		"pi": func(core.RunOpts) (bool, error) {
			agents.RemovePiMcp("codegraph")
			_ = os.Remove(piCodegraphIndexPath())
			if !agents.PiMcpHasAny() {
				agents.PiRemoveSource(agents.PiSrcMcpAdapter)
			}
			RemoveOwner("pi", "codegraph")
			return true, nil
		},
		"omp": func(core.RunOpts) (bool, error) {
			if !agents.RemoveOmpMcp("codegraph") {
				return false, nil
			}
			RemoveOwner("omp", "codegraph")
			return true, nil
		},
		"kilo": func(core.RunOpts) (bool, error) {
			agents.RemoveKiloMcp("codegraph")
			kiloRemoveOwner("codegraph")
			return true, nil
		},
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":      func() *bool { return core.BoolPtr(codegraphVerify("claude")) },
		"opencode":    func() *bool { return core.BoolPtr(codegraphVerify("opencode")) },
		"codex":       func() *bool { return core.BoolPtr(codegraphVerify("codex")) },
		"antigravity": func() *bool { return core.BoolPtr(codegraphVerify("antigravity")) },
		"copilot":     func() *bool { return core.BoolPtr(codegraphVerify("copilot")) },
		"droid":       func() *bool { return core.BoolPtr(codegraphVerify("droid")) },
		"grok":        func() *bool { return core.BoolPtr(codegraphVerify("grok")) },
		"pi":          func() *bool { return core.BoolPtr(codegraphVerify("pi")) },
		"omp":         func() *bool { return core.BoolPtr(codegraphVerify("omp")) },
		"kilo":        func() *bool { return core.BoolPtr(codegraphVerify("kilo")) },
	},
}
