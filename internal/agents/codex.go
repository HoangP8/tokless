package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ConfigureCodexMcp upserts a [mcp_servers.<tool>] block in config.toml.
func ConfigureCodexMcp(toolID string) (changed bool, file string) {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.Config)
	raw = sweepStaleHookStateEntries(raw)
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("codex", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.PickMcpSpawn(toolID)
	}
	block := util.NewTomlBlock("mcp_servers." + toolID)
	block.Set("command", spawn.Command)
	block.Set("args", spawn.Args)
	block.Set("enabled", true)
	block.Set("default_tools_approval_mode", "approve")
	next := util.UpsertBlock(raw, block, false)
	next = applyCodexApprovalPolicy(next)
	if next == raw {
		return false, p.Config
	}
	_ = util.WriteFile(p.Config, next)
	return true, p.Config
}

func codexHookStateHeader(key string) string {
	return util.TomlDottedTableHeader("hooks.state", key)
}

// sweepStaleHookStateEntries drops [hooks.state.*] blocks that are stale or use the
// legacy double-quoted header form for the current hooks.json path.
func sweepStaleHookStateEntries(raw string) string {
	current := codexHooksFile()
	re := regexp.MustCompile(`^\[hooks\.state\.(?:'([^']*)'|"([^"]*)")\]\s*$`)
	lines := strings.SplitAfter(raw, "\n")
	var out strings.Builder
	for i := 0; i < len(lines); {
		lineNoNL := strings.TrimRight(lines[i], "\r\n")
		m := re.FindStringSubmatch(lineNoNL)
		if m == nil {
			out.WriteString(lines[i])
			i++
			continue
		}
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), "[") {
			j++
		}
		key := m[1]
		if key == "" {
			key = m[2]
		}
		legacyDouble := m[2] != ""
		if strings.HasPrefix(key, current+":") && !legacyDouble {
			for ; i < j; i++ {
				out.WriteString(lines[i])
			}
			continue
		}
		i = j
	}
	return out.String()
}

// --- Codex rtk PreToolUse hook ---

const (
	codexHookMatcher     = "Bash|apply_patch|ctx_.*|codegraph_.*"
	codexHookTimeout     = 10
	codexPermHookMatcher = "Bash|apply_patch"
	codexPermHookTimeout = 5
)

func codexHooksFile() string {
	return filepath.Join(util.CodexPathsResolved().Dir, "hooks.json")
}

// codexHookCommand is the command Codex runs for every Bash tool call.
func codexHookCommand() string {
	return toklessCommand("rtk-hook", "codex")
}

func codexPermHookCommand() string {
	return toklessCommand("codex-perm", "codex")
}

