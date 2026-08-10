package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func cursorMcpEntry(toolID string) *util.OrderedMap {
	return cursorMcpEntryFor(toolID, false)
}

func cursorMcpEntryFor(toolID string, windowsBridge bool) *util.OrderedMap {
	spawn := util.PickMcpSpawn(toolID)
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("cursor", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
		spawn.Args = append([]string{"run-mcp", "--agent", "cursor", "--workspace", "${workspaceFolder}"}, spawn.Args[3:]...)
	}
	if windowsBridge {
		args := []string{"--"}
		if toolID == "codegraph" {
			filtered := make([]string, 0, len(spawn.Args))
			for i := 0; i < len(spawn.Args); i++ {
				if spawn.Args[i] == "--workspace" && i+1 < len(spawn.Args) && spawn.Args[i+1] == "${workspaceFolder}" {
					i++
					continue
				}
				filtered = append(filtered, spawn.Args[i])
			}
			spawn.Args = filtered
		}
		if distro := os.Getenv("WSL_DISTRO_NAME"); distro != "" {
			args = []string{"-d", distro}
			args = append(args, "--")
		}
		spawn = util.McpSpawn{Command: "wsl.exe", Args: append(args, append([]string{spawn.Command}, spawn.Args...)...)}
	}
	e := util.NewOrderedMap()
	e.Set("type", "stdio")
	e.Set("command", spawn.Command)
	e.Set("args", toAny(spawn.Args))
	return e
}

func cursorMcpPath() string {
	return util.CursorGlobalMcpPath()
}

type cursorConfigTarget struct {
	dir    string
	bridge bool
}

func cursorConfigTargets() []cursorConfigTarget {
	targets := []cursorConfigTarget{{dir: util.CursorPathsResolved().Dir}}
	if os.Getenv("TOKLESS_CURSOR_WINDOWS_BRIDGE") == "1" {
		if winHome := util.WindowsCursorHomeFromWSL(); winHome != "" {
			targets = append(targets, cursorConfigTarget{dir: filepath.Join(winHome, ".cursor"), bridge: true})
		}
	}
	return targets
}

func cursorMcpTargets() []struct {
	path   string
	bridge bool
} {
	targets := cursorConfigTargets()
	out := make([]struct {
		path   string
		bridge bool
	}, len(targets))
	for i, target := range targets {
		out[i] = struct {
			path   string
			bridge bool
		}{filepath.Join(target.dir, "mcp.json"), target.bridge}
	}
	return out
}

func CursorMcpConfigPath() string { return cursorMcpPath() }

func cursorMcpMap(cfg *util.OrderedMap) (*util.OrderedMap, bool) {
	v, ok := cfg.Get("mcpServers")
	if !ok {
		m := util.NewOrderedMap()
		cfg.Set("mcpServers", m)
		return m, true
	}
	m, ok := v.(*util.OrderedMap)
	return m, ok
}

func cursorEntryMatches(v any, want *util.OrderedMap) bool {
	a, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	// Only exact Tokless-owned entries are safe to remove. Extra fields may
	// carry user configuration and must make this matcher refuse ownership.
	if !cursorEntryHasExactFields(a, want) {
		return false
	}
	at, atok := a.Get("type")
	wt, wtok := want.Get("type")
	ac, aok := a.Get("command")
	wc, wok := want.Get("command")
	aa, _ := a.Get("args")
	wa, _ := want.Get("args")
	args, ok := wa.([]any)
	if !ok {
		return false
	}
	wantArgs := make([]string, len(args))
	for i, v := range args {
		var sok bool
		wantArgs[i], sok = v.(string)
		if !sok {
			return false
		}
	}
	return atok && wtok && at == wt && aok && wok && ac == wc && anyStringSliceEqual(aa, wantArgs)
}

func cursorEntryHasExactFields(a, want *util.OrderedMap) bool {
	if a.Len() != want.Len() {
		return false
	}
	for _, key := range want.Keys() {
		if _, ok := a.Get(key); !ok {
			return false
		}
	}
	return true
}

// cursorStaleToklessEntryMatches recognizes an exact managed entry whose only
// difference is the outer tokless executable path.
func cursorStaleToklessEntryMatches(v any, want *util.OrderedMap) bool {
	a, ok := v.(*util.OrderedMap)
	if !ok || !cursorEntryHasExactFields(a, want) {
		return false
	}
	at, atok := a.Get("type")
	wt, wtok := want.Get("type")
	if !atok || !wtok || at != wt || at != "stdio" {
		return false
	}
	ac, acok := a.Get("command")
	wc, wcok := want.Get("command")
	aa, aaok := a.Get("args")
	wa, waok := want.Get("args")
	if !acok || !wcok || !aaok || !waok {
		return false
	}
	oldCommand, oldOK := ac.(string)
	wantCommand, wantOK := wc.(string)
	oldArgs, oldOKArgs := aa.([]any)
	wantArgs, wantOKArgs := wa.([]any)
	if !oldOK || !wantOK || !oldOKArgs || !wantOKArgs {
		return false
	}
	if cursorToklessCodegraphArgs(oldCommand, oldArgs) && cursorToklessCodegraphArgs(wantCommand, wantArgs) {
		return true
	}

	if wantCommand == "wsl.exe" {
		if oldCommand != "wsl.exe" || len(oldArgs) != len(wantArgs) {
			return false
		}
		staleIndex := toklessArgIndex(wantArgs)
		if staleIndex < 0 {
			return false
		}
		for i := range wantArgs {
			if oldArgs[i] == wantArgs[i] {
				continue
			}
			if i == staleIndex && cursorIsToklessCommand(oldArgs[i]) && cursorIsToklessCommand(wantArgs[i]) {
				continue
			}
			return false
		}
		return true
	}

	return cursorIsToklessCommand(oldCommand) && cursorIsToklessCommand(wantCommand) &&
		anyStringSliceEqual(aa, stringArgs(wantArgs))
}

