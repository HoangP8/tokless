package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func copilotHooksFile(name string) string {
	return filepath.Join(util.CopilotPathsResolved().HooksDir, name)
}

var ideProjectRoot string
var copilotHookWriteMu sync.Mutex

func SetIdeProjectRoot(p string) { ideProjectRoot = p }

func IdeProjectRoot() string { return ideRoot() }

func ideRoot() string {
	if ideProjectRoot != "" {
		return ideProjectRoot
	}
	return "."
}

// IDE (VS Code) paths — project-scoped, written relative to cwd.

func copilotIdeHooksDir() string { return filepath.Join(ideRoot(), ".github", "hooks") }
func copilotIdeHooksFile(name string) string {
	return filepath.Join(copilotIdeHooksDir(), name)
}
func copilotIdeMcpFile() string { return filepath.Join(ideRoot(), ".vscode", "mcp.json") }
func copilotIdeInstructionsFile() string {
	return filepath.Join(ideRoot(), ".github", "copilot-instructions.md")
}

// InstallCopilotContextModeHook writes the context-mode hook file.
func InstallCopilotContextModeHook() {
	_ = InstallCopilotContextModeHookSafe()
}

func InstallCopilotContextModeHookSafe() error {
	p := util.CopilotPathsResolved()
	if err := util.EnsureDir(p.HooksDir); err != nil {
		return err
	}

	events := []struct{ event, token string }{
		{"preToolUse", "pretooluse"},
		{"postToolUse", "posttooluse"},
		{"sessionStart", "sessionstart"},
		{"userPromptSubmitted", "userpromptsubmit"},
		{"agentStop", "stop"},
		{"preCompact", "precompact"},
	}
	hooks := util.NewOrderedMap()
	for _, e := range events {
		h := util.NewOrderedMap()
		h.Set("type", "command")
		h.Set("command", "context-mode hook copilot-cli "+e.token)
		hooks.Set(e.event, []any{h})
	}

	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	return writeOwnedCopilotHook(copilotHooksFile("context-mode.json"), util.StringifyJSON(root), "context-mode hook copilot-cli")
}

func RemoveCopilotContextModeHook() {
	_ = RemoveCopilotContextModeHookSafe()
}

func RemoveCopilotContextModeHookSafe() error {
	return removeOwnedCopilotHook(copilotHooksFile("context-mode.json"), "context-mode hook copilot-cli")
}

func HasCopilotContextModeHook() bool {
	raw, ok := util.ReadFileSafe(copilotHooksFile("context-mode.json"))
	cfg := util.TryParseJsonc(raw)
	return ok && cfg != nil && copilotHookOwned(cfg, "context-mode hook copilot-cli")
}

// InstallCopilotIdeContextModeHook writes IDE context-mode hooks (.github/hooks/).
func InstallCopilotIdeContextModeHook() {
	_ = InstallCopilotIdeContextModeHookSafe()
}

func InstallCopilotIdeContextModeHookSafe() error {
	if err := util.EnsureDir(copilotIdeHooksDir()); err != nil {
		return err
	}
	events := []string{"PreToolUse", "PostToolUse", "SessionStart", "Stop"}
	tokens := []string{"pretooluse", "posttooluse", "sessionstart", "stop"}
	hooks := util.NewOrderedMap()
	for i, ev := range events {
		h := util.NewOrderedMap()
		h.Set("type", "command")
		h.Set("command", "context-mode hook copilot-vscode "+tokens[i])
		h.Set("timeout", 10)
		hooks.Set(ev, []any{h})
	}
	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	return writeOwnedCopilotHook(copilotIdeHooksFile("context-mode.json"), util.StringifyJSON(root), "context-mode hook copilot-vscode")
}

func RemoveCopilotIdeContextModeHook() {
	_ = RemoveCopilotIdeContextModeHookSafe()
}

func RemoveCopilotIdeContextModeHookSafe() error {
	return removeOwnedCopilotHook(copilotIdeHooksFile("context-mode.json"), "context-mode hook copilot-vscode")
}

