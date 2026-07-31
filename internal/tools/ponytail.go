package tools

import (
	core "github.com/HoangP8/tokless/internal/core"
)

var ponytailWireFor, ponytailUnwireFor, ponytailVerifyFor = skillAgentMaps("ponytail")

var ponytail = &core.ToolManifest{
	ID:              "ponytail",
	Label:           "Ponytail",
	Description:     "Minimal, lazy code generation (YAGNI).",
	Homepage:        "https://github.com/DietrichGebert/ponytail",
	InstallHint:     "Instruction-only — synced from the upstream repo.",
	Channel:         core.ChannelSkill,
	InstructionOnly: true,
	// AGENTS.md is the short version; skills/ponytail/SKILL.md says the same in 2.5x the space.
	Skill: &core.SkillSource{
		Repo:     "DietrichGebert/ponytail",
		Path:     "AGENTS.md",
		UseTag:   true,
		MaxBytes: skillMaxBytes,
	},
	Install:   skillInstall("ponytail"),
	WireFor:   ponytailWireFor,
	UnwireFor: ponytailUnwireFor,
	VerifyFor: ponytailVerifyFor,
}
