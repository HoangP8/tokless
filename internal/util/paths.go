package util

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

var IsWin = runtime.GOOS == "windows"

// ShQuote quotes a string safely for POSIX shells.
func ShQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SplitCommand splits a command line into arguments respecting single and double quotes.
func SplitCommand(cmd string) []string {
	var tokens []string
	var token strings.Builder
	inSingle, inDouble := false, false
	runes := []rune(cmd)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			if (inDouble && (next == '"' || next == '\\')) || (!inSingle && !inDouble && (next == '"' || next == '\'' || next == ' ' || next == '\t')) {
				token.WriteRune(next)
				i++
				continue
			}
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if (r == ' ' || r == '\t') && !inSingle && !inDouble {
			if token.Len() > 0 {
				tokens = append(tokens, token.String())
				token.Reset()
			}
			continue
		}
		token.WriteRune(r)
	}
	if token.Len() > 0 {
		tokens = append(tokens, token.String())
	}
	return tokens
}

// PersistedToklessCommand builds a shell command stored in an agent config.
func PersistedToklessCommand(exe string, args ...string) string {
	if exe == "" {
		return ""
	}
	if IsWin {
		exe = strings.ReplaceAll(exe, "\\", "/")
		if strings.ContainsAny(exe, " \t") {
			exe = `"` + strings.ReplaceAll(exe, `"`, `\"`) + `"`
		}
	} else if strings.ContainsAny(exe, " \t") {
		exe = ShQuote(exe)
	}
	formattedArgs := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t") {
			if IsWin {
				formattedArgs[i] = `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
			} else {
				formattedArgs[i] = ShQuote(arg)
			}
		} else {
			formattedArgs[i] = arg
		}
	}
	return strings.Join(append([]string{exe}, formattedArgs...), " ")
}

var homeOverride string

// SetHomeOverride redirects all home-relative paths (used by tests/sandbox).
func SetHomeOverride(p string) { homeOverride = p }

func resolveHome() string {
	if IsWin {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			return h
		}
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func Home() string {
	if homeOverride != "" {
		return homeOverride
	}
	return resolveHome()
}

// ToklessAbs returns the absolute path to the running tokless binary.
func ToklessAbs() string {
	exe, err := os.Executable()
	if err != nil {
		return whichToklessOrBare()
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	if strings.ContainsAny(exe, " \t") {
		return whichToklessOrBare()
	}
	if IsGoTestExecutable(exe) {
		return whichToklessOrBare()
	}
	return exe
}

// ToklessPersistedAbs resolves the stable installed executable written into agent config.
func ToklessPersistedAbs() string {
	if raw, ok := ReadFileSafe(InstallMarkerPath()); ok {
		var marker InstallRecord
		if json.Unmarshal([]byte(raw), &marker) == nil && stableExecutable(marker.Path) {
			return marker.Path
		}
	}
	known := []string{filepath.Join(Home(), ".local", "bin", "tokless")}
	if IsWin {
		known = nil
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			known = append(known, filepath.Join(local, "Programs", "tokless", "tokless.exe"))
		}
	} else {
		known = append(known, "/usr/local/bin/tokless")
	}
	for _, path := range known {
		if stableExecutable(path) {
			return path
		}
	}
	if path := Which("tokless"); filepath.IsAbs(path) && stableExecutable(path) {
		return path
	}
	if os.Getenv("TOKLESS_TEST") == "1" {
		return "tokless"
	}
	return ""
}

func stableExecutable(path string) bool {
	if path == "" || !filepath.IsAbs(path) || strings.IndexFunc(path, func(r rune) bool {
		return unicode.IsControl(r) || (unicode.IsSpace(r) && r != ' ')
	}) >= 0 || strings.TrimSpace(path) != path {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if IsWin {
		ext := strings.ToLower(filepath.Ext(path))
		pathext := os.Getenv("PATHEXT")
		if pathext == "" {
			pathext = ".EXE;.CMD;.BAT;.COM"
		}
		for _, candidate := range strings.Split(pathext, ";") {
			if strings.EqualFold(strings.TrimSpace(candidate), ext) {
				return true
			}
		}
		return false
	}
	return info.Mode()&0o111 != 0
}

// IsGoTestExecutable reports go test binaries and go-build cache paths.
func IsGoTestExecutable(exe string) bool {
	base := strings.ToLower(filepath.Base(exe))
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") || strings.Contains(exe, string(filepath.Separator)+"go-build")
}

// ToklessAbsStrict returns the absolute tokless path, or "" when only a bare-name fallback exists.
func ToklessAbsStrict() string {
	exe, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
			exe = resolved
		}
		if !strings.ContainsAny(exe, " \t") && !IsGoTestExecutable(exe) {
			return exe
		}
	}
	if w := Which("tokless"); w != "" && filepath.IsAbs(w) {
		return w
	}
	return ""
}

func whichToklessOrBare() string {
	if w := Which("tokless"); w != "" {
		return w
	}
	return "tokless"
}

// opencodeConfigDir mirrors opencode's own resolution (xdg-basedir under Bun):
func opencodeConfigDir() string {
	if d := os.Getenv("OPENCODE_CONFIG_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "opencode")
	}
	return filepath.Join(Home(), ".config", "opencode")
}

// opencodeConfigDirV2 resolves the OpenCode v2 config dir.
func opencodeConfigDirV2() string {
	if d := os.Getenv("OPENCODE_CONFIG_DIR_V2"); d != "" {
		return d
	}
	v2Dir := filepath.Join(Home(), ".config", "opencode2")
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		v2Dir = filepath.Join(x, "opencode2")
	}
	legacyDir := filepath.Join(Home(), ".opencode2")
	for _, dir := range []string{v2Dir, legacyDir} {
		for _, name := range []string{"opencode2.jsonc", "opencode2.json", "opencode2.config.json", "opencode.jsonc", "opencode.json", "config.json"} {
			if Exists(filepath.Join(dir, name)) {
				return dir
			}
		}
	}
	for _, dir := range []string{v2Dir, legacyDir} {
		if Exists(dir) {
			return dir
		}
	}
	return opencodeConfigDir()
}

func EnsureDir(p string) error { return os.MkdirAll(p, 0o755) }

func ReadFileSafe(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

var writeFileOverride func(string, string) error

// SetWriteFileOverride installs a narrow persistence seam for tests.
func SetWriteFileOverride(fn func(string, string) error) { writeFileOverride = fn }

func WriteFile(p, content string) error {
	if writeFileOverride != nil {
		return writeFileOverride(p, content)
	}
	if err := EnsureDir(filepath.Dir(p)); err != nil {
		return err
	}
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return os.WriteFile(p, []byte(content), 0o644)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(p); err == nil {
		mode = info.Mode().Perm()
	}
	return writeFileAtomic(p, content, mode)
}

// WriteFileAtomic replaces a file without exposing a truncated intermediate.
func WriteFileAtomic(p, content string, mode os.FileMode) error {
	if writeFileOverride != nil {
		return writeFileOverride(p, content)
	}
	if err := EnsureDir(filepath.Dir(p)); err != nil {
		return err
	}
	return writeFileAtomic(p, content, mode)
}

func writeFileAtomic(p, content string, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tokless-write-*")
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
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceFile(tmpPath, p)
}

// WriteFileMode writes content, using mode for new files.
func WriteFileMode(p, content string, mode os.FileMode) error {
	if writeFileOverride != nil {
		return writeFileOverride(p, content)
	}
	if err := EnsureDir(filepath.Dir(p)); err != nil {
		return err
	}
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return os.WriteFile(p, []byte(content), mode)
	}
	if info, err := os.Stat(p); err == nil {
		mode = info.Mode().Perm()
	}
	return writeFileAtomic(p, content, mode)
}

func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ClaudePaths holds Claude Code config locations.
type ClaudePaths struct {
	Dir, Settings, GlobalJSON, Instructions string
}

func ClaudeCodePaths() ClaudePaths {
	h := Home()
	dir := filepath.Join(h, ".claude")
	globalJSON := filepath.Join(h, ".claude.json")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		dir = d
		globalJSON = filepath.Join(d, ".claude.json")
	}
	return ClaudePaths{
		Dir:          dir,
		Settings:     filepath.Join(dir, "settings.json"),
		GlobalJSON:   globalJSON,
		Instructions: filepath.Join(dir, "CLAUDE.md"),
	}
}

// OpenCodePaths holds OpenCode config locations.
type OpenCodePaths struct {
	Dir, Config, Instructions, PluginsDir, RulesDir string
}

func OpenCodePathsResolved() OpenCodePaths {
	dir := opencodeConfigDir()
	candidates := []string{
		filepath.Join(dir, "opencode.jsonc"),
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, "config.json"),
	}
	config := filepath.Join(dir, "opencode.jsonc")
	if custom := os.Getenv("OPENCODE_CONFIG"); custom != "" {
		config = custom
		candidates = nil
	}
	for _, c := range candidates {
		if Exists(c) {
			config = c
			break
		}
	}
	return OpenCodePaths{
		Dir:          dir,
		Config:       config,
		Instructions: filepath.Join(dir, "AGENTS.md"),
		PluginsDir:   filepath.Join(dir, "plugins"),
		RulesDir:     filepath.Join(dir, "rules"),
	}
}

func OpenCodePathsV2() OpenCodePaths {
	dir := opencodeConfigDirV2()
	config := filepath.Join(dir, "opencode.jsonc")
	for _, name := range []string{"opencode2.jsonc", "opencode2.json", "opencode2.config.json", "opencode.jsonc", "opencode.json", "config.json"} {
		candidate := filepath.Join(dir, name)
		if Exists(candidate) {
			config = candidate
			break
		}
	}
	return OpenCodePaths{
		Dir:          dir,
		Config:       config,
		Instructions: filepath.Join(dir, "AGENTS.md"),
		PluginsDir:   filepath.Join(dir, "plugins"),
		RulesDir:     filepath.Join(dir, "rules"),
	}
}

// KiloPaths holds Kilo's shared configuration location.
type KiloPaths struct {
	Dir, Config, PluginsDir string
}

func KiloPathsResolved() KiloPaths {
	dir := os.Getenv("KILO_CONFIG_DIR")
	if strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(Home(), strings.TrimPrefix(dir, "~/"))
	}
	if dir == "" {
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			dir = filepath.Join(x, "kilo")
		} else {
			dir = filepath.Join(Home(), ".config", "kilo")
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return KiloPaths{Dir: dir, Config: filepath.Join(dir, "kilo.jsonc"), PluginsDir: filepath.Join(dir, "plugin")}
}

// ClinePaths holds Cline config locations.
type ClinePaths struct {
	Dir, DataDir, McpConfig, HooksDir, RulesDir, Instructions string
}

func ClinePathsResolved() ClinePaths {
	dir := os.Getenv("CLINE_DIR")
	if strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(Home(), strings.TrimPrefix(dir, "~/"))
	}
	if dir == "" {
		dir = filepath.Join(Home(), ".cline")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dataDir := os.Getenv("CLINE_DATA_DIR")
	if strings.HasPrefix(dataDir, "~/") {
		dataDir = filepath.Join(Home(), strings.TrimPrefix(dataDir, "~/"))
	}
	if dataDir == "" {
		dataDir = filepath.Join(dir, "data")
	}
	mcpConfig := os.Getenv("CLINE_MCP_SETTINGS_PATH")
	if strings.HasPrefix(mcpConfig, "~/") {
		mcpConfig = filepath.Join(Home(), strings.TrimPrefix(mcpConfig, "~/"))
	}
	if mcpConfig == "" {
		mcpConfig = filepath.Join(dataDir, "settings", "cline_mcp_settings.json")
	}
	rulesDir := filepath.Join(dir, "rules")
	return ClinePaths{
		Dir:          dir,
		DataDir:      dataDir,
		McpConfig:    mcpConfig,
		HooksDir:     filepath.Join(dir, "hooks"),
		RulesDir:     rulesDir,
		Instructions: filepath.Join(rulesDir, "AGENTS.md"),
	}
}

// CodexPaths holds Codex config locations.
type CodexPaths struct {
	Dir, Config, Instructions string
}

func CodexPathsResolved() CodexPaths {
	dir := filepath.Join(Home(), ".codex")
	if env := os.Getenv("CODEX_HOME"); env != "" {
		dir = env
	}
	return CodexPaths{
		Dir:          dir,
		Config:       filepath.Join(dir, "config.toml"),
		Instructions: filepath.Join(dir, "AGENTS.md"),
	}
}

// AntigravityPaths holds Antigravity config locations.
type AntigravityPaths struct {
	Dir, McpConfig, McpConfigCLI, Settings, Instructions string
}

func AntigravityPathsResolved() AntigravityPaths {
	gemini := filepath.Join(Home(), ".gemini")
	dir := filepath.Join(gemini, "antigravity")
	return AntigravityPaths{
		Dir:          dir,
		McpConfig:    filepath.Join(dir, "mcp_config.json"),
		McpConfigCLI: filepath.Join(gemini, "config", "mcp_config.json"),
		Settings:     filepath.Join(gemini, "settings.json"),
		Instructions: filepath.Join(gemini, "GEMINI.md"),
	}
}

// CopilotPaths holds GitHub Copilot CLI config locations.
type CopilotPaths struct {
	Dir, McpConfig, Settings, HooksDir, Instructions string
}

func CopilotPathsResolved() CopilotPaths {
	dir := filepath.Join(Home(), ".copilot")
	if env := os.Getenv("COPILOT_HOME"); env != "" {
		dir = env
	} else if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		dir = filepath.Join(x, "copilot")
	}
	return CopilotPaths{
		Dir:          dir,
		McpConfig:    filepath.Join(dir, "mcp-config.json"),
		Settings:     filepath.Join(dir, "settings.json"),
		HooksDir:     filepath.Join(dir, "hooks"),
		Instructions: filepath.Join(dir, "copilot-instructions.md"),
	}
}

// CursorPaths holds Cursor's user configuration locations.
type CursorPaths struct {
	Dir, McpConfig string
}

func CursorPathsResolved() CursorPaths {
	dir := filepath.Join(Home(), ".cursor")
	if !IsWin && runtime.GOOS != "darwin" {
		if env := os.Getenv("CURSOR_CONFIG_DIR"); env != "" {
			dir = env
		} else if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			dir = filepath.Join(x, "cursor")
		}
	}
	return CursorPaths{Dir: dir, McpConfig: filepath.Join(dir, "mcp.json")}
}

func CursorGlobalMcpPath() string { return CursorPathsResolved().McpConfig }

type ZcodePaths struct {
	Dir, Config, Instructions, McpConfig string
}

func ZcodePathsResolved() ZcodePaths {
	home := os.Getenv("ZCODE_DIST_HOME")
	if strings.HasPrefix(home, "~/") {
		home = filepath.Join(Home(), strings.TrimPrefix(home, "~/"))
	}
	if home == "" {
		home = filepath.Join(Home(), ".zcode", "runtime")
	}
	return ZcodePaths{
		Dir:          home,
		Config:       filepath.Join(Home(), ".zcode", "cli", "config.json"),
		Instructions: filepath.Join(Home(), ".zcode", "AGENTS.md"),
		McpConfig:    filepath.Join(Home(), ".zcode", "cli", "config.json"),
	}
}

// KimiPaths holds Kimi Code CLI paths.
type KimiPaths struct {
	Dir, Config, Instructions, McpConfig string
}

func KimiPathsResolved() KimiPaths {
	home := os.Getenv("KIMI_CODE_HOME")
	if strings.HasPrefix(home, "~/") {
		home = filepath.Join(Home(), strings.TrimPrefix(home, "~/"))
	}
	if home == "" {
		home = filepath.Join(Home(), ".kimi-code")
	}
	return KimiPaths{
		Dir:          home,
		Config:       filepath.Join(home, "config.toml"),
		Instructions: filepath.Join(home, "AGENTS.md"),
		McpConfig:    filepath.Join(home, "mcp.json"),
	}
}

// AmpPaths holds Amp CLI paths.
type AmpPaths struct {
	Dir, Config, Instructions string
}

func AmpPathsResolved() AmpPaths {
	dir := os.Getenv("AMP_CONFIG_DIR")
	if strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(Home(), strings.TrimPrefix(dir, "~/"))
	}
	if dir == "" {
		dir = filepath.Join(Home(), ".config", "amp")
	}
	return AmpPaths{
		Dir:          dir,
		Config:       filepath.Join(dir, "settings.json"),
		Instructions: filepath.Join(dir, "AGENTS.md"),
	}
}

// CopyDirMerge recursively copies src into dst, overwriting files.
func CopyDirMerge(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		mode := os.FileMode(0o644)
		if info != nil && info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		return os.WriteFile(target, b, mode)
	})
}