func HasCopilotIdeContextModeHook() bool {
	raw, ok := util.ReadFileSafe(copilotIdeHooksFile("context-mode.json"))
	cfg := util.TryParseJsonc(raw)
	return ok && cfg != nil && copilotHookOwned(cfg, "context-mode hook copilot-vscode")
}

func copilotRtkHookCommand() string {
	return toklessCommand("rtk-hook", "copilot")
}

// InstallCopilotRtkHook writes ~/.copilot/hooks/tokless-rtk.json.
func InstallCopilotRtkHook() {
	_ = InstallCopilotRtkHookSafe()
}

func InstallCopilotRtkHookSafe() error {
	p := util.CopilotPathsResolved()
	if err := util.EnsureDir(p.HooksDir); err != nil {
		return err
	}

	cmd := copilotRtkHookCommand()

	// Flat format: {type,command,timeout} — used by both Copilot CLI and VS Code.
	flat := func() *util.OrderedMap {
		h := util.NewOrderedMap()
		h.Set("type", "command")
		h.Set("command", cmd)
		return h
	}

	hooks := util.NewOrderedMap()
	hooks.Set("PreToolUse", []any{flat()})
	hooks.Set("preToolUse", []any{flat()})
	hooks.Set("PostToolUse", []any{flat()})
	hooks.Set("postToolUse", []any{flat()})

	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	if err := writeOwnedCopilotHook(copilotHooksFile("tokless-rtk.json"), util.StringifyJSON(root), "rtk-hook copilot"); err != nil {
		return err
	}
	return EnsureCopilotRtkCommandApprovalSafe()
}

func RemoveCopilotRtkHook() {
	_ = RemoveCopilotRtkHookSafe()
}

func RemoveCopilotRtkHookSafe() error {
	return removeOwnedCopilotHook(copilotHooksFile("tokless-rtk.json"), "rtk-hook copilot")
}

// InstallCopilotIdeRtkHook writes .github/hooks/tokless-rtk.json for VS Code IDE.
func InstallCopilotIdeRtkHook() {
	_ = InstallCopilotIdeRtkHookSafe()
}

func InstallCopilotIdeRtkHookSafe() error {
	if err := util.EnsureDir(copilotIdeHooksDir()); err != nil {
		return err
	}
	cmd := copilotRtkHookCommand()
	entry := util.NewOrderedMap()
	entry.Set("type", "command")
	entry.Set("command", cmd)

	hooks := util.NewOrderedMap()
	hooks.Set("PreToolUse", []any{entry})
	hooks.Set("PostToolUse", []any{entry})
	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	return writeOwnedCopilotHook(copilotIdeHooksFile("tokless-rtk.json"), util.StringifyJSON(root), "rtk-hook copilot")
}

func RemoveCopilotIdeRtkHook() {
	_ = RemoveCopilotIdeRtkHookSafe()
}

func RemoveCopilotIdeRtkHookSafe() error {
	return removeOwnedCopilotHook(copilotIdeHooksFile("tokless-rtk.json"), "rtk-hook copilot")
}

func HasCopilotIdeRtkHook() bool {
	raw, ok := util.ReadFileSafe(copilotIdeHooksFile("tokless-rtk.json"))
	cfg := util.TryParseJsonc(raw)
	return ok && cfg != nil && copilotHookOwned(cfg, "rtk-hook copilot")
}

func copilotPermissionsFile() string {
	return filepath.Join(util.CopilotPathsResolved().Dir, "permissions-config.json")
}

// EnsureCopilotRtkCommandApproval merges kind=commands commandIdentifiers=["rtk"]
// into every existing location in permissions-config.json.
func EnsureCopilotRtkCommandApproval() {
	_ = EnsureCopilotRtkCommandApprovalSafe()
}