// codexHookTrustHash reproduces Codex's hook-trust hash.
func codexHookTrustHash(command string) string {
	handler := map[string]interface{}{
		"async":   false,
		"command": command,
		"timeout": codexHookTimeout,
		"type":    "command",
	}
	identity := map[string]interface{}{
		"event_name": "pre_tool_use",
		"matcher":    codexHookMatcher,
		"hooks":      []interface{}{handler},
	}
	b, _ := json.Marshal(identity)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func codexPermHookTrustHash(command string) string {
	handler := map[string]interface{}{
		"async":   false,
		"command": command,
		"timeout": codexPermHookTimeout,
		"type":    "command",
	}
	identity := map[string]interface{}{
		"event_name": "permission_request",
		"matcher":    codexPermHookMatcher,
		"hooks":      []interface{}{handler},
	}
	b, _ := json.Marshal(identity)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func codexGroupHasRtk(group *util.OrderedMap) bool {
	return codexGroupHasManaged(group, "rtk-hook", "codex")
}

func codexGroupHasPerm(group *util.OrderedMap) bool {
	return codexGroupHasManaged(group, "codex-perm", "codex")
}

func codexGroupHasManaged(group *util.OrderedMap, args ...string) bool {
	hooksObj, _ := group.Get("hooks")
	arr, ok := hooksObj.([]interface{})
	if !ok {
		return false
	}
	for _, h := range arr {
		hm, ok := h.(*util.OrderedMap)
		if !ok {
			continue
		}
		cmd, _ := hm.Get("command")
		s, _ := cmd.(string)
		if toklessManagedCommand(s, args...) {
			return true
		}
	}
	return false
}

type codexHookPos struct{ group, hook int }

func codexTransformManagedGroups(groups []interface{}, matcher string, args []string, desired *util.OrderedMap) ([]interface{}, codexHookPos, map[codexHookPos]codexHookPos, []codexHookPos) {
	out := make([]interface{}, 0, len(groups)+1)
	desiredPos := codexHookPos{-1, -1}
	moved := map[codexHookPos]codexHookPos{}
	var removed []codexHookPos
	var desiredHook any
	if desired != nil {
		if hooksObj, ok := desired.Get("hooks"); ok {
			if hooks, ok := hooksObj.([]interface{}); ok && len(hooks) > 0 {
				desiredHook = hooks[0]
			}
		}
	}
	for oldGroup, g := range groups {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			out = append(out, g)
			continue
		}
		groupMatcher, _ := gm.Get("matcher")
		hooksObj, _ := gm.Get("hooks")
		hooks, ok := hooksObj.([]interface{})
		if !ok {
			out = append(out, g)
			continue
		}
		kept := make([]interface{}, 0, len(hooks))
		for oldHook, h := range hooks {
			hm, isMap := h.(*util.OrderedMap)
			managed := false
			if groupMatcher == matcher && isMap {
				cmd, _ := hm.Get("command")
				command, _ := cmd.(string)
				managed = toklessManagedCommand(command, args...)
			}
			if managed {
				if desiredHook != nil && desiredPos.group == -1 {
					desiredPos = codexHookPos{len(out), len(kept)}
					kept = append(kept, desiredHook)
				} else {
					removed = append(removed, codexHookPos{oldGroup, oldHook})
				}
				continue
			}
			newPos := codexHookPos{len(out), len(kept)}
			moved[codexHookPos{oldGroup, oldHook}] = newPos
			kept = append(kept, h)
		}
		if len(kept) > 0 {
			gm.Set("hooks", kept)
			out = append(out, gm)
		} else if len(hooks) == 0 {
			out = append(out, gm)
		}
	}
	if desiredHook != nil && desiredPos.group == -1 {
		desiredPos = codexHookPos{len(out), 0}
		out = append(out, desired)
	}
	return out, desiredPos, moved, removed
}

func codexRewriteHookState(raw, hooksFile, event string, moved map[codexHookPos]codexHookPos, removed []codexHookPos) string {
	for _, pos := range removed {
		key := hooksFile + ":" + event + ":" + strconv.Itoa(pos.group) + ":" + strconv.Itoa(pos.hook)
		raw = util.RemoveBlock(raw, codexHookStateHeader(key))
	}
	type move struct{ old, new, placeholder string }
	var moves []move
	for oldPos, newPos := range moved {
		if oldPos == newPos {
			continue
		}
		oldKey := hooksFile + ":" + event + ":" + strconv.Itoa(oldPos.group) + ":" + strconv.Itoa(oldPos.hook)
		newKey := hooksFile + ":" + event + ":" + strconv.Itoa(newPos.group) + ":" + strconv.Itoa(newPos.hook)
		placeholder := "hooks.state.'__tokless_move_" + strconv.Itoa(len(moves)) + "__'"
		oldHeader := "[" + codexHookStateHeader(oldKey) + "]"
		if strings.Contains(raw, oldHeader) {
			raw = strings.Replace(raw, oldHeader, "["+placeholder+"]", 1)
			moves = append(moves, move{placeholder: placeholder, new: codexHookStateHeader(newKey)})
		}
	}
	for _, m := range moves {
		raw = strings.Replace(raw, "["+m.placeholder+"]", "["+m.new+"]", 1)
	}
	return raw
}

func codexRtkGroup(command string) *util.OrderedMap {
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", command)
	hook.Set("timeout", codexHookTimeout)

	group := util.NewOrderedMap()
	group.Set("matcher", codexHookMatcher)
	group.Set("hooks", []interface{}{hook})
	return group
}

func codexPermGroup(command string) *util.OrderedMap {
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", command)
	hook.Set("timeout", codexPermHookTimeout)

	group := util.NewOrderedMap()
	group.Set("matcher", codexPermHookMatcher)
	group.Set("hooks", []interface{}{hook})
	return group
}

const (
	codexCtxHookMatcher = "local_shell|shell|shell_command|exec_command|Bash|Shell|apply_patch|Edit|Write|grep_files|ctx_execute|ctx_execute_file|ctx_batch_execute|ctx_fetch_and_index|ctx_search|ctx_index|mcp__"
	codexCtxHookTimeout = 10
)

// codexCtxHookCommand matches upstream context-mode's Codex PreToolUse command.
func codexCtxHookCommand() string {
	return "context-mode hook codex pretooluse"
}

func codexCtxHookTrustHash(command string) string {
	handler := map[string]interface{}{
		"async":   false,
		"command": command,
		"timeout": codexCtxHookTimeout,
		"type":    "command",
	}
	identity := map[string]interface{}{
		"event_name": "pre_tool_use",
		"matcher":    codexCtxHookMatcher,
		"hooks":      []interface{}{handler},
	}
	b, _ := json.Marshal(identity)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func codexGroupHasCtx(group *util.OrderedMap) bool {
	hooksObj, ok := group.Get("hooks")
	if !ok {
		return false
	}
	arr, ok := hooksObj.([]interface{})
	if !ok {
		return false
	}
	for _, h := range arr {
		hm, ok := h.(*util.OrderedMap)
		if !ok {
			continue
		}
		if cmd, ok := hm.Get("command"); ok {
			if s, ok := cmd.(string); ok && (strings.Contains(s, "context-mode hook codex pretooluse") || strings.Contains(s, "context-mode-hook codex")) {
				return true
			}
		}
	}
	return false
}

func codexCtxGroup(command string) *util.OrderedMap {
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", command)
	hook.Set("timeout", codexCtxHookTimeout)

	group := util.NewOrderedMap()
	group.Set("matcher", codexCtxHookMatcher)
	group.Set("hooks", []interface{}{hook})
	return group
}

// InstallCodexContextModeHook merges the context-mode redirect PreToolUse hook
// into ~/.codex/hooks.json and pre-seeds its trust hash in config.toml.
func InstallCodexContextModeHook() {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	command := codexCtxHookCommand()

	hooksFile := codexHooksFile()
	raw, _ := util.ReadFileSafe(hooksFile)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	var preArr []interface{}
	if v, ok := hooks.Get("PreToolUse"); ok {
		preArr, _ = v.([]interface{})
	}
	idx := -1
	for i, g := range preArr {
		if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasCtx(gm) {
			idx = i
			break
		}
	}
	group := codexCtxGroup(command)
	if idx == -1 {
		preArr = append(preArr, group)
		idx = len(preArr) - 1
	} else {
		preArr[idx] = group
	}
	hooks.Set("PreToolUse", preArr)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}

	craw, _ := util.ReadFileSafe(p.Config)
	craw = sweepStaleHookStateEntries(craw)
	key := hooksFile + ":pre_tool_use:" + strconv.Itoa(idx) + ":0"
	block := util.NewTomlBlock(codexHookStateHeader(key))
	block.Set("trusted_hash", codexCtxHookTrustHash(command))
	cnext := util.UpsertBlock(craw, block, false)
	cnext = applyCodexApprovalPolicy(cnext)
	features := util.NewTomlBlock("features")
	features.Set("hooks", true)
	cnext = util.UpsertBlock(cnext, features, false)
	if cnext != craw {
		_ = util.WriteFile(p.Config, cnext)
	}
}

// RemoveCodexContextModeHook removes the context-mode redirect group from
// hooks.json and its trust entry from config.toml.
func RemoveCodexContextModeHook() {
	p := util.CodexPathsResolved()
	hooksFile := codexHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		return
	}
	v, ok := hooks.Get("PreToolUse")
	if !ok {
		return
	}
	preArr, ok := v.([]interface{})
	if !ok {
		return
	}
	kept := make([]interface{}, 0, len(preArr))
	removedIdx := -1
	for i, g := range preArr {
		if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasCtx(gm) {
			removedIdx = i
			continue
		}
		kept = append(kept, g)
	}
	if removedIdx < 0 {
		return
	}
	if len(kept) == 0 {
		hooks.Delete("PreToolUse")
	} else {
		hooks.Set("PreToolUse", kept)
	}
	if hooks.Len() == 0 {
		_ = os.Remove(hooksFile)
	} else {
		_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
	}
	craw, _ := util.ReadFileSafe(p.Config)
	key := hooksFile + ":pre_tool_use:" + strconv.Itoa(removedIdx) + ":0"
	if cnext := util.RemoveBlock(craw, codexHookStateHeader(key)); cnext != craw {
		_ = util.WriteFile(p.Config, cnext)
	}
	codexCleanupOrphanedConfig()
}