func cursorToklessCodegraphArgs(command string, args []any) bool {
	if command == "wsl.exe" {
		start := -1
		for i, arg := range args {
			if arg == "--" {
				start = i + 1
				break
			}
		}
		if start < 0 || start >= len(args) {
			return false
		}
		command, args = stringValue(args[start]), args[start+1:]
	}
	if !cursorIsToklessCommand(command) || len(args) < 6 ||
		args[0] != "run-mcp" || args[1] != "--agent" || args[2] != "cursor" {
		return false
	}
	target := 3
	if args[target] == "--workspace" {
		if len(args) < target+3 {
			return false
		}
		target += 2
	}
	if target+2 < len(args) && cursorLegacyCodegraphCommand(stringValue(args[target])) &&
		args[target+1] == "serve" && args[target+2] == "--mcp" {
		return true
	}
	return cursorNpxCodegraphArgs(args[target:])
}

func cursorNpxCodegraphArgs(args []any) bool {
	if len(args) != 5 || !cursorNpxCommand(stringValue(args[0])) {
		return false
	}
	return args[1] == "--no-install" && args[2] == "@colbymchenry/codegraph" &&
		args[3] == "serve" && args[4] == "--mcp"
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func toklessArgIndex(args []any) int {
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			return i + 1
		}
	}
	return -1
}

func cursorIsToklessCommand(command any) bool {
	s, ok := command.(string)
	if !ok {
		return false
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(s, "\\", "/")))
	switch base {
	case "tokless", "tokless.exe", "tokless-linux", "tokless-linux-x64", "tokless-linux-arm64", "tokless-darwin", "tokless-darwin-x64", "tokless-darwin-arm64", "tokless-windows", "tokless-windows-x64.exe":
		return true
	default:
		return strings.HasPrefix(strings.ToLower(base), "tokless")
	}
}

func stringArgs(args []any) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i], _ = arg.(string)
	}
	return out
}

func cursorLegacyEntryMatches(v any, want *util.OrderedMap) bool {
	a, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	// Only migrate Cursor's documented direct CodeGraph entry. Do not treat
	// arbitrary commands with a similar tail as Tokless-managed entries. Extra
	// fields are user-owned and make this matcher refuse migration.
	if a.Len() != 3 {
		return false
	}
	t, tok := a.Get("type")
	command, cok := a.Get("command")
	args, aok := a.Get("args")
	commandName, commandOK := command.(string)
	if !tok || t != "stdio" || !cok || !commandOK || !cursorLegacyCodegraphCommand(commandName) || !aok {
		return false
	}
	legacyArgs, ok := args.([]any)
	if !ok || (len(legacyArgs) != 2 && len(legacyArgs) != 4) {
		return false
	}
	if legacyArgs[0] != "serve" || legacyArgs[1] != "--mcp" {
		return false
	}
	if len(legacyArgs) == 4 && (legacyArgs[2] != "--path" || legacyArgs[3] != "${workspaceFolder}") {
		return false
	}

	// Keep matcher specific to desired Cursor CodeGraph entry. This prevents
	// the legacy exception from widening to other tools.
	wantCommand, wcok := want.Get("command")
	wa, wok := want.Get("args")
	wantArgs, ok := wa.([]any)
	if !wok || !wcok || !ok {
		return false
	}
	if wantCommand == "wsl.exe" {
		for i, arg := range wantArgs {
			if arg == "--" && i+1 < len(wantArgs) {
				wantArgs = wantArgs[i+1:]
				break
			}
		}
	}
	for i := 0; i+2 < len(wantArgs); i++ {
		if wantArgs[i] == "run-mcp" && wantArgs[i+1] == "--agent" && wantArgs[i+2] == "cursor" {
			return true
		}
	}
	return false
}

func cursorLegacyCodegraphCommand(command string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/")))
	switch base {
	case "codegraph", "codegraph.exe", "codegraph.cmd", "codegraph.bat":
		return true
	default:
		return false
	}
}

func cursorNpxCommand(command string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/")))
	switch base {
	case "npx", "npx.exe", "npx.cmd", "npx.bat":
		return true
	default:
		return false
	}
}

