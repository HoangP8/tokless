package agents

import (
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// zcode is the ZCode harness. Its desktop/CLI config is documented for MCP
// servers only; the provider schema is not, so proxy support is detection-only
// (see zcode_byok.go).
var zcode = &core.AgentManifest{
	ID:        "zcode",
	Label:     "ZCode",
	Homepage:  "https://github.com/zai-org/ZCode",
	CLIBin:    "zcode",
	ConfigDir: func() string { return util.ZcodePathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("zcode", util.ZcodePathsResolved().Dir, util.ExpectedBinDirs(), nil)
	},
}
