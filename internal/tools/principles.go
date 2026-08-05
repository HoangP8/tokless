package tools

import (
	"path/filepath"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func kiloInstructionsPath() string { return agents.KiloInstructionsPath() }
func kiloWriteOwner(owner string) bool {
	path := kiloInstructionsPath()
	if path == "" {
		return false
	}
	_ = util.EnsureDir(filepath.Dir(path))
	cur, _ := util.ReadFileSafe(path)
	return writeOwnerInPath(path, cur, owner)
}
func kiloRemoveOwner(owner string) {
	path := kiloInstructionsPath()
	if path == "" {
		return
	}
	if cur, ok := util.ReadFileSafe(path); ok {
		removeOwnerInPath(path, cur, owner)
	}
}
func kiloHasOwner(owner string) bool {
	path := kiloInstructionsPath()
	return path != "" && hasOwnerAtPath(path, owner)
}

func principlesWireFor(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would add principles section to " + agent)
			return true, nil
		}
		_ = WriteOwner(agent, "principles")
		return principlesVerifyFor(agent)(), nil
	}
}

func principlesUnwireFor(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		RemoveOwner(agent, "principles")
		return true, nil
	}
}

func principlesVerifyFor(agent string) func() bool {
	return func() bool { return HasOwner(agent, "principles") }
}

var principles = &core.ToolManifest{
	ID:              "principles",
	Label:           "Principles",
	Description:     "Meta-rules for thinking before coding, simplicity, surgical changes, and goal-driven execution.",
	Homepage:        "https://github.com/multica-ai/andrej-karpathy-skills",
	InstallHint:     "Instruction-only — no install needed.",
	Channel:         core.ChannelGitHub,
	NotTrackable:    true,
	InstructionOnly: true,
	Install: func(opts core.RunOpts) (bool, error) {
		opts.Reportf("instruction-only", 1)
		return true, nil
	},
	WireFor: map[string]core.AgentFn{
		"claude":      principlesWireFor("claude"),
		"opencode":    principlesWireFor("opencode"),
		"codex":       principlesWireFor("codex"),
		"antigravity": principlesWireFor("antigravity"),
		"copilot": func(opts core.RunOpts) (bool, error) {
			ok, err := principlesWireFor("copilot")(opts)
			if ok {
				agents.SyncCopilotIdeInstructions()
			}
			return ok, err
		},
		"kilo": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			kiloWriteOwner("principles")
			return kiloHasOwner("principles"), nil
		},
		"cline": principlesWireFor("cline"),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":      principlesUnwireFor("claude"),
		"opencode":    principlesUnwireFor("opencode"),
		"codex":       principlesUnwireFor("codex"),
		"antigravity": principlesUnwireFor("antigravity"),
		"copilot":     principlesUnwireFor("copilot"),
		"kilo": func(core.RunOpts) (bool, error) {
			kiloRemoveOwner("principles")
			return true, nil
		},
		"cline": principlesUnwireFor("cline"),
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":      func() *bool { v := principlesVerifyFor("claude")(); return &v },
		"opencode":    func() *bool { v := principlesVerifyFor("opencode")(); return &v },
		"codex":       func() *bool { v := principlesVerifyFor("codex")(); return &v },
		"antigravity": func() *bool { v := principlesVerifyFor("antigravity")(); return &v },
		"copilot":     func() *bool { v := principlesVerifyFor("copilot")(); return &v },
		"kilo": func() *bool {
			return core.BoolPtr(kiloHasOwner("principles"))
		},
		"cline": func() *bool { v := principlesVerifyFor("cline")(); return &v },
	},
}
