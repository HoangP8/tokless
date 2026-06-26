package util

import (
	"os"
	"path/filepath"
	"runtime"
)

var IsWin = runtime.GOOS == "windows"

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

func EnsureDir(p string) error { return os.MkdirAll(p, 0o755) }

func ReadFileSafe(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func WriteFile(p, content string) error {
	if err := EnsureDir(filepath.Dir(p)); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o644)
}

func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ClaudePaths holds Claude Code config locations.
type ClaudePaths struct {
	Dir, Settings, GlobalJSON, Instructions, SkillsDir string
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
		SkillsDir:    filepath.Join(dir, "skills"),
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

// AntigravityPaths
type AntigravityPaths struct {
	Dir, McpConfig, McpConfigCLI, Settings, SkillsDir, Instructions string
}

func AntigravityPathsResolved() AntigravityPaths {
	gemini := filepath.Join(Home(), ".gemini")
	dir := filepath.Join(gemini, "antigravity")
	return AntigravityPaths{
		Dir:          dir,
		McpConfig:    filepath.Join(dir, "mcp_config.json"),
		McpConfigCLI: filepath.Join(gemini, "config", "mcp_config.json"),
		Settings:     filepath.Join(gemini, "settings.json"),
		SkillsDir:    filepath.Join(gemini, "config", "skills"),
		Instructions: filepath.Join(gemini, "GEMINI.md"),
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
