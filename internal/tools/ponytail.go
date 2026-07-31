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
		ok := WriteOwner(agent, "ponytail")
		return ok, nil
	}
}

func ponytailWireCopilot() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		ok := WriteOwner("copilot", "ponytail")
		if ok {
			agents.SyncCopilotIdeInstructions()
		}
		return ok, nil
	}
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
	},
	Install: func(opts core.RunOpts) (bool, error) {
		return true, nil
	},
}
