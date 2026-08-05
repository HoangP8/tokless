package tools

import (
	"github.com/HoangP8/tokless/internal/agents"
	core "github.com/HoangP8/tokless/internal/core"
)

func ponytailWireFor(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		WriteOwner(agent, "ponytail")
		return HasOwner(agent, "ponytail"), nil
	}
}

func ponytailWireCopilot() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		WriteOwner("copilot", "ponytail")
		if HasOwner("copilot", "ponytail") {
			agents.SyncCopilotIdeInstructions()
		}
		return HasOwner("copilot", "ponytail"), nil
	}
}

func ponytailKiloWire() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		kiloWriteOwner("ponytail")
		return kiloHasOwner("ponytail"), nil
	}
}

func ponytailKiloUnwire(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	kiloRemoveOwner("ponytail")
	return true, nil
}

func ponytailKiloVerify() *bool {
	v := kiloHasOwner("ponytail")
	return &v
}

func ponytailUnwireFor(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		RemoveOwner(agent, "ponytail")
		return true, nil
	}
}

func ponytailVerifyFor(agent string) core.VerifyFn {
	return func() *bool {
		v := HasOwner(agent, "ponytail")
		return &v
	}
}

var ponytail = &core.ToolManifest{
	ID:              "ponytail",
	Label:           "Ponytail",
	Description:     "Minimal, lazy code generation (YAGNI).",
	Homepage:        "https://github.com/DietrichGebert/ponytail",
	InstallHint:     "Instruction-only — no install needed.",
	Channel:         core.ChannelGitHub,
	NotTrackable:    true,
	InstructionOnly: true,
	NeedsNode:       false,
	NeedsGit:        false,
	WireFor: map[string]core.AgentFn{
		"claude":      ponytailWireFor("claude"),
		"opencode":    ponytailWireFor("opencode"),
		"codex":       ponytailWireFor("codex"),
		"antigravity": ponytailWireFor("antigravity"),
		"copilot":     ponytailWireCopilot(),
		"droid":       ponytailWireFor("droid"),
		"grok":        ponytailWireFor("grok"),
		"pi":          ponytailWireFor("pi"),
		"omp":         ponytailWireFor("omp"),
		"kilo":        ponytailKiloWire(),
		"cline":       ponytailWireFor("cline"),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude":      ponytailUnwireFor("claude"),
		"opencode":    ponytailUnwireFor("opencode"),
		"codex":       ponytailUnwireFor("codex"),
		"antigravity": ponytailUnwireFor("antigravity"),
		"copilot":     ponytailUnwireFor("copilot"),
		"droid":       ponytailUnwireFor("droid"),
		"grok":        ponytailUnwireFor("grok"),
		"pi":          ponytailUnwireFor("pi"),
		"omp":         ponytailUnwireFor("omp"),
		"kilo":        ponytailKiloUnwire,
		"cline":       ponytailUnwireFor("cline"),
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude":      ponytailVerifyFor("claude"),
		"opencode":    ponytailVerifyFor("opencode"),
		"codex":       ponytailVerifyFor("codex"),
		"antigravity": ponytailVerifyFor("antigravity"),
		"copilot":     ponytailVerifyFor("copilot"),
		"droid":       ponytailVerifyFor("droid"),
		"grok":        ponytailVerifyFor("grok"),
		"pi":          ponytailVerifyFor("pi"),
		"omp":         ponytailVerifyFor("omp"),
		"kilo":        ponytailKiloVerify,
		"cline":       ponytailVerifyFor("cline"),
	},
	Install: func(opts core.RunOpts) (bool, error) {
		return true, nil
	},
}