func EnsureCopilotRtkCommandApprovalSafe() error {
	path := copilotPermissionsFile()
	contents, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	raw := string(contents)
	cfg := util.TryParseJsonc(string(raw))
	if strings.TrimSpace(raw) != "" && cfg == nil {
		return fmt.Errorf("invalid Copilot permissions config %s", path)
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	locs, ok := mapChild(cfg, "locations")
	if !ok {
		if _, exists := cfg.Get("locations"); exists {
			return fmt.Errorf("Copilot permissions config %s has non-object locations", path)
		}
		return nil
	}
	changed := false
	for _, key := range locs.Keys() {
		locRaw, _ := locs.Get(key)
		loc, ok := locRaw.(*util.OrderedMap)
		if !ok {
			continue
		}
		var approvals []any
		if v, ok := loc.Get("tool_approvals"); ok {
			var valid bool
			approvals, valid = v.([]any)
			if !valid {
				return fmt.Errorf("Copilot permissions config %s has non-array tool_approvals for %s", path, key)
			}
		}
		if copilotApprovalsHasRtk(approvals) {
			continue
		}
		entry := util.NewOrderedMap()
		entry.Set("kind", "commands")
		entry.Set("commandIdentifiers", []any{"rtk"})
		approvals = append(approvals, entry)
		loc.Set("tool_approvals", approvals)
		changed = true
	}
	if !changed {
		return nil
	}
	return writeCopilotFile(path, util.StringifyJSON(cfg))
}

func copilotApprovalsHasRtk(approvals []any) bool {
	for _, a := range approvals {
		m, ok := a.(*util.OrderedMap)
		if !ok {
			if mm, ok := a.(map[string]any); ok {
				if kind, _ := mm["kind"].(string); kind != "commands" {
					continue
				}
				ids, _ := mm["commandIdentifiers"].([]any)
				for _, id := range ids {
					if s, ok := id.(string); ok && s == "rtk" {
						return true
					}
				}
			}
			continue
		}
		kind, _ := m.Get("kind")
		if ks, _ := kind.(string); ks != "commands" {
			continue
		}
		idsRaw, _ := m.Get("commandIdentifiers")
		ids, _ := idsRaw.([]any)
		for _, id := range ids {
			if s, ok := id.(string); ok && s == "rtk" {
				return true
			}
		}
	}
	return false
}

// InstallCopilotCodegraphIndexHook writes a sessionStart hook that syncs the
// per-project codegraph index once when a Copilot CLI session begins.
func InstallCopilotCodegraphIndexHook() {
	_ = InstallCopilotCodegraphIndexHookSafe()
}

func InstallCopilotCodegraphIndexHookSafe() error {
	p := util.CopilotPathsResolved()
	if err := util.EnsureDir(p.HooksDir); err != nil {
		return err
	}
	cmd := toklessCommand("copilot-hook", "codegraph-index")
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", cmd)
	hooks := util.NewOrderedMap()
	hooks.Set("sessionStart", []any{hook})
	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	return writeOwnedCopilotHook(copilotHooksFile("tokless-codegraph-index.json"), util.StringifyJSON(root), "copilot-hook codegraph-index")
}

func RemoveCopilotCodegraphIndexHook() {
	_ = RemoveCopilotCodegraphIndexHookSafe()
}

func RemoveCopilotCodegraphIndexHookSafe() error {
	return removeOwnedCopilotHook(copilotHooksFile("tokless-codegraph-index.json"), "copilot-hook codegraph-index")
}

// InstallCopilotIdeCodegraphIndexHook writes a SessionStart bootstrap hook for VS Code.
func InstallCopilotIdeCodegraphIndexHook() {
	_ = InstallCopilotIdeCodegraphIndexHookSafe()
}

func InstallCopilotIdeCodegraphIndexHookSafe() error {
	if err := util.EnsureDir(copilotIdeHooksDir()); err != nil {
		return err
	}
	cmd := toklessCommand("copilot-hook", "codegraph-index", "--vscode")
	entry := util.NewOrderedMap()
	entry.Set("type", "command")
	entry.Set("command", cmd)
	entry.Set("timeout", 120)
	hooks := util.NewOrderedMap()
	hooks.Set("SessionStart", []any{entry})
	root := util.NewOrderedMap()
	root.Set("version", 1)
	root.Set("hooks", hooks)
	return writeOwnedCopilotHook(copilotIdeHooksFile("tokless-codegraph-index.json"), util.StringifyJSON(root), "copilot-hook codegraph-index")
}

func writeOwnedCopilotHook(path, content, marker string) error {
	copilotHookWriteMu.Lock()
	defer copilotHookWriteMu.Unlock()
	return writeOwnedCopilotHookLocked(path, content, marker)
}

func writeOwnedCopilotHookLocked(path, content, marker string) error {
	if raw, ok := util.ReadFileSafe(path); ok {
		cfg := util.TryParseJsonc(raw)
		if cfg == nil || !copilotHookOwned(cfg, marker) {
			return fmt.Errorf("refusing to overwrite foreign Copilot hook %s", path)
		}
	}
	return writeCopilotFile(path, content)
}

func writeCopilotFile(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeCopilotFileMode(path, content, mode)
}

func writeCopilotFileMode(path, content string, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through Copilot symlink %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return util.WriteFileAtomic(path, content, mode)
}

// WriteCopilotInstructionFile writes Copilot-owned instruction content safely.
func WriteCopilotInstructionFile(path, content string) error {
	return writeCopilotFile(path, content)
}

func WriteCopilotProjectFile(path, content string) error {
	return writeCopilotVSCodeFile(path, content)
}

func removeOwnedCopilotHook(path, marker string) error {
	copilotHookWriteMu.Lock()
	defer copilotHookWriteMu.Unlock()
	return removeOwnedCopilotHookLocked(path, marker)
}

func removeOwnedCopilotHookLocked(path, marker string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to remove Copilot hook symlink %s", path)
	}
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return nil
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil || !copilotHookOwned(cfg, marker) {
		return fmt.Errorf("refusing to remove foreign Copilot hook %s", path)
	}
	return os.Remove(path)
}