// HasCodexContextModeHook reports whether the context-mode redirect hook is
// present in ~/.codex/hooks.json.
func HasCodexContextModeHook() bool {
	raw, ok := util.ReadFileSafe(codexHooksFile())
	if !ok {
		return false
	}
	return strings.Contains(raw, "context-mode hook codex pretooluse")
}

// InstallCodexRtkHook merges the rtk PreToolUse hook into ~/.codex/hooks.json.
func InstallCodexRtkHook() {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	command := codexHookCommand()

	hooksFile := codexHooksFile()
	raw, _ := util.ReadFileSafe(hooksFile)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	var preArr []interface{}
	if v, ok := hooks.Get("PreToolUse"); ok {
		preArr, _ = v.([]interface{})
	}
	preArr, pos, moved, removed := codexTransformManagedGroups(preArr, codexHookMatcher, []string{"rtk-hook", "codex"}, codexRtkGroup(command))
	hooks.Set("PreToolUse", preArr)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}

	craw, _ := util.ReadFileSafe(p.Config)
	craw = sweepStaleHookStateEntries(craw)
	craw = codexRewriteHookState(craw, hooksFile, "pre_tool_use", moved, removed)
	key := hooksFile + ":pre_tool_use:" + strconv.Itoa(pos.group) + ":" + strconv.Itoa(pos.hook)
	block := util.NewTomlBlock(codexHookStateHeader(key))
	block.Set("trusted_hash", codexHookTrustHash(command))
	cnext := util.UpsertBlock(craw, block, false)
	cnext = applyCodexApprovalPolicy(cnext)
	features := util.NewTomlBlock("features")
	features.Set("hooks", true)
	cnext = util.UpsertBlock(cnext, features, false)
	if cnext != craw {
		_ = util.WriteFile(p.Config, cnext)
	}

	_ = os.Remove(filepath.Join(p.Dir, "RTK.md"))

	InstallCodexPermissionHook()
	InstallCodexRulesAllowlist()
}