// ConfigureCursorMcp updates Cursor global MCP config only. Malformed non-empty files are left untouched.
func ConfigureCursorMcp(toolID string) (bool, string) {
	targets := cursorMcpTargets()
	type prepared struct {
		path, before, after string
		changed             bool
	}
	preparedFiles := make([]prepared, 0, len(targets))
	for _, target := range targets {
		p := target.path
		raw, exists := util.ReadFileSafe(p)
		if exists && hasJSONCComment(raw) {
			return false, p
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil && exists && strings.TrimSpace(raw) != "" {
			return false, p
		}
		if cfg == nil {
			cfg = util.NewOrderedMap()
		}
		servers, ok := cursorMcpMap(cfg)
		if !ok {
			return false, p
		}
		desired := cursorMcpEntryFor(toolID, target.bridge)
		if existing, found := servers.Get(toolID); found && !cursorEntryMatches(existing, desired) && !cursorStaleToklessEntryMatches(existing, desired) && !cursorLegacyEntryMatches(existing, desired) {
			return false, p
		}
		if existing, found := servers.Get(toolID); found && cursorEntryMatches(existing, desired) {
			preparedFiles = append(preparedFiles, prepared{path: p, before: raw, changed: false})
			continue
		}
		servers.Set(toolID, desired)
		preparedFiles = append(preparedFiles, prepared{path: p, before: raw, after: util.StringifyJSON(cfg), changed: true})
	}
	changed := false
	for _, file := range preparedFiles {
		if !file.changed {
			continue
		}
		if err := util.WriteFile(file.path, file.after); err != nil {
			for _, prior := range preparedFiles {
				if prior.path == file.path {
					break
				}
				if prior.changed {
					_ = util.WriteFile(prior.path, prior.before)
				}
			}
			return false, file.path
		}
		changed = true
	}
	return changed, cursorMcpPath()
}

func RemoveCursorMcp(toolID string) bool {
	type prepared struct{ path, before, after string }
	var files []prepared
	for _, target := range cursorMcpTargets() {
		raw, ok := util.ReadFileSafe(target.path)
		if !ok {
			continue
		}
		if hasJSONCComment(raw) {
			return false
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			return false
		}
		servers, ok := cursorMcpMapRead(cfg)
		if !ok {
			continue
		}
		v, ok := servers.Get(toolID)
		if !ok {
			continue
		}
		if !cursorEntryMatches(v, cursorMcpEntryFor(toolID, target.bridge)) {
			return false
		}
		servers.Delete(toolID)
		if servers.Len() == 0 {
			cfg.Delete("mcpServers")
		}
		files = append(files, prepared{target.path, raw, util.StringifyJSON(cfg)})
	}
	if len(files) == 0 {
		return false
	}
	for i, file := range files {
		if err := util.WriteFile(file.path, file.after); err != nil {
			for _, prior := range files[:i] {
				_ = util.WriteFile(prior.path, prior.before)
			}
			return false
		}
	}
	return true
}

func cursorMcpMapRead(cfg *util.OrderedMap) (*util.OrderedMap, bool) {
	v, ok := cfg.Get("mcpServers")
	m, mok := v.(*util.OrderedMap)
	return m, ok && mok
}

func CursorMcpHas(toolID string) bool {
	for _, target := range cursorMcpTargets() {
		raw, ok := util.ReadFileSafe(target.path)
		if !ok {
			return false
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			return false
		}
		servers, ok := cursorMcpMapRead(cfg)
		if !ok {
			return false
		}
		v, ok := servers.Get(toolID)
		if !ok || !cursorEntryMatches(v, cursorMcpEntryFor(toolID, target.bridge)) {
			return false
		}
	}
	return true
}

func cursorConfigDir() string {
	return util.CursorPathsResolved().Dir
}

func cursorIDEConfigDirs() []string {
	targets := cursorConfigTargets()
	dirs := make([]string, len(targets))
	for i, target := range targets {
		dirs[i] = target.dir
	}
	return dirs
}

func cursorTargetBridge(path string) bool {
	path = filepath.Clean(path)
	for _, target := range cursorConfigTargets() {
		dir := filepath.Clean(target.dir)
		if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return target.bridge
		}
	}
	return false
}

func cursorHooksFile() string { return filepath.Join(cursorConfigDir(), "hooks.json") }

func cursorHooksFiles() []string {
	dirs := cursorIDEConfigDirs()
	paths := make([]string, len(dirs))
	for i, dir := range dirs {
		paths[i] = filepath.Join(dir, "hooks.json")
	}
	return paths
}

func cursorPermissionsFile() string {
	return filepath.Join(cursorConfigDir(), "permissions.json")
}

func cursorPermissionsFiles() []string {
	dirs := cursorIDEConfigDirs()
	paths := make([]string, len(dirs))
	for i, dir := range dirs {
		paths[i] = filepath.Join(dir, "permissions.json")
	}
	return paths
}
func cursorCLIConfigFile() string {
	return cursorNativeCLIConfigFile()
}

func cursorNativeCLIConfigFile() string {
	if util.IsWin || runtime.GOOS == "darwin" {
		return filepath.Join(util.Home(), ".cursor", "cli-config.json")
	}
	if dir := os.Getenv("CURSOR_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "cli-config.json")
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "cursor", "cli-config.json")
	}
	return filepath.Join(util.Home(), ".cursor", "cli-config.json")
}

func cursorCLIConfigFiles() []string {
	return []string{cursorNativeCLIConfigFile()}
}

func cursorRtkHookCommand() string {
	return cursorRtkHookCommandFor(false)
}
func cursorRtkHookCommandFor(bridge bool) string {
	if bridge {
		args := []string{"--"}
		if distro := os.Getenv("WSL_DISTRO_NAME"); distro != "" {
			args = []string{"-d", cursorWindowsCommandArg(distro), "--"}
		}
		args = append(args, cursorWindowsCommandArg(util.ToklessAbs()), "rtk", "hook", "cursor")
		return strings.Join(append([]string{"wsl.exe"}, args...), " ")
	}
	return "rtk hook cursor"
}

func cursorCodegraphHookCommand() string {
	return cursorCodegraphHookCommandFor(false)
}
func cursorCodegraphHookCommandFor(bridge bool) string {
	args := []string{"cursor-hook", "codegraph-index"}
	if bridge {
		wsl := []string{"--"}
		if distro := os.Getenv("WSL_DISTRO_NAME"); distro != "" {
			wsl = []string{"-d", cursorWindowsCommandArg(distro), "--"}
		}
		return strings.Join(append([]string{"wsl.exe"}, append(wsl, append([]string{cursorWindowsCommandArg(util.ToklessAbs())}, args...)...)...), " ")
	}
	path := util.ToklessAbs()
	if util.IsWin {
		path = cursorWindowsCommandArg(path)
	} else {
		path = cursorPOSIXCommandArg(path)
	}
	return strings.Join(append([]string{path}, args...), " ")
}