func copilotHookOwned(cfg *util.OrderedMap, marker string) bool {
	if cfg == nil {
		return false
	}
	for _, key := range cfg.Keys() {
		if key != "version" && key != "hooks" {
			return false
		}
	}
	if version, ok := cfg.Get("version"); !ok || fmt.Sprint(version) != "1" {
		return false
	}
	hooksRaw, ok := cfg.Get("hooks")
	if !ok {
		return false
	}
	hooks, ok := hooksRaw.(*util.OrderedMap)
	if !ok {
		return false
	}
	expected := map[string]bool{}
	expectedTimeout := -1
	switch marker {
	case "context-mode hook copilot-cli":
		expected = map[string]bool{"preToolUse": true, "postToolUse": true, "sessionStart": true, "userPromptSubmitted": true, "agentStop": true, "preCompact": true}
	case "context-mode hook copilot-vscode":
		expected = map[string]bool{"PreToolUse": true, "PostToolUse": true, "SessionStart": true, "Stop": true}
		expectedTimeout = 10
	case "rtk-hook copilot":
		if hooks.Len() == 2 {
			expected = map[string]bool{"PreToolUse": true, "PostToolUse": true}
		} else {
			expected = map[string]bool{"PreToolUse": true, "preToolUse": true, "PostToolUse": true, "postToolUse": true}
		}
	case "copilot-hook codegraph-index":
		if strings.Contains(copilotHookCommandMarker(cfg), "--vscode") {
			expected = map[string]bool{"SessionStart": true}
			expectedTimeout = 120
		} else {
			expected = map[string]bool{"sessionStart": true}
		}
	default:
		return false
	}
	if len(expected) != hooks.Len() {
		return false
	}
	for _, event := range hooks.Keys() {
		if !expected[event] {
			return false
		}
		entries, ok := hooks.Get(event)
		if !ok {
			continue
		}
		list, ok := entries.([]any)
		if !ok || len(list) != 1 {
			return false
		}
		entry, ok := list[0].(*util.OrderedMap)
		if !ok {
			return false
		}
		if entry.Len() != 2 && !(expectedTimeout >= 0 && entry.Len() == 3) {
			return false
		}
		typeValue, typeOK := entry.Get("type")
		if !typeOK || typeValue != "command" {
			return false
		}
		if timeout, hasTimeout := entry.Get("timeout"); (expectedTimeout >= 0) != hasTimeout || (hasTimeout && fmt.Sprint(timeout) != fmt.Sprint(expectedTimeout)) {
			return false
		}
		command, _ := entry.Get("command")
		commandText := fmt.Sprint(command)
		owned := copilotHookCommandOwned(commandText, marker)
		if !owned {
			return false
		}
	}
	return hooks.Len() > 0
}

