package tools

import (
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// This text enters every session of every agent, so an upstream doc that
// balloons would cost the tokens tokless exists to save. All the built-in
// text together is ~8.5 KB.
const skillMaxBytes = 8192

func skillSpec(t *core.ToolManifest) util.VersionSpec {
	return util.VersionSpec{
		ID:       t.ID,
		Channel:  string(core.ChannelSkill),
		Repo:     t.Skill.Repo,
		SkillDoc: t.Skill.Path,
		UseTag:   t.Skill.UseTag,
		MaxBytes: t.Skill.MaxBytes,
	}
}

// skillInstall downloads the current text. Never fails the run — if the
// download doesn't work, the previous copy keeps being used.
func skillInstall(id string) func(core.RunOpts) (bool, error) {
	return func(opts core.RunOpts) (bool, error) {
		t := core.GetTool(id)
		if t == nil || t.Skill == nil {
			return true, nil
		}
		if opts.DryRun {
			util.L.Sub("[dry-run] would sync " + id + " from " + t.Skill.Repo)
			return true, nil
		}
		return util.SkillEnsure(skillSpec(t), opts.Report, opts.Upgrade)
	}
}
