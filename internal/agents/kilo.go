package agents

import (
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func kiloProjectDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func KiloProjectFile(name ...string) string {
	root := kiloProjectDir()
	if root == "" {
		return ""
	}
	parts := append([]string{root, ".kilo"}, name...)
	return filepath.Join(parts...)
}

func KiloProjectAvailable() bool { return kiloProjectDir() != "" }

func KiloProjectConfigPath() string { return KiloProjectFile("kilo.jsonc") }

func KiloInstructionsPath() string {
	return filepath.Join(util.KiloPathsResolved().Dir, "AGENTS.md")
}

const kiloCreatedMarker = ".tokless-kilo-config-created"

func kiloConfigWasAbsent(config string) bool {
	if config == "" {
		return false
	}
	_, err := os.Stat(config)
	return os.IsNotExist(err)
}

func markKiloConfigCreated(config string) {
	if config == "" {
		return
	}
	_ = util.WriteFile(filepath.Join(filepath.Dir(config), kiloCreatedMarker), "tokless\n")
}

func ConfigureKiloMcp(toolID string, command []string) (bool, string) {
	p := KiloProjectConfigPath()
	if p == "" {
		return false, ""
	}
	created := kiloConfigWasAbsent(p)
	raw, _ := util.ReadFileSafe(p)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://app.kilo.ai/config.json")
	}
	mcp := mapForKilo(cfg, "mcp")
	desired := util.NewOrderedMap()
	desired.Set("type", "local")
	desired.Set("command", toAny(command))
	desired.Set("enabled", true)
	if existing, ok := mcp.Get(toolID); ok && kiloMcpEqual(existing, command) {
		return false, p
	}
	mcp.Set(toolID, desired)
	if err := util.WriteFile(p, util.StringifyJSON(cfg)); err != nil {
		return false, p
	}
	if created {
		markKiloConfigCreated(p)
	}
	return true, p
}

func RemoveKiloMcp(toolID string) bool {
	p := KiloProjectConfigPath()
	if p == "" {
		return false
	}
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcp")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	if _, ok := mcp.Get(toolID); !ok {
		return false
	}
	mcp.Delete(toolID)
	if mcp.Len() == 0 {
		cfg.Delete("mcp")
	}
	_ = util.WriteFile(p, util.StringifyJSON(cfg))
	return true
}

func KiloMcpConfigured(toolID string) bool {
	p := KiloProjectConfigPath()
	if p == "" {
		return false
	}
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcp")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	entry, ok := mcp.Get(toolID)
	if !ok {
		return false
	}
	em, ok := entry.(*util.OrderedMap)
	if !ok {
		return false
	}
	t, tok := em.Get("type")
	e, eok := em.Get("enabled")
	c, cok := em.Get("command")
	command, cok2 := c.([]any)
	return tok && t == "local" && eok && e == true && cok && cok2 && len(command) > 0 && allStringsNonempty(command)
}

func KiloMcpMatches(toolID string, want []string) bool {
	p := KiloProjectConfigPath()
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcp")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	entry, ok := mcp.Get(toolID)
	if !ok || !KiloMcpConfigured(toolID) {
		return false
	}
	em, ok := entry.(*util.OrderedMap)
	if !ok {
		return false
	}
	c, ok := em.Get("command")
	return ok && anyStringSliceEqual(c, want)
}

func allStringsNonempty(values []any) bool {
	for _, value := range values {
		s, ok := value.(string)
		if !ok || s == "" {
			return false
		}
	}
	return true
}

// CleanupKiloProject removes only an empty project config created by Tokless.
func CleanupKiloProject() {
	config := KiloProjectConfigPath()
	if config == "" {
		return
	}
	marker := filepath.Join(filepath.Dir(config), kiloCreatedMarker)
	if _, err := os.Stat(marker); err != nil {
		return
	}
	raw, ok := util.ReadFileSafe(config)
	if !ok || hasJSONCComment(raw) {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	for _, key := range cfg.Keys() {
		if key != "$schema" {
			return
		}
	}
	if err := os.Remove(config); err != nil {
		return
	}
	_ = os.Remove(marker)
	_ = os.Remove(filepath.Dir(config))
}

func hasJSONCComment(raw string) bool {
	inString, escaped := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		} else if c == '/' && i+1 < len(raw) && (raw[i+1] == '/' || raw[i+1] == '*') {
			return true
		}
	}
	return false
}

func mapForKilo(cfg *util.OrderedMap, key string) *util.OrderedMap {
	if v, ok := cfg.Get(key); ok {
		if m, ok := v.(*util.OrderedMap); ok {
			return m
		}
	}
	m := util.NewOrderedMap()
	cfg.Set(key, m)
	return m
}

func kiloMcpEqual(v any, command []string) bool {
	m, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	t, _ := m.Get("type")
	e, _ := m.Get("enabled")
	c, _ := m.Get("command")
	return t == "local" && e == true && anyStringSliceEqual(c, command)
}

func anyStringSliceEqual(v any, want []string) bool {
	a, ok := v.([]any)
	if !ok || len(a) != len(want) {
		return false
	}
	for i, x := range a {
		if x != want[i] {
			return false
		}
	}
	return true
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

var kilo = &core.AgentManifest{
	ID:        "kilo",
	Label:     "Kilo",
	Homepage:  "https://kilo.ai",
	CLIBin:    "kilo",
	ConfigDir: func() string { return util.KiloPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("kilo", util.KiloPathsResolved().Dir, nil, nil)
	},
}
