package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func ClineInstructionsPath() string {
	return util.ClinePathsResolved().Instructions
}

// ConfigureClineMcpSafe wires Tokless MCP in Cline's global settings only.
func ConfigureClineMcpSafe(toolID string, command []string) (bool, string, error) {
	if toolID == "headroom" {
		return false, util.ClinePathsResolved().McpConfig, fmt.Errorf("Headroom is HTTP proxy-only; MCP wiring is disabled")
	}
	if !clineExpectedCommand(toolID, command) {
		return false, util.ClinePathsResolved().McpConfig, fmt.Errorf("unrecognized Tokless MCP command for Cline tool %q; refusing to adopt or overwrite entry", toolID)
	}
	p := util.ClinePathsResolved().McpConfig
	raw, exists := util.ReadFileSafe(p)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		if exists && len(raw) > 0 {
			return false, p, fmt.Errorf("cannot parse Cline MCP settings %s; refusing to overwrite it", p)
		}
		cfg = util.NewOrderedMap()
	}
	mcp, err := clineMcpMap(cfg)
	if err != nil {
		return false, p, err
	}
	state, stateExists, err := clineStateRead()
	if err != nil {
		return false, p, err
	}
	_, hadLedger := state[toolID]
	if existing, ok := mcp.Get(toolID); ok {
		owned, hasOwner := state[toolID]
		if !stateExists || !hasOwner {
			return false, p, fmt.Errorf("Cline MCP %q has no Tokless ownership state; refusing same-name adoption", toolID)
		}
		if !clineManagedMcpEntry(existing, owned) || !clineExpectedCommand(toolID, owned) {
			return false, p, fmt.Errorf("Cline MCP %q differs from Tokless ownership state; restore the owned entry or remove it manually", toolID)
		}
		if clineManagedMcpEntry(existing, command) {
			return false, p, nil
		}
	}
	if stateExists {
		if owned, ok := state[toolID]; ok && !stringSlicesEqual(owned, command) {
			return false, p, fmt.Errorf("Cline MCP %q ownership state differs from requested command", toolID)
		}
	}
	if exists && hasJSONCComment(raw) {
		return false, p, fmt.Errorf("Cline MCP settings %s contains JSONC comments; edit it without relocating comments before wiring Tokless", p)
	}
	desired := util.NewOrderedMap()
	desired.Set("command", command[0])
	desired.Set("args", toAny(command[1:]))
	desired.Set("env", util.NewOrderedMap())
	desired.Set("disabled", false)
	mcp.Set(toolID, desired)
	content := util.StringifyJSON(cfg)
	if err := clineStateSet(toolID, command); err != nil {
		return false, p, err
	}
	if err := clineWriteFileGuarded(p, content, raw, exists); err != nil {
		if !hadLedger {
			_ = clineStateDelete(toolID)
		}
		return false, p, err
	}
	return true, p, nil
}

func RemoveClineMcp(toolID string) bool {
	p := util.ClinePathsResolved().McpConfig
	state, stateExists, err := clineStateRead()
	if err != nil || !stateExists {
		return false
	}
	if _, owned := state[toolID]; !owned {
		return false
	}
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return clineStateDelete(toolID) == nil
	}
	if hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcpServers")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return clineStateDelete(toolID) == nil
	}
	entry, ok := mcp.Get(toolID)
	if !ok {
		return clineStateDelete(toolID) == nil
	}
	if !clineManagedMcpEntry(entry, state[toolID]) {
		return false
	}
	mcp.Delete(toolID)
	if mcp.Len() == 0 {
		cfg.Delete("mcpServers")
	}
	if clineWriteFileGuarded(p, util.StringifyJSON(cfg), raw, true) != nil {
		return false
	}
	return clineStateDelete(toolID) == nil
}

func ClineMcpConfigured(toolID string) bool {
	p := util.ClinePathsResolved().McpConfig
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcpServers")
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
	c, cok := em.Get("command")
	command, cok2 := c.(string)
	if !cok || !cok2 || command == "" {
		return false
	}
	if d, dok := em.Get("disabled"); dok && d == true {
		return false
	}
	if a, aok := em.Get("args"); aok {
		args, isSlice := a.([]any)
		if !isSlice || !allStringsNonempty(args) {
			return false
		}
	}
	return true
}

