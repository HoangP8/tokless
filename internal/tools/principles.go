package tools

import (
	"github.com/HoangP8/tokless/internal/core"
)

var principlesWireFor, principlesUnwireFor, principlesVerifyFor = skillAgentMaps("principles")

var principles = &core.ToolManifest{
	ID:              "principles",
	Label:           "Principles",
	Description:     "Meta-rules for thinking before coding, simplicity, surgical changes, and goal-driven execution.",
	Homepage:        "https://github.com/multica-ai/andrej-karpathy-skills",
	InstallHint:     "Instruction-only — synced from the upstream repo.",
	Channel:         core.ChannelSkill,
	InstructionOnly: true,
	// No releases or tags upstream, so the version is a commit SHA.
	Skill: &core.SkillSource{
		Repo:     "multica-ai/andrej-karpathy-skills",
		Path:     "CLAUDE.md",
		UseTag:   false,
		MaxBytes: skillMaxBytes,
	},
	Install:   skillInstall("principles"),
	WireFor:   principlesWireFor,
	UnwireFor: principlesUnwireFor,
	VerifyFor: principlesVerifyFor,
}