func copilotHookCommandMarker(cfg *util.OrderedMap) string {
	hooksRaw, _ := cfg.Get("hooks")
	hooks, _ := hooksRaw.(*util.OrderedMap)
	for _, event := range hooks.Keys() {
		entries, _ := hooks.Get(event)
		list, _ := entries.([]any)
		entry, _ := list[0].(*util.OrderedMap)
		command, _ := entry.Get("command")
		return fmt.Sprint(command)
	}
	return ""
}

func copilotHookCommandOwned(command, marker string) bool {
	switch marker {
	case "context-mode hook copilot-cli":
		return hasExactHookSuffix(command, marker, []string{"pretooluse", "posttooluse", "sessionstart", "userpromptsubmit", "stop", "precompact"})
	case "context-mode hook copilot-vscode":
		return hasExactHookSuffix(command, marker, []string{"pretooluse", "posttooluse", "sessionstart", "stop"})
	case "rtk-hook copilot":
		return toklessManagedCommand(command, "rtk-hook", "copilot")
	case "copilot-hook codegraph-index":
		return toklessManagedCommand(command, "copilot-hook", "codegraph-index") || toklessManagedCommand(command, "copilot-hook", "codegraph-index", "--vscode")
	default:
		return command == marker
	}
}

func hasExactHookSuffix(command, prefix string, suffixes []string) bool {
	if !strings.HasPrefix(command, prefix+" ") {
		return false
	}
	suffix := strings.TrimPrefix(command, prefix+" ")
	for _, allowed := range suffixes {
		if suffix == allowed {
			return true
		}
	}
	return false
}

func RemoveCopilotIdeCodegraphIndexHook() {
	_ = RemoveCopilotIdeCodegraphIndexHookSafe()
}

func RemoveCopilotIdeCodegraphIndexHookSafe() error {
	return removeOwnedCopilotHook(copilotIdeHooksFile("tokless-codegraph-index.json"), "copilot-hook codegraph-index")
}

func HasCopilotIdeCodegraphIndexHook() bool {
	raw, ok := util.ReadFileSafe(copilotIdeHooksFile("tokless-codegraph-index.json"))
	cfg := util.TryParseJsonc(raw)
	return ok && cfg != nil && copilotHookOwned(cfg, "copilot-hook codegraph-index")
}

func HasCopilotCodegraphIndexHook() bool {
	raw, ok := util.ReadFileSafe(copilotHooksFile("tokless-codegraph-index.json"))
	cfg := util.TryParseJsonc(raw)
	return ok && cfg != nil && copilotHookOwned(cfg, "copilot-hook codegraph-index")
}

func HasCopilotRtkHook() bool {
	raw, ok := util.ReadFileSafe(copilotHooksFile("tokless-rtk.json"))
	cfg := util.TryParseJsonc(raw)
	return ok && cfg != nil && copilotHookOwned(cfg, "rtk-hook copilot")
}

// ConfigureCopilotMcp upserts a tool entry in ~/.copilot/mcp-config.json.
func ConfigureCopilotMcp(toolID string) (changed bool, file string) {
	changed, file, _ = ConfigureCopilotMcpSafe(toolID)
	return changed, file
}