// RemoveCodexRtkHook removes the rtk group from hooks.json and its trust entry.
func RemoveCodexRtkHook() {
	p := util.CodexPathsResolved()
	hooksFile := codexHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			if hooks, ok := mapChild(cfg, "hooks"); ok {
				if v, ok := hooks.Get("PreToolUse"); ok {
					if preArr, ok := v.([]interface{}); ok {
						kept, _, moved, removed := codexTransformManagedGroups(preArr, codexHookMatcher, []string{"rtk-hook", "codex"}, nil)
						if len(removed) > 0 {
							hooks.Set("PreToolUse", kept)
							_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
							craw, _ := util.ReadFileSafe(p.Config)
							cnext := codexRewriteHookState(craw, hooksFile, "pre_tool_use", moved, removed)
							if cnext != craw {
								_ = util.WriteFile(p.Config, cnext)
							}
						}
					}
				}
			}
		}
	}
	RemoveCodexPermissionHook()
	RemoveCodexRulesAllowlist()
	codexCleanupOrphanedConfig()
}

// HasCodexRtkHook reports whether the rtk hook is present in hooks.json.
func HasCodexRtkHook() bool {
	raw, ok := util.ReadFileSafe(codexHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		return false
	}
	v, ok := hooks.Get("PreToolUse")
	if !ok {
		return false
	}
	preArr, ok := v.([]interface{})
	if !ok {
		return false
	}
	for _, g := range preArr {
		if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasRtk(gm) {
			return true
		}
	}
	return false
}