func cursorProjectRulesHookCommand() string {
	return cursorProjectRulesHookCommandFor(false)
}

func cursorProjectRulesHookCommandFor(bridge bool) string {
	args := []string{"cursor-hook", "project-rules"}
	if bridge {
		wsl := []string{"--"}
		if distro := os.Getenv("WSL_DISTRO_NAME"); distro != "" {
			wsl = []string{"-d", cursorWindowsCommandArg(distro), "--"}
		}
		return strings.Join(append([]string{"wsl.exe"}, append(wsl, append([]string{cursorWindowsCommandArg(util.ToklessAbs())}, args...)...)...), " ")
	}
	path := util.ToklessAbs()
	if util.IsWin {
		path = cursorWindowsCommandArg(path)
	} else {
		path = cursorPOSIXCommandArg(path)
	}
	return strings.Join(append([]string{path}, args...), " ")
}

func cursorWindowsCommandArg(arg string) string {
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}

func cursorPOSIXCommandArg(arg string) string {
	return `'` + strings.ReplaceAll(arg, `'`, `'\''`) + `'`
}

func cursorOwnedCodegraphHookEntry(v any) bool {
	return cursorOwnedCodegraphHookEntryFor(v, cursorCodegraphHookCommand())
}
func cursorOwnedCodegraphHookEntryFor(v any, want string) bool {
	e, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	if e.Len() != 1 {
		return false
	}
	command, ok := e.Get("command")
	return ok && command == want
}

// HasCursorCodegraphIndexHook reports whether both required Cursor lifecycle
// events contain exactly one exact Tokless-owned CodeGraph hook.
func HasCursorCodegraphIndexHook() bool {
	for _, path := range cursorHooksFiles() {
		if !hasCursorCodegraphIndexHook(path, cursorCodegraphHookCommandFor(cursorTargetBridge(path))) {
			return false
		}
	}
	return true
}

func hasCursorCodegraphIndexHook(path, want string) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := cursorMcpMapReadKey(cfg, "hooks")
	if !ok {
		return false
	}
	for _, event := range []string{"sessionStart", "workspaceOpen"} {
		value, ok := hooks.Get(event)
		if !ok {
			return false
		}
		entries, ok := value.([]any)
		if !ok {
			return false
		}
		count := 0
		for _, entry := range entries {
			if cursorOwnedCodegraphHookEntryFor(entry, want) {
				count++
			}
		}
		if count != 1 {
			return false
		}
	}
	return true
}

func installCursorCodegraphIndexHook(path string) bool {
	p := path
	command := cursorCodegraphHookCommandFor(cursorTargetBridge(path))
	raw, exists := util.ReadFileSafe(p)
	if exists && strings.TrimSpace(raw) != "" && hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil && exists && strings.TrimSpace(raw) != "" {
		return false
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	if version, found := cfg.Get("version"); found && !cursorVersionOne(version) {
		return false
	}
	hooksValue, found := cfg.Get("hooks")
	var hooks *util.OrderedMap
	if found {
		var ok bool
		hooks, ok = hooksValue.(*util.OrderedMap)
		if !ok {
			return false
		}
	} else {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	for _, event := range []string{"sessionStart", "workspaceOpen"} {
		v, found := hooks.Get(event)
		if found {
			if _, ok := v.([]any); !ok {
				return false
			}
		}
		entries, _ := v.([]any)
		kept := make([]any, 0, len(entries)+1)
		for _, entry := range entries {
			if !cursorOwnedCodegraphHookEntryFor(entry, command) {
				kept = append(kept, entry)
			}
		}
		e := util.NewOrderedMap()
		e.Set("command", command)
		kept = append(kept, e)
		hooks.Set(event, kept)
	}
	cfg.Set("version", 1)
	next := util.StringifyJSON(cfg)
	return next == raw || util.WriteFile(p, next) == nil
}

func InstallCursorCodegraphIndexHook() bool {
	paths := cursorHooksFiles()
	before := make([]string, len(paths))
	for i, path := range paths {
		before[i], _ = util.ReadFileSafe(path)
		if !installCursorCodegraphIndexHook(path) {
			for j := 0; j < i; j++ {
				_ = util.WriteFile(paths[j], before[j])
			}
			return false
		}
	}
	return true
}

func removeCursorCodegraphIndexHook(path string) bool {
	p := path
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return true
	}
	if strings.TrimSpace(raw) != "" && hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := cursorMcpMapReadKey(cfg, "hooks")
	if !ok {
		return true
	}
	changed := false
	for _, event := range []string{"sessionStart", "workspaceOpen"} {
		v, found := hooks.Get(event)
		if !found {
			continue
		}
		entries, ok := v.([]any)
		if !ok {
			return false
		}
		kept := make([]any, 0, len(entries))
		for _, entry := range entries {
			if !cursorOwnedCodegraphHookEntryFor(entry, cursorCodegraphHookCommandFor(cursorTargetBridge(p))) {
				kept = append(kept, entry)
			} else {
				changed = true
			}
		}
		if len(kept) == 0 {
			hooks.Delete(event)
		} else {
			hooks.Set(event, kept)
		}
	}
	if hooks.Len() == 0 {
		cfg.Delete("hooks")
	}
	if !changed {
		return true
	}
	return util.WriteFile(p, util.StringifyJSON(cfg)) == nil
}

func RemoveCursorCodegraphIndexHook() bool {
	paths := cursorHooksFiles()
	before := make([]string, len(paths))
	for i, path := range paths {
		before[i], _ = util.ReadFileSafe(path)
		if !removeCursorCodegraphIndexHook(path) {
			for j := 0; j < i; j++ {
				_ = util.WriteFile(paths[j], before[j])
			}
			return false
		}
	}
	return true
}

func cursorOwnedProjectRulesHookEntryFor(v any, want string) bool {
	e, ok := v.(*util.OrderedMap)
	if !ok || e.Len() != 1 {
		return false
	}
	command, ok := e.Get("command")
	return ok && command == want
}

func HasCursorProjectRulesHook() bool {
	for _, path := range cursorHooksFiles() {
		if !hasCursorProjectRulesHook(path, cursorProjectRulesHookCommandFor(cursorTargetBridge(path))) {
			return false
		}
	}
	return true
}

func hasCursorProjectRulesHook(path, want string) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := cursorMcpMapReadKey(cfg, "hooks")
	if !ok {
		return false
	}
	for _, event := range []string{"sessionStart", "workspaceOpen"} {
		v, ok := hooks.Get(event)
		if !ok {
			return false
		}
		entries, ok := v.([]any)
		if !ok {
			return false
		}
		count := 0
		for _, entry := range entries {
			if cursorOwnedProjectRulesHookEntryFor(entry, want) {
				count++
			}
		}
		if count != 1 {
			return false
		}
	}
	return true
}

