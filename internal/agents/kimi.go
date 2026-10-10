package agents

import (
	"path/filepath"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// kimi is the Kimi Code harness. Its provider config is TOML; see kimi_byok.go
// for the BYOK gateway wiring.
var kimi = &core.AgentManifest{
	ID:        "kimi",
	Label:     "Kimi Code",
	Homepage:  "https://github.com/MoonshotAI/kimi-code",
	CLIBin:    "kimi",
	ConfigDir: func() string { return util.KimiPathsResolved().Dir },
	Detect: func() core.Detection {
		known := append(util.ExpectedBinDirs(), filepath.Join(util.KimiPathsResolved().Dir, "bin"))
		return detectAgent("kimi", util.KimiPathsResolved().Dir, known, nil)
	},
}