func ClineMcpMatches(toolID string, want []string) bool {
	p := util.ClinePathsResolved().McpConfig
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcpServers")
	mcp, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	entry, ok := mcp.Get(toolID)
	if !ok || !ClineMcpConfigured(toolID) {
		return false
	}
	em, ok := entry.(*util.OrderedMap)
	if !ok || len(want) == 0 {
		return false
	}
	c, cok := em.Get("command")
	command, cok2 := c.(string)
	if !cok || !cok2 || command != want[0] {
		return false
	}
	a, aok := em.Get("args")
	if !aok {
		return len(want) == 1
	}
	return anyStringSliceEqual(a, want[1:])
}

func clineExpectedCommand(toolID string, command []string) bool {
	if len(command) < 2 || !kiloIsToklessCommand(command[0]) || command[1] != "run-mcp" {
		return false
	}
	switch toolID {
	case "context-mode":
		return len(command) >= 4 && command[2] == "--context-mode" && kiloContextServer(command[3:])
	case "codegraph":
		return len(command) >= 7 && command[2] == "--agent" && command[3] == "cline" && kiloCodegraphServer(command[4:])
	default:
		return false
	}
}

// clineStatePath is the ownership ledger for Cline MCP entries.
func clineStatePath() string {
	return filepath.Join(util.ClinePathsResolved().Dir, ".tokless-cline-state.json")
}

func clineStateRead() (map[string][]string, bool, error) {
	state, exists, _, err := clineStateReadRaw()
	if err != nil {
		return nil, exists, err
	}
	return state, exists, nil
}

func clineStateReadRaw() (map[string][]string, bool, string, error) {
	raw, ok := util.ReadFileSafe(clineStatePath())
	if !ok {
		return map[string][]string{}, false, "", nil
	}
	var state map[string][]string
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, true, raw, fmt.Errorf("Cline Tokless ownership state is unreadable; refusing to modify existing MCP entries: %w", err)
	}
	for id, command := range state {
		if len(command) == 0 || !clineExpectedCommand(id, command) {
			return nil, true, raw, fmt.Errorf("Cline Tokless ownership state is malformed for %q; refusing to modify existing MCP entries", id)
		}
	}
	return state, true, raw, nil
}

func clineStateSet(id string, command []string) error {
	state, exists, raw, err := clineStateReadRaw()
	if err != nil {
		return err
	}
	state[id] = append([]string(nil), command...)
	return clineWriteFileGuarded(clineStatePath(), util.StringifyJSON(state), raw, exists)
}

func clineStateDelete(id string) error {
	state, exists, raw, err := clineStateReadRaw()
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	delete(state, id)
	if len(state) == 0 {
		if err := os.Remove(clineStatePath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return clineWriteFileGuarded(clineStatePath(), util.StringifyJSON(state), raw, exists)
}

// Guard catches edits observed between read and replacement.
func clineWriteFileGuarded(path, content, expectedRaw string, expectedExists bool) error {
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	current, exists := util.ReadFileSafe(path)
	if exists != expectedExists || (exists && current != expectedRaw) {
		return fmt.Errorf("Cline file %s changed during update; retry", path)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tokless-cline-*")
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

func clineMcpMap(cfg *util.OrderedMap) (*util.OrderedMap, error) {
	if v, ok := cfg.Get("mcpServers"); ok {
		if m, ok := v.(*util.OrderedMap); ok {
			return m, nil
		}
		return nil, fmt.Errorf("Cline config field %q is not an object; refusing to overwrite it", "mcpServers")
	}
	m := util.NewOrderedMap()
	cfg.Set("mcpServers", m)
	return m, nil
}

func clineManagedMcpEntry(v any, command []string) bool {
	m, ok := v.(*util.OrderedMap)
	if !ok || len(command) == 0 {
		return false
	}
	c, _ := m.Get("command")
	cs, ok := c.(string)
	if !ok || cs != command[0] {
		return false
	}
	a, _ := m.Get("args")
	if !anyStringSliceEqual(a, command[1:]) {
		return false
	}
	env, _ := m.Get("env")
	em, ok := env.(*util.OrderedMap)
	if !ok || em.Len() != 0 {
		return false
	}
	d, dok := m.Get("disabled")
	return dok && d == false
}

var cline = &core.AgentManifest{
	ID:        "cline",
	Label:     "Cline",
	Homepage:  "https://cline.bot",
	CLIBin:    "cline",
	ConfigDir: func() string { return util.ClinePathsResolved().Dir },
	Detect: func() core.Detection {
		if util.FindBinary("cline", nil) != "" {
			return core.Detection{Installed: true, Source: "cli"}
		}
		return core.Detection{}
	},
}