func installCursorProjectRulesHook(path string) bool {
	raw, exists := util.ReadFileSafe(path)
	if exists && strings.TrimSpace(raw) != "" && hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil && exists && strings.TrimSpace(raw) != "" {
		return false
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	hooksValue, found := cfg.Get("hooks")
	var hooks *util.OrderedMap
	if found {
		var ok bool
		hooks, ok = hooksValue.(*util.OrderedMap)
		if !ok {
			return false
		}
	} else {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	command := cursorProjectRulesHookCommandFor(cursorTargetBridge(path))
	for _, event := range []string{"sessionStart", "workspaceOpen"} {
		v, found := hooks.Get(event)
		if found {
			if _, ok := v.([]any); !ok {
				return false
			}
		}
		entries, _ := v.([]any)
		kept := make([]any, 0, len(entries)+1)
		for _, entry := range entries {
			if !cursorOwnedProjectRulesHookEntryFor(entry, command) {
				kept = append(kept, entry)
			}
		}
		e := util.NewOrderedMap()
		e.Set("command", command)
		kept = append(kept, e)
		hooks.Set(event, kept)
	}
	cfg.Set("version", 1)
	next := util.StringifyJSON(cfg)
	return next == raw || util.WriteFile(path, next) == nil
}

func InstallCursorProjectRulesHook() bool {
	paths := cursorHooksFiles()
	before := make([]string, len(paths))
	for i, path := range paths {
		before[i], _ = util.ReadFileSafe(path)
		if !installCursorProjectRulesHook(path) {
			for j := 0; j < i; j++ {
				_ = util.WriteFile(paths[j], before[j])
			}
			return false
		}
	}
	return true
}

func removeCursorProjectRulesHook(path string) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return true
	}
	if strings.TrimSpace(raw) != "" && hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := cursorMcpMapReadKey(cfg, "hooks")
	if !ok {
		return true
	}
	command := cursorProjectRulesHookCommandFor(cursorTargetBridge(path))
	changed := false
	for _, event := range []string{"sessionStart", "workspaceOpen"} {
		v, found := hooks.Get(event)
		if !found {
			continue
		}
		entries, ok := v.([]any)
		if !ok {
			return false
		}
		kept := make([]any, 0, len(entries))
		for _, entry := range entries {
			if cursorOwnedProjectRulesHookEntryFor(entry, command) {
				changed = true
			} else {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			hooks.Delete(event)
		} else {
			hooks.Set(event, kept)
		}
	}
	if hooks.Len() == 0 {
		cfg.Delete("hooks")
	}
	if !changed {
		return true
	}
	return util.WriteFile(path, util.StringifyJSON(cfg)) == nil
}

func RemoveCursorProjectRulesHook() bool {
	paths := cursorHooksFiles()
	before := make([]string, len(paths))
	for i, path := range paths {
		before[i], _ = util.ReadFileSafe(path)
		if !removeCursorProjectRulesHook(path) {
			for j := 0; j < i; j++ {
				_ = util.WriteFile(paths[j], before[j])
			}
			return false
		}
	}
	return true
}

func cursorVersionOne(v any) bool {
	return v == json.Number("1") || v == float64(1) || v == 1
}

func cursorRtkHookEntry() *util.OrderedMap {
	return cursorRtkHookEntryFor(cursorRtkHookCommand())
}
func cursorRtkHookEntryFor(command string) *util.OrderedMap {
	e := util.NewOrderedMap()
	e.Set("command", command)
	e.Set("matcher", "Shell")
	return e
}

func cursorRtkHookEntryMatches(v any) bool {
	return cursorRtkHookEntryMatchesFor(v, cursorRtkHookCommand())
}
func cursorRtkHookEntryMatchesFor(v any, want string) bool {
	e, ok := v.(*util.OrderedMap)
	if !ok || e.Len() != 2 {
		return false
	}
	command, commandOK := e.Get("command")
	matcher, matcherOK := e.Get("matcher")
	return commandOK && matcherOK && command == want && matcher == "Shell"
}

func cursorToklessLauncher(token string) bool {
	base := token
	if i := strings.LastIndexAny(base, `/\\`); i >= 0 {
		base = base[i+1:]
	}
	return base == "tokless"
}

func cursorOwnedRtkHookCommand(command string) bool {
	if command == "rtk hook cursor" {
		return true
	}
	parts := strings.Fields(command)
	if len(parts) == 4 && cursorToklessLauncher(parts[0]) && parts[1] == "rtk" && parts[2] == "hook" && parts[3] == "cursor" {
		return true
	}
	if len(parts) < 5 || strings.ToLower(filepath.Base(strings.ReplaceAll(parts[0], `\\`, `/`))) != "wsl.exe" {
		return false
	}
	separator := -1
	for i := 1; i < len(parts); i++ {
		if parts[i] == "--" {
			separator = i
			break
		}
	}
	return separator >= 1 && len(parts) == separator+5 && cursorToklessLauncher(parts[separator+1]) &&
		parts[separator+2] == "rtk" && parts[separator+3] == "hook" && parts[separator+4] == "cursor"
}

func cursorOwnedRtkHookEntryMatches(v any) bool {
	e, ok := v.(*util.OrderedMap)
	if !ok || e.Len() != 2 {
		return false
	}
	command, commandOK := e.Get("command")
	matcher, matcherOK := e.Get("matcher")
	commandString, stringOK := command.(string)
	return commandOK && matcherOK && stringOK && cursorOwnedRtkHookCommand(commandString) && matcher == "Shell"
}

// InstallCursorRtkHook writes Cursor's upstream RTK hook entry only.
func installCursorRtkHook(path string) bool {
	p := path
	command := cursorRtkHookCommandFor(cursorTargetBridge(path))
	raw, exists := util.ReadFileSafe(p)
	if exists && strings.TrimSpace(raw) != "" && hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil && exists && strings.TrimSpace(raw) != "" {
		return false
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	} else if version, found := cfg.Get("version"); found && !cursorVersionOne(version) {
		return false
	}
	hooksValue, found := cfg.Get("hooks")
	var hooks *util.OrderedMap
	if found {
		var ok bool
		hooks, ok = hooksValue.(*util.OrderedMap)
		if !ok {
			return false
		}
	} else {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	v, found := hooks.Get("preToolUse")
	if found {
		if _, ok := v.([]any); !ok {
			return false
		}
	}
	entries, _ := v.([]any)
	kept := make([]any, 0, len(entries)+1)
	current := cursorRtkHookEntryFor(command)
	currentCount := 0
	for _, entry := range entries {
		if cursorOwnedRtkHookEntryMatches(entry) || cursorRtkHookEntryMatchesFor(entry, command) {
			if cursorRtkHookEntryMatchesFor(entry, command) {
				currentCount++
			}
			continue
		}
		kept = append(kept, entry)
	}
	if currentCount == 1 {
		kept = append(kept, current)
		if len(kept) == len(entries) {
			return true
		}
	} else {
		kept = append(kept, current)
	}
	// Cursor hook entries use plain JSON values so existing ordered config remains serializable.
	hooks.Set("preToolUse", kept)
	cfg.Set("version", 1)
	next := util.StringifyJSON(cfg)
	if next != raw {
		return util.WriteFile(p, next) == nil
	}
	return true
}

func InstallCursorRtkHook() bool {
	paths := cursorHooksFiles()
	before := make([]string, len(paths))
	for i, path := range paths {
		before[i], _ = util.ReadFileSafe(path)
		if !installCursorRtkHook(path) {
			for j := 0; j < i; j++ {
				_ = util.WriteFile(paths[j], before[j])
			}
			return false
		}
	}
	return true
}

// RemoveCursorRtkHook removes only Cursor's exact upstream RTK hook entry.
func removeCursorRtkHook(path string) bool {
	p := path
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return true
	}
	if strings.TrimSpace(raw) != "" && hasJSONCComment(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if version, found := cfg.Get("version"); found && !cursorVersionOne(version) {
		return false
	}
	hooks, ok := cursorMcpMapReadKey(cfg, "hooks")
	if !ok {
		return true
	}
	items, ok := hooks.Get("preToolUse")
	entries, ok := items.([]any)
	if !ok {
		return false
	}
	kept := make([]any, 0, len(entries))
	for _, entry := range entries {
		if !cursorRtkHookEntryMatchesFor(entry, cursorRtkHookCommandFor(cursorTargetBridge(p))) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == len(entries) {
		return true
	}
	if len(kept) == 0 {
		hooks.Delete("preToolUse")
	} else {
		hooks.Set("preToolUse", kept)
	}
	if hooks.Len() == 0 {
		cfg.Delete("hooks")
	}
	if cfg.Len() == 0 {
		return os.Remove(p) == nil
	}
	return util.WriteFile(p, util.StringifyJSON(cfg)) == nil
}

func RemoveCursorRtkHook() bool {
	paths := cursorHooksFiles()
	before := make([]string, len(paths))
	for i, path := range paths {
		before[i], _ = util.ReadFileSafe(path)
		if !removeCursorRtkHook(path) {
			for j := 0; j < i; j++ {
				_ = util.WriteFile(paths[j], before[j])
			}
			return false
		}
	}
	return true
}

func cursorPermission(path, key, want string, nested bool, remove bool) bool {
	raw, exists := util.ReadFileSafe(path)
	cfg := parseCursorPermissionConfig(raw, exists, !nested)
	if cfg == nil && exists && strings.TrimSpace(raw) != "" {
		return false
	}
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	root := cfg
	if nested {
		v, found := cfg.Get("permissions")
		if found {
			var ok bool
			root, ok = v.(*util.OrderedMap)
			if !ok {
				return false
			}
		} else {
			root = util.NewOrderedMap()
			cfg.Set("permissions", root)
		}
	}
	v, found := root.Get(key)
	items := []any{}
	if found {
		var ok bool
		items, ok = v.([]any)
		if !ok {
			return false
		}
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return false
			}
		}
	}
	kept := make([]any, 0, len(items)+1)
	changed := false
	for _, item := range items {
		if remove && item == want {
			changed = true
			continue
		}
		kept = append(kept, item)
	}
	if !remove {
		for _, item := range items {
			if item == want {
				return true
			}
		}
		kept = append(kept, want)
		changed = true
	}
	if !changed {
		return true
	}
	if len(kept) == 0 {
		root.Delete(key)
	} else {
		root.Set(key, kept)
	}
	if nested && root.Len() == 0 {
		cfg.Delete("permissions")
	}
	if !nested && cfg.Len() == 0 {
		return os.Remove(path) == nil
	}
	return util.WriteFile(path, util.StringifyJSON(cfg)) == nil
}

func parseCursorPermissionConfig(raw string, exists, allowJSONC bool) *util.OrderedMap {
	if !allowJSONC {
		var cfg util.OrderedMap
		if !exists || strings.TrimSpace(raw) == "" {
			return util.NewOrderedMap()
		}
		if strings.TrimSpace(raw)[0] != '{' {
			return nil
		}
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return nil
		}
		return &cfg
	}
	return util.TryParseJsonc(raw)
}

