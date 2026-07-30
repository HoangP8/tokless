package agents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

var grok = &core.AgentManifest{
	ID:        "grok",
	Label:     "Grok",
	Homepage:  "https://x.ai/grok",
	CLIBin:    "grok",
	ConfigDir: func() string { return filepath.Join(util.Home(), ".grok") },
	Detect: func() core.Detection {
		return detectAgent("grok", filepath.Join(util.Home(), ".grok"), nil, nil)
	},
}

// InstallGrokRtkHook writes the rtk PreToolUse hook into ~/.grok/hooks/rtk.json.
func InstallGrokRtkHook() {
	dir := filepath.Join(util.Home(), ".grok", "hooks")
	_ = os.MkdirAll(dir, 0o755)
	hookJSON := `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "` + toklessCommand("rtk-hook", "grok") + `",
            "timeout": 10
          }
        ]
      }
    ]
  }
}
`
	_ = os.WriteFile(filepath.Join(dir, "rtk.json"), []byte(hookJSON), 0o644)
}

// HasGrokRtkHook reports whether the rtk hook is installed for grok.
func HasGrokRtkHook() bool {
	hookPath := filepath.Join(util.Home(), ".grok", "hooks", "rtk.json")
	raw, err := os.ReadFile(hookPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "rtk-hook") || strings.Contains(string(raw), "rtk rewrite")
}
