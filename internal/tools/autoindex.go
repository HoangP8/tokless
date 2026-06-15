package tools

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// autoIndexCmd is the legacy SessionStart command prefix we clean up.
const autoIndexCmd = "tokless index --auto"

// --- Claude Code: settings.json hooks.SessionStart ---

func unwireClaudeAutoIndex() {
	cp := util.ClaudeCodePaths()
	if !util.Exists(cp.Settings) {
		return
	}
	cfg := loadOrdered(cp.Settings)
	hv, ok := cfg.Get("hooks")
	if !ok {
		return
	}
	hooks, ok := hv.(*util.OrderedMap)
	if !ok {
		return
	}
	if removeAutoIndexGroups(hooks) {
		if hooks.Len() == 0 {
			cfg.Delete("hooks")
		} else {
			cfg.Set("hooks", hooks)
		}
		_ = util.WriteFile(cp.Settings, util.StringifyJSON(cfg))
	}
}

// --- Codex: hooks.json hooks.SessionStart ---

func unwireCodexAutoIndex() {
	cx := util.CodexPathsResolved()
	hooksPath := filepath.Join(cx.Dir, "hooks.json")
	if !util.Exists(hooksPath) {
		return
	}
	cfg := loadOrdered(hooksPath)
	hv, ok := cfg.Get("hooks")
	if !ok {
		return
	}
	hooks, ok := hv.(*util.OrderedMap)
	if !ok {
		return
	}
	if removeAutoIndexGroups(hooks) {
		if hooks.Len() == 0 {
			_ = os.Remove(hooksPath)
		} else {
			cfg.Set("hooks", hooks)
			_ = util.WriteFile(hooksPath, util.StringifyJSON(cfg))
		}
	}
}

// --- OpenCode: legacy plugin file ---

func opencodeAutoIndexPath() string {
	return filepath.Join(util.OpenCodePathsResolved().Dir, "plugins", "tokless-codegraph-init.js")
}

func unwireOpencodeAutoIndex() {
	_ = os.Remove(opencodeAutoIndexPath())
	if win := util.WindowsHomeFromWSL(); win != "" {
		_ = os.Remove(filepath.Join(win, ".config", "opencode", "plugins", "tokless-codegraph-init.js"))
	}
}

// --- Antigravity / Gemini CLI: ~/.gemini/settings.json hooks.SessionStart ---

func geminiSettingsPath() string {
	return filepath.Join(util.Home(), ".gemini", "settings.json")
}

func unwireGeminiAutoIndex() {
	p := geminiSettingsPath()
	if !util.Exists(p) {
		return
	}
	cfg := loadOrdered(p)
	hv, ok := cfg.Get("hooks")
	if !ok {
		return
	}
	hooks, ok := hv.(*util.OrderedMap)
	if !ok {
		return
	}
	if removeAutoIndexGroups(hooks) {
		if hooks.Len() == 0 {
			cfg.Delete("hooks")
		} else {
			cfg.Set("hooks", hooks)
		}
		_ = util.WriteFile(p, util.StringifyJSON(cfg))
	}
}

// --- shared group helpers (a "group" is {matcher, hooks:[{command}]}) ---

func groupsContainAutoIndex(groups []any) bool {
	for _, g := range groups {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			continue
		}
		hv, ok := gm.Get("hooks")
		if !ok {
			continue
		}
		inner, ok := hv.([]any)
		if !ok {
			continue
		}
		for _, h := range inner {
			hm, ok := h.(*util.OrderedMap)
			if !ok {
				continue
			}
			if c, ok := hm.Get("command"); ok {
				if s, ok := c.(string); ok && strings.HasPrefix(s, autoIndexCmd) {
					return true
				}
			}
		}
	}
	return false
}

// removeAutoIndexGroups drops our SessionStart groups; returns true if changed.
func removeAutoIndexGroups(hooks *util.OrderedMap) bool {
	v, ok := hooks.Get("SessionStart")
	if !ok {
		return false
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	var kept []any
	for _, g := range arr {
		if gm, ok := g.(*util.OrderedMap); ok && groupsContainAutoIndex([]any{gm}) {
			continue
		}
		kept = append(kept, g)
	}
	if len(kept) == len(arr) {
		return false
	}
	if len(kept) == 0 {
		hooks.Delete("SessionStart")
	} else {
		hooks.Set("SessionStart", kept)
	}
	return true
}