func ConfigureCursorRtkPermissions() bool {
	specs := make([]cursorPermissionSpec, 0, len(cursorPermissionsFiles())+1)
	for _, path := range cursorPermissionsFiles() {
		specs = append(specs, cursorPermissionSpec{path, "terminalAllowlist", "rtk", false})
	}
	for _, path := range cursorCLIConfigFiles() {
		specs = append(specs, cursorPermissionSpec{path, "allow", "Shell(rtk)", true})
	}
	return updateCursorPermissions(specs, false)
}

func ConfigureCursorMcpPermissions(toolID string) bool {
	specs := make([]cursorPermissionSpec, 0, len(cursorPermissionsFiles())+1)
	for _, path := range cursorPermissionsFiles() {
		specs = append(specs, cursorPermissionSpec{path, "mcpAllowlist", toolID + ":*", false})
	}
	for _, path := range cursorCLIConfigFiles() {
		specs = append(specs, cursorPermissionSpec{path, "allow", "Mcp(" + toolID + ":*)", true})
	}
	return updateCursorPermissions(specs, false)
}

func RemoveCursorMcpPermissions(toolID string) bool {
	specs := make([]cursorPermissionSpec, 0, len(cursorPermissionsFiles())+1)
	for _, path := range cursorPermissionsFiles() {
		specs = append(specs, cursorPermissionSpec{path, "mcpAllowlist", toolID + ":*", false})
	}
	for _, path := range cursorCLIConfigFiles() {
		specs = append(specs, cursorPermissionSpec{path, "allow", "Mcp(" + toolID + ":*)", true})
	}
	return updateCursorPermissions(specs, true)
}