// InstallCodexPermissionHook merges a PermissionRequest group into hooks.json + pre-seeds trust.
func InstallCodexPermissionHook() {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	command := codexPermHookCommand()
	hooksFile := codexHooksFile()
	raw, _ := util.ReadFileSafe(hooksFile)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	var permArr []interface{}
	if v, ok := hooks.Get("PermissionRequest"); ok {
		permArr, _ = v.([]interface{})
	}
	permArr, pos, moved, removed := codexTransformManagedGroups(permArr, codexPermHookMatcher, []string{"codex-perm", "codex"}, codexPermGroup(command))
	hooks.Set("PermissionRequest", permArr)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}
	craw, _ := util.ReadFileSafe(p.Config)
	craw = sweepStaleHookStateEntries(craw)
	craw = codexRewriteHookState(craw, hooksFile, "permission_request", moved, removed)
	key := hooksFile + ":permission_request:" + strconv.Itoa(pos.group) + ":" + strconv.Itoa(pos.hook)
	block := util.NewTomlBlock(codexHookStateHeader(key))
	block.Set("trusted_hash", codexPermHookTrustHash(command))
	cnext := util.UpsertBlock(craw, block, false)
	if cnext != craw {
		_ = util.WriteFile(p.Config, cnext)
	}
}

// RemoveCodexPermissionHook removes the PermissionRequest group and its trust entry.
func RemoveCodexPermissionHook() {
	p := util.CodexPathsResolved()
	hooksFile := codexHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			if hooks, ok := mapChild(cfg, "hooks"); ok {
				if v, ok := hooks.Get("PermissionRequest"); ok {
					if permArr, ok := v.([]interface{}); ok {
						kept, _, moved, removed := codexTransformManagedGroups(permArr, codexPermHookMatcher, []string{"codex-perm", "codex"}, nil)
						if len(removed) > 0 {
							hooks.Set("PermissionRequest", kept)
							_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
							craw, _ := util.ReadFileSafe(p.Config)
							cnext := codexRewriteHookState(craw, hooksFile, "permission_request", moved, removed)
							if cnext != craw {
								_ = util.WriteFile(p.Config, cnext)
							}
						}
					}
				}
			}
		}
	}
}

// HasCodexPermissionHook reports whether the PermissionRequest hook is present.
func HasCodexPermissionHook() bool {
	raw, ok := util.ReadFileSafe(codexHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		return false
	}
	v, ok := hooks.Get("PermissionRequest")
	if !ok {
		return false
	}
	permArr, ok := v.([]interface{})
	if !ok {
		return false
	}
	for _, g := range permArr {
		if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasPerm(gm) {
			return true
		}
	}
	return false
}

func codexRulesFile() string {
	return filepath.Join(util.CodexPathsResolved().Dir, "rules", "default.rules")
}

