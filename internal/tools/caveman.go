package tools

import (
	"github.com/HoangP8/tokless/internal/agents"
	core "github.com/HoangP8/tokless/internal/core"
)

func cavemanWireFor(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		WriteOwner(agent, "caveman")
		return HasOwner(agent, "caveman"), nil
	}
}

func cavemanWireCopilot() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		WriteOwner("copilot", "caveman")
		if HasOwner("copilot", "caveman") {
			agents.SyncCopilotIdeInstructions()
		}
		return HasOwner("copilot", "caveman"), nil
	}
}

func cavemanKiloWire() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		kiloWriteOwner("caveman")
		return kiloHasOwner("caveman"), nil
	}
}

func cavemanKiloUnwire(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	kiloRemoveOwner("caveman")
	return true, nil
}

func cavemanKiloVerify() *bool {
	v := kiloHasOwner("caveman")
	return &v
}

func cavemanUnwireFor(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		RemoveOwner(agent, "caveman")
		return true, nil
	}
}

func cavemanVerifyFor(agent string) core.VerifyFn {
	return func() *bool {
		v := HasOwner(agent, "caveman")
		return &v
	}
}

var caveman = &core.ToolManifest{
	ID:              "caveman",
	Label:           "Caveman",
	Description:     "Compressed agent prompts through primitive English.",
	Homepage:        "https://github.com/JuliusBrussee/caveman",
	InstallHint:     "Instruction-only — no install needed.",
	Channel:         core.ChannelGitHub,
	NotTrackable:    true,
	InstructionOnly: true,
	NeedsNode:       false,
	NeedsGit:        false,
	WireFor: map[string]core.AgentFn{
		"claude":      cavemanWireFor("claude"),
		"opencode":    cavemanWireFor("opencode"),
		"codex":       cavemanWireFor("codex"),
		"antigravity": cavemanWireFor("antigravity"),
		"copilot":     cavemanWireCopilot(),
		"droid":       cavemanWireFor("droid"),
		"grok":        cavemanWireFor("grok"),
		"pi":          cavemanWireFor("pi"),
		"omp":         cavemanWireFor("omp"),
		"kilo":        cavemanKiloWire(),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":      cavemanUnwireFor("claude"),
		"opencode":    cavemanUnwireFor("opencode"),
		"codex":       cavemanUnwireFor("codex"),
		"antigravity": cavemanUnwireFor("antigravity"),
		"copilot":     cavemanUnwireFor("copilot"),
		"droid":       cavemanUnwireFor("droid"),
		"grok":        cavemanUnwireFor("grok"),
		"pi":          cavemanUnwireFor("pi"),
		"omp":         cavemanUnwireFor("omp"),
		"kilo":        cavemanKiloUnwire,
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":      cavemanVerifyFor("claude"),
		"opencode":    cavemanVerifyFor("opencode"),
		"codex":       cavemanVerifyFor("codex"),
		"antigravity": cavemanVerifyFor("antigravity"),
		"copilot":     cavemanVerifyFor("copilot"),
		"droid":       cavemanVerifyFor("droid"),
		"grok":        cavemanVerifyFor("grok"),
		"pi":          cavemanVerifyFor("pi"),
		"omp":         cavemanVerifyFor("omp"),
		"kilo":        cavemanKiloVerify,
	},
	Install: func(opts core.RunOpts) (bool, error) {
		return true, nil
	},
}
