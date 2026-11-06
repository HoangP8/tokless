package agents

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// OpenCodePluginKey is "plugin" on v1, "plugins" on v2.
func OpenCodePluginKey(v2 bool) string {
	if v2 {
		return "plugins"
	}
	return "plugin"
}

// OpenCodeHiddenCtxTools are hidden via permission "deny".
var OpenCodeHiddenCtxTools = []string{"ctx_resume", "ctx_stats", "ctx_doctor", "ctx_upgrade", "ctx_purge"}

// ErrOpenCodeConfigRefused marks an edit refused before any write.
var ErrOpenCodeConfigRefused = errors.New("opencode config left untouched")

func openCodeConfigPath(v2 bool) string {
	if v2 {
		return util.OpenCodePathsV2().Config
	}
	return util.OpenCodePathsResolved().Config
}

func pluginSpecIs(spec, pkg string) bool {
	return spec == pkg || strings.HasPrefix(spec, pkg+"@")
}

func OpenCodePluginSpecs(v2 bool, pkg string) []string {
	raw, ok := util.ReadFileSafe(openCodeConfigPath(v2))
	if !ok {
		return nil
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return nil
	}
	var out []string
	if arr, ok := cfg.Get(OpenCodePluginKey(v2)); ok {
		if list, ok := arr.([]any); ok {
			for _, p := range list {
				if s, ok := p.(string); ok && pluginSpecIs(s, pkg) {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// openCodeToklessMcpPaths covers v1 mcp.<pkg> and v2 mcp.servers.<pkg>.
func openCodeToklessMcpPaths(cfg *util.OrderedMap, pkg string) [][]string {
	mcp, ok := mapChild(cfg, "mcp")
	if !ok {
		return nil
	}
	var paths [][]string
	if v, ok := mcp.Get(pkg); ok && toklessMcpEntry(v) {
		paths = append(paths, []string{"mcp", pkg})
	}
	if servers, ok := mapChild(mcp, "servers"); ok {
		if v, ok := servers.Get(pkg); ok && toklessMcpEntry(v) {
			paths = append(paths, []string{"mcp", "servers", pkg})
		}
	}
	return paths
}

func OpenCodeToklessMcpHas(v2 bool, pkg string) bool {
	raw, ok := util.ReadFileSafe(openCodeConfigPath(v2))
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	return cfg != nil && len(openCodeToklessMcpPaths(cfg, pkg)) > 0
}

// SetOpenCodePluginHiding pins spec, denies hide tools, drops tokless MCP entries.
func SetOpenCodePluginHiding(v2 bool, pkg, spec string, hide []string) (bool, error) {
	return editOpenCodePlugin(v2, pkg, spec, true, hide)
}

// RemoveOpenCodePluginHiding removes only what tokless added.
func RemoveOpenCodePluginHiding(v2 bool, pkg string, hide []string) (bool, error) {
	return editOpenCodePlugin(v2, pkg, "", true, hide)
}

// OpenCodeHidingMissing: a non-object permission is the user's, not missing.
func OpenCodeHidingMissing(v2 bool, hide []string) bool {
	raw, ok := util.ReadFileSafe(openCodeConfigPath(v2))
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	perm := util.NewOrderedMap()
	if v, ok := cfg.Get("permission"); ok {
		m, isMap := v.(*util.OrderedMap)
		if !isMap {
			return false
		}
		perm = m
	}
	for _, n := range hide {
		if _, has := perm.Get(n); !has {
			return true
		}
	}
	return false
}

func RemoveOpenCodeToklessMcp(v2 bool, pkg string) (bool, error) {
	return editOpenCodePlugin(v2, pkg, "", false, nil)
}

// editOpenCodePlugin refuses a commented config it would change.
func editOpenCodePlugin(v2 bool, pkg, spec string, touchSpecs bool, hide []string) (bool, error) {
	path := openCodeConfigPath(v2)
	key := OpenCodePluginKey(v2)
	raw, exists := util.ReadFileSafe(path)
	if !exists && util.Exists(path) {
		return false, fmt.Errorf("opencode config %s is unreadable", path)
	}
	var cfg *util.OrderedMap
	if strings.TrimSpace(raw) == "" {
		if spec == "" {
			return false, nil
		}
		cfg = util.NewOrderedMap()
		if !v2 {
			cfg.Set("$schema", "https://opencode.ai/config.json")
		}
	} else if cfg = util.TryParseJsonc(raw); cfg == nil {
		return false, fmt.Errorf("opencode config %s is not valid JSON/JSONC; left untouched: %w", path, ErrOpenCodeConfigRefused)
	}
	before := util.StringifyJSON(cfg)
	hadMcp := len(openCodeToklessMcpPaths(cfg, pkg)) > 0
	if err := rewriteOpenCodePlugin(cfg, key, pkg, spec, touchSpecs, hide); err != nil {
		return false, fmt.Errorf("opencode config %s: %w: %w", path, err, ErrOpenCodeConfigRefused)
	}
	next := util.StringifyJSON(cfg)
	if next == before {
		return false, nil
	}
	if util.HasJSONCComments(raw) {
		// A leftover tokless MCP entry would show the tools twice.
		mcpHint := ""
		if hadMcp && spec != "" {
			mcpHint = fmt.Sprintf(" and remove the tokless-owned %q entry under \"mcp\"", pkg)
		}
		refuse := func(format string, args ...any) (bool, error) {
			return false, fmt.Errorf("opencode config %s has comments; edit by hand: "+format+": %w", append(append([]any{path}, args...), ErrOpenCodeConfigRefused)...)
		}
		if !touchSpecs {
			return refuse("remove the tokless-owned %q MCP entry", pkg)
		}
		if spec == "" {
			return refuse("remove %q from %q", pkg, key)
		}
		if len(hide) > 0 {
			return refuse("add %q to %q and set %s to \"deny\" under \"permission\"%s", spec, key, strings.Join(hide, ", "), mcpHint)
		}
		return refuse("add %q to %q%s", spec, key, mcpHint)
	}
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return false, err
	}
	return true, util.WriteFile(path, next)
}

func rewriteOpenCodePlugin(cfg *util.OrderedMap, key, pkg, spec string, touchSpecs bool, hide []string) error {
	if touchSpecs {
		var list []any
		if v, ok := cfg.Get(key); ok {
			arr, isArr := v.([]any)
			if !isArr {
				return fmt.Errorf("%q is not an array; refusing to replace it", key)
			}
			list = arr
		}
		var out []any
		placed := spec == ""
		matched := false
		for _, p := range list {
			if s, ok := p.(string); ok && pluginSpecIs(s, pkg) {
				matched = true
				if !placed {
					out = append(out, spec)
					placed = true
				}
				continue
			}
			out = append(out, p)
		}
		if !placed {
			out = append(out, spec)
		}
		switch {
		case len(out) == 0 && matched:
			cfg.Delete(key)
		case len(out) > 0:
			cfg.Set(key, out)
		}
	}
	if touchSpecs && len(hide) > 0 {
		if spec != "" {
			addOpenCodeDeny(cfg, hide)
		} else {
			removeOpenCodeDeny(cfg, hide)
		}
	}
	for _, p := range openCodeToklessMcpPaths(cfg, pkg) {
		cur := cfg
		parents := []*util.OrderedMap{cfg}
		for _, k := range p[:len(p)-1] {
			cur, _ = mapChild(cur, k)
			parents = append(parents, cur)
		}
		cur.Delete(p[len(p)-1])
		for i := len(parents) - 1; i > 0 && parents[i].Len() == 0; i-- {
			parents[i-1].Delete(p[i-1])
		}
	}
	return nil
}

func addOpenCodeDeny(cfg *util.OrderedMap, names []string) {
	perm := util.NewOrderedMap()
	if v, ok := cfg.Get("permission"); ok {
		m, isMap := v.(*util.OrderedMap)
		if !isMap {
			util.L.Sub("opencode config: \"permission\" is not an object; context-mode tools not hidden")
			return
		}
		perm = m
	}
	added := false
	for _, n := range names {
		if _, has := perm.Get(n); !has {
			perm.Set(n, "deny")
			added = true
		}
	}
	if added {
		cfg.Set("permission", perm)
	}
}

// removeOpenCodeDeny drops only entries that are exactly "deny".
func removeOpenCodeDeny(cfg *util.OrderedMap, names []string) {
	perm, ok := mapChild(cfg, "permission")
	if !ok {
		return
	}
	for _, n := range names {
		if v, has := perm.Get(n); has && v == "deny" {
			perm.Delete(n)
		}
	}
	if perm.Len() == 0 {
		cfg.Delete("permission")
	}
}