func ConfigureCopilotMcpSafe(toolID string) (changed bool, file string, err error) {
	p := util.CopilotPathsResolved()
	if err := util.EnsureDir(p.Dir); err != nil {
		return false, p.McpConfig, err
	}
	raw, readErr := os.ReadFile(p.McpConfig)
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, p.McpConfig, readErr
	}
	cfg := util.TryParseJsonc(string(raw))
	if len(raw) > 0 && cfg == nil {
		return false, p.McpConfig, fmt.Errorf("invalid Copilot MCP config %s", p.McpConfig)
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers, err := copilotMcpServers(cfg, "mcpServers", p.McpConfig)
	if err != nil {
		return false, p.McpConfig, err
	}
	desired := copilotMcpDesired(toolID, false)

	if existing, ok := servers.Get(toolID); ok && copilotMcpEqual(existing, desired) {
		return false, p.McpConfig, nil
	} else if ok && !copilotManagedMcpEntry(existing, toolID, false) {
		return false, p.McpConfig, fmt.Errorf("refusing to overwrite foreign Copilot MCP entry %s", toolID)
	}
	servers.Set(toolID, desired)
	if err := writeCopilotFile(p.McpConfig, util.StringifyJSON(cfg)); err != nil {
		return false, p.McpConfig, err
	}
	return true, p.McpConfig, nil
}

// RemoveCopilotMcp deletes a tool entry from ~/.copilot/mcp-config.json.
func RemoveCopilotMcp(toolID string) bool {
	removed, _ := RemoveCopilotMcpSafe(toolID)
	return removed
}

func RemoveCopilotMcpSafe(toolID string) (bool, error) {
	p := util.CopilotPathsResolved()
	contents, err := os.ReadFile(p.McpConfig)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	raw := string(contents)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false, fmt.Errorf("invalid Copilot MCP config %s", p.McpConfig)
	}
	servers, err := copilotMcpServers(cfg, "mcpServers", p.McpConfig)
	if err != nil {
		return false, err
	}
	existing, has := servers.Get(toolID)
	if !has {
		return false, nil
	}
	if !copilotManagedMcpEntry(existing, toolID, false) {
		return false, fmt.Errorf("refusing to remove foreign Copilot MCP entry %s", toolID)
	}
	servers.Delete(toolID)
	if err := writeCopilotFile(p.McpConfig, util.StringifyJSON(cfg)); err != nil {
		return false, err
	}
	return true, nil
}

// CopilotMcpHas reports whether toolID is registered in copilot's MCP config.
func CopilotMcpHas(toolID string) bool {
	p := util.CopilotPathsResolved()
	raw, ok := util.ReadFileSafe(p.McpConfig)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(string(raw))
	if cfg == nil {
		return false
	}
	if s, ok := cfg.Get("mcpServers"); ok {
		if sm, ok := s.(*util.OrderedMap); ok {
			_, has := sm.Get(toolID)
			return has
		}
	}
	return false
}

// --- IDE (VS Code) MCP: .vscode/mcp.json ---

func ConfigureCopilotIdeMcp(toolID string) (changed bool, file string) {
	changed, file, _ = ConfigureCopilotIdeMcpSafe(toolID)
	return changed, file
}

func ConfigureCopilotIdeMcpSafe(toolID string) (changed bool, file string, err error) {
	copilotProjectWriteMu.Lock()
	defer copilotProjectWriteMu.Unlock()
	return configureCopilotIdeMcpLocked(toolID)
}

func configureCopilotIdeMcpLocked(toolID string) (changed bool, file string, err error) {
	path := copilotIdeMcpFile()
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return false, path, err
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, path, readErr
	}
	cfg := util.TryParseJsonc(string(raw))
	if len(raw) > 0 && cfg == nil {
		return false, path, fmt.Errorf("invalid Copilot VS Code MCP config %s", path)
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers, err := copilotMcpServers(cfg, "servers", path)
	if err != nil {
		return false, path, err
	}
	desired := copilotMcpDesired(toolID, true)

	if existing, ok := servers.Get(toolID); ok && copilotMcpEqual(existing, desired) {
		return false, path, nil
	} else if ok && !copilotManagedMcpEntry(existing, toolID, true) {
		return false, path, fmt.Errorf("refusing to overwrite foreign Copilot VS Code MCP entry %s", toolID)
	}
	servers.Set(toolID, desired)
	if err := writeCopilotFile(path, util.StringifyJSON(cfg)); err != nil {
		return false, path, err
	}
	return true, path, nil
}