func HasCursorMcpPermissions(toolID string) bool {
	specs := make([]cursorPermissionSpec, 0, len(cursorPermissionsFiles())+1)
	for _, path := range cursorPermissionsFiles() {
		specs = append(specs, cursorPermissionSpec{path, "mcpAllowlist", toolID + ":*", false})
	}
	for _, path := range cursorCLIConfigFiles() {
		specs = append(specs, cursorPermissionSpec{path, "allow", "Mcp(" + toolID + ":*)", true})
	}
	return hasCursorPermissions(specs)
}

type cursorPermissionSpec struct {
	path, key, want string
	nested          bool
}

func updateCursorPermissions(specs []cursorPermissionSpec, remove bool) bool {
	type snapshot struct {
		path, raw string
		exists    bool
	}
	snapshots := make([]snapshot, 0, len(specs))
	seen := map[string]bool{}
	for _, spec := range specs {
		if seen[spec.path] {
			continue
		}
		seen[spec.path] = true
		raw, exists := util.ReadFileSafe(spec.path)
		snapshots = append(snapshots, snapshot{spec.path, raw, exists})
	}
	rollback := func() {
		for _, s := range snapshots {
			if s.exists {
				_ = util.WriteFile(s.path, s.raw)
			} else {
				_ = os.Remove(s.path)
			}
		}
	}
	for _, spec := range specs {
		if !cursorPermission(spec.path, spec.key, spec.want, spec.nested, remove) {
			rollback()
			return false
		}
	}
	return true
}

func hasCursorPermissions(specs []cursorPermissionSpec) bool {
	for _, spec := range specs {
		if !cursorPermissionPresent(spec.path, spec.key, spec.want, spec.nested) {
			return false
		}
	}
	return true
}

func cursorPermissionPresent(path, key, want string, nested bool) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	cfg := parseCursorPermissionConfig(raw, true, !nested)
	if cfg == nil {
		return false
	}
	root := cfg
	if nested {
		v, ok := cfg.Get("permissions")
		if !ok {
			return false
		}
		root, ok = v.(*util.OrderedMap)
		if !ok {
			return false
		}
	}
	v, ok := root.Get(key)
	if !ok {
		return false
	}
	items, ok := v.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func RemoveCursorRtkPermissions() bool {
	specs := make([]cursorPermissionSpec, 0, len(cursorPermissionsFiles())+1)
	for _, path := range cursorPermissionsFiles() {
		specs = append(specs, cursorPermissionSpec{path, "terminalAllowlist", "rtk", false})
	}
	for _, path := range cursorCLIConfigFiles() {
		specs = append(specs, cursorPermissionSpec{path, "allow", "Shell(rtk)", true})
	}
	return updateCursorPermissions(specs, true)
}

func HasCursorRtkPermissions() bool {
	specs := make([]cursorPermissionSpec, 0, len(cursorPermissionsFiles())+1)
	for _, path := range cursorPermissionsFiles() {
		specs = append(specs, cursorPermissionSpec{path, "terminalAllowlist", "rtk", false})
	}
	for _, path := range cursorCLIConfigFiles() {
		specs = append(specs, cursorPermissionSpec{path, "allow", "Shell(rtk)", true})
	}
	return hasCursorPermissions(specs)
}

