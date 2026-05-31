package util

import (
	"os"
	"path/filepath"
	"runtime"
)

var (
	IsWin = runtime.GOOS == "windows"
	IsMac = runtime.GOOS == "darwin"
)

var homeOverride string

// SetHomeOverride redirects all home-relative paths (used by tests/sandbox).
func SetHomeOverride(p string) { homeOverride = p }

func resolveHome() string {
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

func AppDataDir() string {
	h := resolveHome()
	if IsWin {
		if a := os.Getenv("APPDATA"); a != "" {
			return a
		}
		return filepath.Join(h, "AppData", "Roaming")
	}
	if IsMac {
		return filepath.Join(h, "Library", "Application Support")
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x
	}
	return filepath.Join(h, ".config")
}

func ConfigRoot() string {
	if homeOverride != "" {
		if IsWin {
			return filepath.Join(homeOverride, "AppData", "Roaming")
		}
		if IsMac {
			return filepath.Join(homeOverride, "Library", "Application Support")
		}
		return filepath.Join(homeOverride, ".config")
	}
	return AppDataDir()
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
	return ClaudePaths{
		Dir:          filepath.Join(h, ".claude"),
		Settings:     filepath.Join(h, ".claude", "settings.json"),
		GlobalJSON:   filepath.Join(h, ".claude.json"),
		Instructions: filepath.Join(h, ".claude", "CLAUDE.md"),
		SkillsDir:    filepath.Join(h, ".claude", "skills"),
	}
}

// OpenCodePaths holds OpenCode config locations.
type OpenCodePaths struct {
	Dir, Config, Instructions, PluginsDir, RulesDir string
}

func OpenCodePathsResolved() OpenCodePaths {
	dir := filepath.Join(ConfigRoot(), "opencode")
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
