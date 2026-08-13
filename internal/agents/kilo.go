package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// KiloProjectFile resolves a path inside a legacy project .kilo directory.
func KiloProjectFile(name ...string) string {
	root := kiloProjectDir()
	if root == "" {
		return ""
	}
	parts := append([]string{root, ".kilo"}, name...)
	return filepath.Join(parts...)
}

func KiloInstructionsPath() string {
	return filepath.Join(util.KiloPathsResolved().Dir, "AGENTS.md")
}

// ConfigureKiloMcpSafe wires Tokless MCP in Kilo's global config only; it
// never creates or modifies a project .kilo.
func ConfigureKiloMcpSafe(toolID string, command []string) (bool, string, error) {
	if !kiloExpectedCommand(toolID, command) {
		return false, util.KiloPathsResolved().Config, fmt.Errorf("unrecognized Tokless MCP command for Kilo tool %q; refusing to adopt or overwrite entry", toolID)
	}
	p := util.KiloPathsResolved().Config
	raw, exists := util.ReadFileSafe(p)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if exists && strings.TrimSpace(raw) != "" {
			return false, p, fmt.Errorf("cannot parse Kilo global config %s; refusing to overwrite it", p)
		}
		cfg = util.NewOrderedMap()
	}
	if _, ok := cfg.Get("$schema"); !ok {
		cfg.Set("$schema", "https://app.kilo.ai/config.json")
	}
	mcp, err := kiloMcpMap(cfg)
	if err != nil {
		return false, p, err
	}
	state, stateExists, err := kiloStateRead()
	if err != nil {
		return false, p, err
	}
	if existing, ok := mcp.Get(toolID); ok {
		owned, hasOwner := state[toolID]
		if !stateExists || !hasOwner {
			return false, p, fmt.Errorf("Kilo global MCP %q has no Tokless ownership state; refusing same-name adoption", toolID)
		}
		if !kiloManagedMcpEntry(existing, owned) || !kiloExpectedCommand(toolID, owned) {
			return false, p, fmt.Errorf("Kilo global MCP %q differs from Tokless ownership state; restore the owned entry or remove it manually", toolID)
		}
		if kiloManagedMcpEntry(existing, command) {
			return false, p, nil
		}
	}
	if stateExists {
		if owned, ok := state[toolID]; ok && !stringSlicesEqual(owned, command) {
			return false, p, fmt.Errorf("Kilo global MCP %q ownership state differs from requested command", toolID)
		}
	}
	if exists && hasJSONCComment(raw) {
		return false, p, fmt.Errorf("Kilo global config %s contains JSONC comments; edit it without relocating comments before wiring Tokless", p)
	}
	desired := util.NewOrderedMap()
	desired.Set("type", "local")
	desired.Set("command", toAny(command))
	desired.Set("enabled", true)
	mcp.Set(toolID, desired)
	content := util.StringifyJSON(cfg)
	if err := kiloStateSet(toolID, command); err != nil {
		return false, p, err
	}
	if err := writeKiloFileGuarded(p, content, raw, exists); err != nil {
		_ = kiloStateDelete(toolID)
		return false, p, err
	}
	return true, p, nil
}

func RemoveKiloMcp(toolID string) bool {
	p := util.KiloPathsResolved().Config
	state, stateExists, err := kiloStateRead()
	if err != nil || !stateExists {
		return false
	}
	if _, owned := state[toolID]; !owned {
		return false
	}
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return kiloStateDelete(toolID) == nil
	}
	if hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcp")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return kiloStateDelete(toolID) == nil
	}
	entry, ok := mcp.Get(toolID)
	if !ok {
		return kiloStateDelete(toolID) == nil
	}
	if !kiloManagedMcpEntry(entry, state[toolID]) {
		return false
	}
	mcp.Delete(toolID)
	if mcp.Len() == 0 {
		cfg.Delete("mcp")
	}
	if writeKiloConfig(p, raw, true, cfg) != nil {
		return false
	}
	return kiloStateDelete(toolID) == nil
}