// InstallCodexRulesAllowlist writes the shell allowlist to ~/.codex/rules/default.rules.
// Surgical: only writes when the file does not exist — never overwrites user's rules.
func InstallCodexRulesAllowlist() {
	rulesFile := codexRulesFile()
	if util.Exists(rulesFile) {
		return
	}
	_ = util.EnsureDir(filepath.Dir(rulesFile))
	_ = util.WriteFile(rulesFile, `# tokless-managed codex allowlist — our tools pre-approved, everything else prompts.

prefix_rule(pattern = ["rtk"], decision = "allow")
prefix_rule(pattern = ["tokless"], decision = "allow")
prefix_rule(pattern = ["git"], decision = "allow")
prefix_rule(pattern = ["cd"], decision = "allow")
prefix_rule(pattern = ["ls"], decision = "allow")
prefix_rule(pattern = ["node"], decision = "allow")
prefix_rule(pattern = ["npm"], decision = "allow")
prefix_rule(pattern = ["npx"], decision = "allow")
prefix_rule(pattern = ["context-mode"], decision = "allow")
prefix_rule(pattern = ["codegraph"], decision = "allow")
prefix_rule(pattern = ["cat"], decision = "allow")
prefix_rule(pattern = ["head"], decision = "allow")
prefix_rule(pattern = ["tail"], decision = "allow")
prefix_rule(pattern = ["grep"], decision = "allow")
prefix_rule(pattern = ["find"], decision = "allow")
prefix_rule(pattern = ["pwd"], decision = "allow")
prefix_rule(pattern = ["which"], decision = "allow")
prefix_rule(pattern = ["echo"], decision = "allow")
prefix_rule(pattern = ["bash"], decision = "allow")
prefix_rule(pattern = ["sh"], decision = "allow")
`)
}

// RemoveCodexRulesAllowlist removes the allowlist file only if it carries our
// marker — never deletes a user-authored rules file.
func RemoveCodexRulesAllowlist() {
	if !HasCodexRulesAllowlist() {
		return
	}
	_ = os.Remove(codexRulesFile())
	_ = os.Remove(filepath.Dir(codexRulesFile())) // ok if non-empty
}

// HasCodexRulesAllowlist reports whether the allowlist file exists with our marker.
func HasCodexRulesAllowlist() bool {
	if !util.Exists(codexRulesFile()) {
		return false
	}
	raw, ok := util.ReadFileSafe(codexRulesFile())
	if !ok {
		return false
	}
	return strings.Contains(raw, "tokless-managed codex allowlist")
}

func applyCodexApprovalPolicy(raw string) string {
	if util.GetTomlTopKey(raw, "approval_policy") == "" {
		return util.SetTomlTopKey(raw, "approval_policy", "on-request")
	}
	return raw
}

// codexCleanupOrphanedConfig removes tokless-injected top-level config keys
// when no tokless-managed hooks remain in hooks.json.
func codexCleanupOrphanedConfig() {
	p := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return
	}
	changed := false
	if !codexHasAnyToklessHook() {
		if util.HasBlock(raw, "features") {
			raw = util.RemoveBlock(raw, "features")
			changed = true
		}
		if v := util.GetTomlTopKey(raw, "approval_policy"); v == "on-request" {
			raw = util.RemoveTomlTopKey(raw, "approval_policy")
			changed = true
		}
	}
	if changed {
		_ = util.WriteFile(p.Config, raw)
	}
}

// codexHasAnyToklessHook reports whether any tokless-managed hook group
// is still present in hooks.json.
func codexHasAnyToklessHook() bool {
	return HasCodexRtkHook() || HasCodexContextModeHook() || HasCodexPermissionHook()
}

// mapChild fetches an OrderedMap child by key.
func mapChild(m *util.OrderedMap, key string) (*util.OrderedMap, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	om, ok := v.(*util.OrderedMap)
	return om, ok
}

func codexKnownBinDirs() []string {
	var dirs []string
	if d := os.Getenv("CODEX_INSTALL_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if util.IsWin {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs, filepath.Join(la, "Programs", "OpenAI", "Codex", "bin"))
		}
	}
	dirs = append(dirs,
		filepath.Join(util.Home(), ".local", "bin"),
		filepath.Join(util.Home(), ".cargo", "bin"),
	)
	return dirs
}

var codex = &core.AgentManifest{
	ID:        "codex",
	Label:     "Codex",
	Homepage:  "https://github.com/openai/codex",
	CLIBin:    "codex",
	ConfigDir: func() string { return util.CodexPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectAgent("codex", util.CodexPathsResolved().Dir, codexKnownBinDirs(), nil)
	},
}

// Register wires all agents into the core registry.
func Register() {
	core.RegisterAgent(claude)
	core.RegisterAgent(opencode)
	core.RegisterAgent(codex)
	core.RegisterAgent(antigravity)
	core.RegisterAgent(copilot)
}
