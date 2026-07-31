package tools

import (
	core "github.com/HoangP8/tokless/internal/core"
)

var cavemanWireFor, cavemanUnwireFor, cavemanVerifyFor = skillAgentMaps("caveman")

var caveman = &core.ToolManifest{
	ID:              "caveman",
	Label:           "Caveman",
	Description:     "Compressed agent prompts through primitive English.",
	Homepage:        "https://github.com/JuliusBrussee/caveman",
	InstallHint:     "Instruction-only — synced from the upstream repo.",
	Channel:         core.ChannelSkill,
	InstructionOnly: true,
	// Root CLAUDE.md is 24 KB of docs about the repo itself — wrong file.
	Skill: &core.SkillSource{
		Repo:     "JuliusBrussee/caveman",
		Path:     "skills/caveman/SKILL.md",
		UseTag:   true,
		MaxBytes: skillMaxBytes,
	},
	Install:   skillInstall("caveman"),
	WireFor:   cavemanWireFor,
	UnwireFor: cavemanUnwireFor,
	VerifyFor: cavemanVerifyFor,
}