func cursorMcpMapReadKey(cfg *util.OrderedMap, key string) (*util.OrderedMap, bool) {
	v, ok := cfg.Get(key)
	m, mok := v.(*util.OrderedMap)
	return m, ok && mok
}

func HasCursorRtkHook() bool {
	for _, path := range cursorHooksFiles() {
		if !hasCursorRtkHook(path, cursorRtkHookCommandFor(cursorTargetBridge(path))) {
			return false
		}
	}
	return true
}

func hasCursorRtkHook(path, want string) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	version, ok := cfg.Get("version")
	if !ok || !cursorVersionOne(version) {
		return false
	}
	hooks, ok := cursorMcpMapReadKey(cfg, "hooks")
	if !ok {
		return false
	}
	items, ok := hooks.Get("preToolUse")
	entries, ok := items.([]any)
	if !ok {
		return false
	}
	for _, entry := range entries {
		if cursorRtkHookEntryMatchesFor(entry, want) {
			return true
		}
	}
	return false
}

// cursorDetectRoot is used by tests to keep probes out of the real filesystem.
var cursorDetectRoot string

func cursorRootPath(path string) string {
	if cursorDetectRoot == "" {
		return path
	}
	return filepath.Join(cursorDetectRoot, filepath.FromSlash(strings.TrimPrefix(path, "/")))
}

func cursorDesktopPaths() []string {
	switch goosForDetect {
	case "windows":
		var paths []string
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			paths = append(paths, filepath.Join(d, "Programs", "cursor", "Cursor.exe"))
		}
		if d := os.Getenv("ProgramFiles"); d != "" {
			paths = append(paths, filepath.Join(d, "cursor", "Cursor.exe"))
		}
		return paths
	case "darwin":
		return []string{cursorRootPath("/Applications/Cursor.app"), cursorRootPath("/Applications/Cursor Nightly.app")}
	case "linux":
		return []string{
			filepath.Join(util.Home(), ".local", "share", "applications", "cursor.desktop"),
			cursorRootPath("/usr/local/share/applications/cursor.desktop"),
			cursorRootPath("/usr/share/applications/cursor.desktop"),
		}
	default:
		return nil
	}
}

func cursorDesktopFileValid(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	typeApplication, nameCursor, execCursor := false, false, false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "Type=Application" {
			typeApplication = true
		}
		if strings.HasPrefix(line, "Name=") && strings.Contains(strings.ToLower(line[5:]), "cursor") {
			nameCursor = true
		}
		if strings.HasPrefix(line, "Exec=") {
			for _, token := range strings.Fields(line[5:]) {
				token = strings.Trim(token, `"'`)
				if strings.Contains(strings.ToLower(filepath.Base(token)), "cursor") {
					execCursor = true
					break
				}
			}
		}
	}
	return typeApplication && nameCursor && execCursor
}

func cursorMacAppValid(path string) bool {
	if !util.Exists(path) {
		return false
	}
	plist := filepath.Join(path, "Contents", "Info.plist")
	raw, err := os.ReadFile(plist)
	if err != nil {
		return true // App bundle path is official; plist is an optional confirmation.
	}
	return strings.Contains(string(raw), "com.todesktop.230313mzl4w4u92") ||
		strings.Contains(string(raw), "co.anysphere.cursor.nightly")
}

func cursorDesktopInstalled() bool {
	if util.WindowsCursorHomeFromWSL() != "" {
		return true
	}
	for _, p := range cursorDesktopPaths() {
		switch goosForDetect {
		case "linux":
			if cursorDesktopFileValid(p) {
				return true
			}
		case "darwin":
			if cursorMacAppValid(p) {
				return true
			}
		default:
			if util.Exists(p) {
				return true
			}
		}
	}
	return false
}
func cursorKnownBinDirs() []string {
	dirs := []string{filepath.Join(util.Home(), ".local", "bin")}
	if goosForDetect == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs,
				filepath.Join(local, "Programs", "cursor"),
				filepath.Join(local, "Programs", "Cursor", "resources", "app", "bin"),
			)
		}
		dirs = append(dirs, filepath.Join(util.Home(), ".cursor", "bin"))
	}
	return dirs
}

func cursorCLIPath() string {
	for _, name := range []string{"cursor-agent", "agent"} {
		if bin := util.FindBinary(name, cursorKnownBinDirs()); bin != "" {
			return bin
		}
	}
	return ""
}
func cursorCLIIsCursor() bool {
	bin := cursorCLIPath()
	if bin == "" {
		return false
	}
	for _, arg := range [][]string{{"--version"}, {"--help"}} {
		result := util.Run(bin, arg, util.RunOptions{Capture: true, Quiet: true})
		if strings.Contains(strings.ToLower(result.Stdout+"\n"+result.Stderr), "cursor") {
			return true
		}
	}
	return false
}

var cursor = &core.AgentManifest{ID: "cursor", Label: "Cursor", Homepage: "https://www.cursor.com", CLIBin: "agent", ConfigDir: cursorConfigDir, Detect: func() core.Detection {
	cli := cursorCLIIsCursor()
	desktop := cursorDesktopInstalled()
	config := os.Getenv("TOKLESS_TEST") == "1" && util.Exists(cursorConfigDir())
	switch {
	case cli && desktop:
		return core.Detection{Installed: true, Source: "cli+desktop"}
	case cli:
		return core.Detection{Installed: true, Source: "cli"}
	case desktop:
		return core.Detection{Installed: true, Source: "desktop"}
	case config:
		return core.Detection{Installed: true, Source: "config"}
	default:
		return core.Detection{}
	}
}}
