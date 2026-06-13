package tools

import (
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/util"
)

const autoIndexCmd = "tokless index --auto"

// --- Claude Code: settings.json hooks.SessionStart command hook ---

func wireClaudeAutoIndex() {
	cp := util.ClaudeCodePaths()
	_ = util.EnsureDir(cp.Dir)
	cfg := loadOrdered(cp.Settings)
	hooks := getOrCreateMapT(cfg, "hooks")
	if claudeHasAutoIndex(hooks) {
		return
	}
	cmd := util.NewOrderedMap()
	cmd.Set("type", "command")
	cmd.Set("command", autoIndexCmd)
	cmd.Set("timeout", 120)
	group := util.NewOrderedMap()
	group.Set("matcher", "startup")
	group.Set("hooks", []any{cmd})
	var ss []any
	if v, ok := hooks.Get("SessionStart"); ok {
		if existing, ok := v.([]any); ok {
			ss = existing
		}
	}
	ss = append(ss, group)
	hooks.Set("SessionStart", ss)
	cfg.Set("hooks", hooks)
	_ = util.WriteFile(cp.Settings, util.StringifyJSON(cfg))
}

func claudeHasAutoIndex(hooks *util.OrderedMap) bool {
	v, ok := hooks.Get("SessionStart")
	if !ok {
		return false
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	return groupsContainAutoIndex(arr)
}

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

// --- Codex: hooks.json hooks.SessionStart, merged with any existing hooks ---

func wireCodexAutoIndex() {
	cx := util.CodexPathsResolved()
	_ = util.EnsureDir(cx.Dir)
	hooksPath := filepath.Join(cx.Dir, "hooks.json")
	cfg := loadOrdered(hooksPath)
	hooks := getOrCreateMapT(cfg, "hooks")
	var ss []any
	if v, ok := hooks.Get("SessionStart"); ok {
		if existing, ok := v.([]any); ok {
			ss = existing
		}
	}
	if groupsContainAutoIndex(ss) {
		return
	}
	cmd := util.NewOrderedMap()
	cmd.Set("type", "command")
	cmd.Set("command", autoIndexCmd)
	cmd.Set("timeout", 120)
	group := util.NewOrderedMap()
	group.Set("matcher", "startup")
	group.Set("hooks", []any{cmd})
	ss = append(ss, group)
	hooks.Set("SessionStart", ss)
	cfg.Set("hooks", hooks)
	_ = util.WriteFile(hooksPath, util.StringifyJSON(cfg))
	enableCodexFeatureFlags(false)
}

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

// --- OpenCode: a flat plugin file that runs tokless index --auto on session.created ---

const opencodeAutoIndexPlugin = `// tokless: auto-build codegraph's per-project index on session start.
// Managed by tokless; safe to delete. Idempotent + guarded by 'tokless index --auto'.
import { spawn } from "child_process"

export const ToklessCodegraphInit = async ({ directory }) => {
  let done = false
  return {
    event: async ({ event }) => {
      if (done) return
      if (event?.type !== "session.created") return
      done = true
      try {
        spawn("tokless", ["index", "--auto"], { cwd: directory, stdio: "ignore", detached: true }).unref()
      } catch {}
    },
  }
}

export default ToklessCodegraphInit
`

func opencodeAutoIndexPath() string {
	return filepath.Join(util.OpenCodePathsResolved().Dir, "plugins", "tokless-codegraph-init.js")
}

func wireOpencodeAutoIndex() {
	p := opencodeAutoIndexPath()
	_ = util.EnsureDir(filepath.Dir(p))
	_ = util.WriteFile(p, opencodeAutoIndexPlugin)
}

func unwireOpencodeAutoIndex() {
	_ = os.Remove(opencodeAutoIndexPath())
}

// --- Antigravity / Gemini CLI: ~/.gemini/settings.json hooks.SessionStart ---

func geminiSettingsPath() string {
	return filepath.Join(util.Home(), ".gemini", "settings.json")
}

func wireGeminiAutoIndex() {
	p := geminiSettingsPath()
	_ = util.EnsureDir(filepath.Dir(p))
	cfg := loadOrdered(p)
	hooks := getOrCreateMapT(cfg, "hooks")
	var ss []any
	if v, ok := hooks.Get("SessionStart"); ok {
		if existing, ok := v.([]any); ok {
			ss = existing
		}
	}
	if groupsContainAutoIndex(ss) {
		return
	}
	cmd := util.NewOrderedMap()
	cmd.Set("type", "command")
	cmd.Set("command", autoIndexCmd)
	cmd.Set("timeout", 120000) // gemini hook timeouts are milliseconds
	group := util.NewOrderedMap()
	group.Set("matcher", "")
	group.Set("hooks", []any{cmd})
	ss = append(ss, group)
	hooks.Set("SessionStart", ss)
	cfg.Set("hooks", hooks)
	_ = util.WriteFile(p, util.StringifyJSON(cfg))
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
				if s, ok := c.(string); ok && s == autoIndexCmd {
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