func KiloMcpConfigured(toolID string) bool {
	p := util.KiloPathsResolved().Config
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
	p := util.KiloPathsResolved().Config
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

func kiloExpectedCommand(toolID string, command []string) bool {
	if len(command) < 2 || !kiloIsToklessCommand(command[0]) || command[1] != "run-mcp" {
		return false
	}
	switch toolID {
	case "context-mode":
		return len(command) >= 4 && command[2] == "--context-mode" && kiloContextServer(command[3:])
	case "codegraph":
		return len(command) >= 7 && command[2] == "--agent" && command[3] == "kilo" && kiloCodegraphServer(command[4:])
	case "headroom":
		return len(command) == 7 && command[2] == "--tool" && command[3] == "headroom" && kiloHeadroomServer(command[4:])
	default:
		return false
	}
}

func kiloHeadroomServer(command []string) bool {
	return len(command) == 3 && kiloCommandBase(command[0]) == "headroom" && command[1] == "mcp" && command[2] == "serve"
}

func kiloContextServer(command []string) bool {
	if len(command) == 1 {
		return kiloCommandBase(command[0]) == "context-mode"
	}
	if len(command) == 3 && kiloCommandBase(command[0]) == "npx" && command[1] == "--no-install" && command[2] == "context-mode" {
		return true
	}
	if len(command) == 5 && kiloCmdShimServer(command[:3], "npx") && command[3] == "--no-install" && command[4] == "context-mode" {
		return true
	}
	return len(command) == 3 && kiloCmdShimServer(command, "context-mode")
}

func kiloCodegraphServer(command []string) bool {
	if len(command) == 3 && command[1] == "serve" && command[2] == "--mcp" {
		return kiloCommandBase(command[0]) == "codegraph"
	}
	if len(command) == 5 && kiloCommandBase(command[0]) == "npx" && command[1] == "--no-install" && command[2] == "@colbymchenry/codegraph" && command[3] == "serve" && command[4] == "--mcp" {
		return true
	}
	if len(command) == 7 && kiloCmdShimServer(command[:3], "npx") && command[3] == "--no-install" && command[4] == "@colbymchenry/codegraph" && command[5] == "serve" && command[6] == "--mcp" {
		return true
	}
	return len(command) == 5 && kiloCmdShimServer(command[:3], "codegraph") && command[3] == "serve" && command[4] == "--mcp"
}

func kiloCommandBase(command string) string {
	command = strings.ReplaceAll(command, "\\", "/")
	base := strings.ToLower(filepath.Base(command))
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func kiloCmdShimServer(command []string, server string) bool {
	if len(command) != 3 || kiloCommandBase(command[0]) != "cmd" || command[1] != "/c" || kiloCommandBase(command[2]) != server {
		return false
	}
	ext := strings.ToLower(filepath.Ext(strings.ReplaceAll(command[2], "\\", "/")))
	return ext == ".cmd" || ext == ".bat"
}

func kiloIsToklessCommand(command string) bool {
	return kiloCommandBase(command) == "tokless"
}

// kiloStatePath is the ownership ledger for Kilo global MCP entries.
func kiloStatePath() string {
	return filepath.Join(util.KiloPathsResolved().Dir, ".tokless-kilo-mcp-owners.json")
}

func kiloStateRead() (map[string][]string, bool, error) {
	state, exists, _, err := kiloStateReadRaw()
	if err != nil {
		return nil, exists, err
	}
	return state, exists, nil
}

func kiloStateReadRaw() (map[string][]string, bool, string, error) {
	raw, ok := util.ReadFileSafe(kiloStatePath())
	if !ok {
		return map[string][]string{}, false, "", nil
	}
	var state map[string][]string
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, true, raw, fmt.Errorf("Kilo Tokless ownership state is unreadable; refusing to modify existing MCP entries: %w", err)
	}
	for id, command := range state {
		if len(command) == 0 || !kiloExpectedCommand(id, command) {
			return nil, true, raw, fmt.Errorf("Kilo Tokless ownership state is malformed for %q; refusing to modify existing MCP entries", id)
		}
	}
	return state, true, raw, nil
}

func kiloStateSet(id string, command []string) error {
	state, exists, raw, err := kiloStateReadRaw()
	if err != nil {
		return err
	}
	state[id] = append([]string(nil), command...)
	return writeKiloFileGuarded(kiloStatePath(), util.StringifyJSON(state), raw, exists)
}

func kiloStateDelete(id string) error {
	state, exists, raw, err := kiloStateReadRaw()
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	delete(state, id)
	if len(state) == 0 {
		if err := os.Remove(kiloStatePath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeKiloFileGuarded(kiloStatePath(), util.StringifyJSON(state), raw, exists)
}

func writeKiloConfig(path, raw string, exists bool, cfg *util.OrderedMap) error {
	return writeKiloFileGuarded(path, util.StringifyJSON(cfg), raw, exists)
}

var kiloBeforeReplaceHook func(string)

// Guard catches edits observed between read and replacement.
func writeKiloFileGuarded(path, content, expectedRaw string, expectedExists bool) error {
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if kiloBeforeReplaceHook != nil {
		kiloBeforeReplaceHook(path)
	}
	current, exists := util.ReadFileSafe(path)
	if exists != expectedExists || (exists && current != expectedRaw) {
		return fmt.Errorf("Kilo file %s changed during update; retry", path)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tokless-kilo-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceKiloFile(tmpPath, path)
}

func kiloMcpMap(cfg *util.OrderedMap) (*util.OrderedMap, error) {
	if v, ok := cfg.Get("mcp"); ok {
		if m, ok := v.(*util.OrderedMap); ok {
			return m, nil
		}
		return nil, fmt.Errorf("Kilo config field %q is not an object; refusing to overwrite it", "mcp")
	}
	m := util.NewOrderedMap()
	cfg.Set("mcp", m)
	return m, nil
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

func kiloManagedMcpEntry(v any, command []string) bool {
	m, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	if m.Len() != 3 {
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

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
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

func kiloKnownBinDirs() []string {
	return []string{filepath.Join(util.Home(), ".kilo", "bin")}
}

var kilo = &core.AgentManifest{
	ID:        "kilo",
	Label:     "Kilo",
	Homepage:  "https://kilo.ai",
	CLIBin:    "kilo",
	ConfigDir: func() string { return util.KiloPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectVSCodeAgent("kilo", util.KiloPathsResolved().Dir, kiloKnownBinDirs(), "kilocode.kilo-code")
	},
}

// --- Kilo headroom HTTP proxy ---

const kiloProxyProvider = "tokless-headroom"

// kiloProxyProviderEntry builds the opencode-style provider entry injected
// into provider.<id>.
func kiloProxyProviderEntry(endpoint string) *util.OrderedMap {
	entry := util.NewOrderedMap()
	entry.Set("npm", "@ai-sdk/openai-compatible")
	entry.Set("name", "Headroom Proxy")
	options := util.NewOrderedMap()
	options.Set("baseURL", endpoint)
	entry.Set("options", options)
	model := util.NewOrderedMap()
	model.Set("name", "Headroom")
	model.Set("tool_call", true)
	limit := util.NewOrderedMap()
	limit.Set("context", 128000)
	limit.Set("output", 16384)
	model.Set("limit", limit)
	models := util.NewOrderedMap()
	models.Set("headroom", model)
	entry.Set("models", models)
	return entry
}

// ConfigureKiloProxy injects provider.tokless-headroom into kilo.jsonc,
// pointing at the OpenAI-compatible headroom daemon endpoint.
func ConfigureKiloProxy() (changed bool, file string) {
	p := util.KiloPathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, ok := util.ReadFileSafe(p.Config)
	if util.HasJSONCComments(raw) {
		return false, p.Config
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if ok {
			return false, p.Config
		}
		cfg = util.NewOrderedMap()
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		if _, present := cfg.Get("provider"); present {
			return false, p.Config
		}
		providers = util.NewOrderedMap()
		cfg.Set("provider", providers)
	}
	desired := kiloProxyProviderEntry(ProxyEndpointFor("kilo"))
	if existing, ok := providers.Get(kiloProxyProvider); ok {
		if jsonEqual(existing, desired) {
			return false, p.Config
		}
		return false, p.Config
	}
	providers.Set(kiloProxyProvider, desired)
	if err := util.WriteFile(p.Config, util.StringifyJSON(cfg)); err != nil {
		return false, p.Config
	}
	return true, p.Config
}

// RemoveKiloProxy deletes provider.tokless-headroom only while its value still
// equals what tokless injected.
func RemoveKiloProxy() bool {
	p := util.KiloPathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return false
	}
	if util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		return false
	}
	existing, ok := providers.Get(kiloProxyProvider)
	if !ok || !jsonEqual(existing, kiloProxyProviderEntry(ProxyEndpointFor("kilo"))) {
		return false
	}
	providers.Delete(kiloProxyProvider)
	if providers.Len() == 0 {
		cfg.Delete("provider")
	}
	return util.WriteFile(p.Config, util.StringifyJSON(cfg)) == nil
}

// KiloProxyWired reports whether provider.tokless-headroom points at the
// headroom daemon endpoint.
func KiloProxyWired() bool {
	raw, ok := util.ReadFileSafe(util.KiloPathsResolved().Config)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	providers, ok := mapChild(cfg, "provider")
	if !ok {
		return false
	}
	existing, ok := providers.Get(kiloProxyProvider)
	return ok && jsonEqual(existing, kiloProxyProviderEntry(ProxyEndpointFor("kilo")))
}

// jsonEqual compares two JSON values by canonical form.
func jsonEqual(a, b any) bool {
	return canonicalJSON(a) == canonicalJSON(b)
}

func canonicalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var m any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return string(b)
	}
	b2, _ := json.Marshal(m)
	return string(b2)
}