func copilotMcpServers(cfg *util.OrderedMap, key, path string) (*util.OrderedMap, error) {
	if v, ok := cfg.Get(key); ok {
		servers, ok := v.(*util.OrderedMap)
		if !ok {
			return nil, fmt.Errorf("Copilot MCP config %s has non-object %s", path, key)
		}
		return servers, nil
	}
	servers := util.NewOrderedMap()
	cfg.Set(key, servers)
	return servers, nil
}

func copilotMcpDesired(toolID string, ide bool) *util.OrderedMap {
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("copilot", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.McpSpawnFor(toolID)
	}
	desired := util.NewOrderedMap()
	if ide {
		desired.Set("type", "stdio")
	} else {
		desired.Set("type", "local")
	}
	desired.Set("command", spawn.Command)
	desired.Set("args", toAnySlice(spawn.Args))
	if !ide {
		desired.Set("tools", []any{"*"})
	}
	if toolID == "context-mode" {
		env := util.NewOrderedMap()
		if ide {
			env.Set("CONTEXT_MODE_PLATFORM", "copilot-vscode")
		} else {
			env.Set("CONTEXT_MODE_PLATFORM", "copilot-cli")
		}
		env.Set("CONTEXT_MODE_COPILOT_PLUGIN", "1")
		desired.Set("env", env)
	}
	return desired
}

func copilotManagedMcpEntry(v any, toolID string, ide bool) bool {
	return copilotMcpEqual(v, copilotMcpDesired(toolID, ide))
}

func copilotMcpEqual(existing any, desired *util.OrderedMap) bool {
	em, ok := existing.(*util.OrderedMap)
	if !ok || em.Len() != desired.Len() {
		return false
	}
	for _, key := range desired.Keys() {
		want, _ := desired.Get(key)
		have, ok := em.Get(key)
		if !ok || jsonString(have) != jsonString(want) {
			return false
		}
	}
	return true
}

func jsonString(v any) string {
	return util.StringifyJSON(v)
}

func RemoveCopilotIdeMcp(toolID string) bool {
	removed, _ := RemoveCopilotIdeMcpSafe(toolID)
	return removed
}

func RemoveCopilotIdeMcpSafe(toolID string) (bool, error) {
	copilotProjectWriteMu.Lock()
	defer copilotProjectWriteMu.Unlock()
	return removeCopilotIdeMcpLocked(toolID)
}

func removeCopilotIdeMcpLocked(toolID string) (bool, error) {
	path := copilotIdeMcpFile()
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	raw := string(contents)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false, fmt.Errorf("invalid Copilot VS Code MCP config %s", path)
	}
	serversValue, exists := cfg.Get("servers")
	if !exists {
		return false, nil
	}
	servers, ok := serversValue.(*util.OrderedMap)
	if !ok {
		return false, fmt.Errorf("Copilot MCP config %s has non-object servers", path)
	}
	existing, has := servers.Get(toolID)
	if !has {
		return false, nil
	}
	if !copilotManagedMcpEntry(existing, toolID, true) {
		return false, fmt.Errorf("refusing to remove foreign Copilot VS Code MCP entry %s", toolID)
	}
	servers.Delete(toolID)
	if err := writeCopilotFile(path, util.StringifyJSON(cfg)); err != nil {
		return false, err
	}
	return true, nil
}

