package tools

import (
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

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
		"copilot":     principlesWireFor("copilot"),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":      principlesUnwireFor("claude"),
		"opencode":    principlesUnwireFor("opencode"),
		"codex":       principlesUnwireFor("codex"),
		"antigravity": principlesUnwireFor("antigravity"),
		"copilot":     principlesUnwireFor("copilot"),
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":      func() *bool { v := principlesVerifyFor("claude")(); return &v },
		"opencode":    func() *bool { v := principlesVerifyFor("opencode")(); return &v },
		"codex":       func() *bool { v := principlesVerifyFor("codex")(); return &v },
		"antigravity": func() *bool { v := principlesVerifyFor("antigravity")(); return &v },
		"copilot":     func() *bool { v := principlesVerifyFor("copilot")(); return &v },
	},
}
