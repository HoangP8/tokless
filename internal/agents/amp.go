package agents

import (
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

var amp = &core.AgentManifest{
	ID: "amp", Label: "Amp", Homepage: "https://ampcode.com", CLIBin: "amp",
	ConfigDir: func() string { return util.AmpPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("amp", util.AmpPathsResolved().Dir, util.ExpectedBinDirs(), nil)
	},
}

func init() { core.RegisterAgent(amp) }

func ampRtkRules(commands []string) []string {
	rules := make([]string, 0, len(commands))
	for _, prefix := range commands {
		rules = append(rules, "rtk "+prefix+" *")
	}
	return rules
}

func updateAmpRtkPermissions(remove bool, commands []string, commandListAvailable bool) bool {
	f := util.AmpPathsResolved().Config
	raw, _ := util.ReadFileSafe(f)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if raw != "" {
			return false
		}
		cfg = util.NewOrderedMap()
	}
	const permissionsKey = "amp.permissions"
	value, exists := cfg.Get(permissionsKey)
	permissions, ok := value.([]any)
	if exists && !ok {
		return false
	}

	// Amp permissions are priority-ordered (first match wins), so the rtk
	// allowlist must sit ahead of any user catch-all (e.g. an `ask` on `*`).
	// Every rtk rule is dropped from the kept set and re-emitted at the front;
	// non-rtk rules keep their relative order.
	wantRules := []string(nil)
	if !remove && commandListAvailable {
		wantRules = ampRtkRules(commands)
	}
	kept := make([]any, 0, len(permissions))
	for _, item := range permissions {
		if m, ok := item.(*util.OrderedMap); ok {
			tool, _ := m.Get("tool")
			action, _ := m.Get("action")
			matches, _ := m.Get("matches")
			cmd, _ := anyString(asOrdered(matches), "cmd")
			if tool == "Bash" && action == "allow" && strings.HasPrefix(cmd, "rtk ") {
				continue
			}
		}
		kept = append(kept, item)
	}
	front := make([]any, 0, len(wantRules))
	for _, cmd := range wantRules {
		rule := util.NewOrderedMap()
		rule.Set("tool", "Bash")
		matches := util.NewOrderedMap()
		matches.Set("cmd", cmd)
		rule.Set("matches", matches)
		rule.Set("action", "allow")
		front = append(front, rule)
	}
	next := append(front, kept...)
	changed := !ampPermissionsEqual(permissions, next)
	if changed || !exists {
		cfg.Set(permissionsKey, next)
		if util.WriteFile(f, util.StringifyJSON(cfg)) != nil {
			return false
		}
	}
	return changed
}

// ampPermissionsEqual compares two permission lists by their serialized form so
// a pure reordering (which changes first-match-wins semantics) counts as a change.
func ampPermissionsEqual(a, b []any) bool {
	wrap := func(list []any) string {
		m := util.NewOrderedMap()
		m.Set("permissions", list)
		return util.StringifyJSON(m)
	}
	return wrap(a) == wrap(b)
}

func anyString(m *util.OrderedMap, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m.Get(key)
	s, valid := v.(string)
	return s, ok && valid
}

var ampRtkCommandSource = util.RtkCommands

func ampRtkCommands() ([]string, bool) {
	commands, err := ampRtkCommandSource()
	return commands, err == nil
}
func configureAmpRtkPermissions(commands []string, available bool) (bool, string) {
	return updateAmpRtkPermissions(false, commands, available), util.AmpPathsResolved().Config
}
func ConfigureAmpRtkPermissions() (bool, string) {
	commands, ok := ampRtkCommands()
	return configureAmpRtkPermissions(commands, ok)
}
func RemoveAmpRtkPermissions() bool {
	commands, ok := ampRtkCommands()
	return updateAmpRtkPermissions(true, commands, ok)
}
func AmpRtkPermissionsWired() bool {
	commands, available := ampRtkCommands()
	return AmpRtkPermissionsWiredWith(commands, available)
}
func RemoveAmpRtkPermissionsWith(commands []string) bool {
	return updateAmpRtkPermissions(true, commands, true)
}
func AmpRtkPermissionsWiredWith(commands []string, available bool) bool {
	if !available || len(commands) == 0 {
		return false
	}
	raw, ok := util.ReadFileSafe(util.AmpPathsResolved().Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, _ := cfg.Get("amp.permissions")
	rules, ok := v.([]any)
	if !ok {
		return false
	}
	found := map[string]bool{}
	for _, item := range rules {
		m, ok := item.(*util.OrderedMap)
		if !ok {
			continue
		}
		t, _ := m.Get("tool")
		a, _ := m.Get("action")
		mm, _ := m.Get("matches")
		cmd, _ := anyString(asOrdered(mm), "cmd")
		if t == "Bash" && a == "allow" && strings.HasPrefix(cmd, "rtk ") {
			if cmd == "rtk *" || !containsAmpRtkCommand(ampRtkRules(commands), cmd) {
				return false
			}
			found[cmd] = true
		}
	}
	for _, cmd := range ampRtkRules(commands) {
		if !found[cmd] {
			return false
		}
	}
	return len(found) == len(commands)
}
func containsAmpRtkCommand(commands []string, command string) bool {
	for _, candidate := range commands {
		if candidate == command {
			return true
		}
	}
	return false
}
func asOrdered(v any) *util.OrderedMap { m, _ := v.(*util.OrderedMap); return m }