func CopilotIdeMcpHas(toolID string) bool {
	raw, ok := util.ReadFileSafe(copilotIdeMcpFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if s, ok := cfg.Get("servers"); ok {
		if sm, ok := s.(*util.OrderedMap); ok {
			_, has := sm.Get(toolID)
			return has
		}
	}
	return false
}

// CopilotTransactionFileOwned reports whether a newly created transaction
// surface consists only of entries generated by Tokless.
func CopilotTransactionFileOwned(path string) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	base := filepath.Base(path)
	if base == "context-mode.json" || base == "tokless-codegraph-index.json" || base == "tokless-rtk.json" {
		cfg := util.TryParseJsonc(raw)
		for _, marker := range []string{"context-mode hook copilot-cli", "context-mode hook copilot-vscode", "copilot-hook codegraph-index", "rtk-hook copilot"} {
			if copilotHookOwned(cfg, marker) {
				return true
			}
		}
		return false
	}
	if path == copilotIdeMcpFile() || path == util.CopilotPathsResolved().McpConfig {
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			return false
		}
		key, ide := "mcpServers", false
		if path == copilotIdeMcpFile() {
			key, ide = "servers", true
		}
		servers, ok := cfg.Get(key)
		entries, ok := servers.(*util.OrderedMap)
		if !ok || entries.Len() == 0 || cfg.Len() != 1 {
			return false
		}
		for _, toolID := range entries.Keys() {
			entry, _ := entries.Get(toolID)
			if !copilotManagedMcpEntry(entry, toolID, ide) {
				return false
			}
		}
		return true
	}
	const begin = "# tokless:copilot-instructions begin"
	const end = "# tokless:copilot-instructions end"
	if base == "copilot-instructions.md" {
		return strings.HasPrefix(raw, begin+"\n") && strings.HasSuffix(raw, end+"\n") && strings.Count(raw, begin) == 1 && strings.Count(raw, end) == 1
	}
	return false
}

// SyncCopilotIdeInstructions copies the CLI merged instruction body to the IDE file.
func SyncCopilotIdeInstructions() {
	_ = SyncCopilotIdeInstructionsSafe()
}

func SyncCopilotIdeInstructionsSafe() error {
	copilotProjectWriteMu.Lock()
	defer copilotProjectWriteMu.Unlock()
	return syncCopilotIdeInstructionsLocked()
}

func syncCopilotIdeInstructionsLocked() error {
	cliPath := util.CopilotPathsResolved().Instructions
	contents, err := os.ReadFile(cliPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	body := string(contents)
	ok := err == nil
	path := copilotIdeInstructionsFile()
	const begin = "# tokless:copilot-instructions begin"
	const end = "# tokless:copilot-instructions end"
	if !ok || strings.TrimSpace(body) == "" {
		if existing, exists := util.ReadFileSafe(path); exists {
			start, finish := strings.Index(existing, begin), strings.Index(existing, end)
			if start >= 0 && finish >= start {
				finish += len(end)
				remaining := existing[:start] + existing[finish:]
				if strings.TrimSpace(remaining) == "" {
					return clearCopilotProjectFileLocked(path)
				}
				return writeCopilotVSCodeFileLocked(path, strings.TrimRight(remaining, "\n")+"\n")
			}
		}
		return nil
	}
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	managed := begin + "\n" + strings.TrimRight(body, "\n") + "\n" + end + "\n"
	if existing, ok := util.ReadFileSafe(path); ok && strings.TrimSpace(existing) != "" {
		start := strings.Index(existing, begin)
		finish := strings.Index(existing, end)
		if start < 0 || finish < start {
			return fmt.Errorf("refusing to overwrite foreign Copilot instructions %s", path)
		}
		finish += len(end)
		managed = existing[:start] + managed + existing[finish:]
	}
	return writeCopilotVSCodeFileLocked(path, managed)
}

func copilotKnownBinDirs() []string {
	dirs := []string{
		filepath.Join(util.Home(), ".local", "bin"),
		"/usr/local/bin",
	}
	if util.IsWin {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs, filepath.Join(la, "Microsoft", "WinGet", "Links"))
		}
	}
	return dirs
}

var copilot = &core.AgentManifest{
	ID:        "copilot",
	Label:     "GitHub Copilot",
	Homepage:  "https://github.com/github/copilot-cli",
	CLIBin:    "copilot",
	ConfigDir: func() string { return util.CopilotPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectVSCodeAgent("copilot", util.CopilotPathsResolved().Dir, copilotKnownBinDirs(), "github.copilot-chat")
	},
}
