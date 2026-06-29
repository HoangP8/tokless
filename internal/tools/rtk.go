package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func rtkAssetForThisPlatform() string {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	switch runtime.GOOS {
	case "darwin":
		return "rtk-" + arch + "-apple-darwin.tar.gz"
	case "linux":
		if runtime.GOARCH == "arm64" {
			return "rtk-aarch64-unknown-linux-gnu.tar.gz"
		}
		return "rtk-x86_64-unknown-linux-musl.tar.gz"
	case "windows":
		return "rtk-" + arch + "-pc-windows-msvc.zip"
	}
	return ""
}

func rtkEnsureInstalled(opts core.RunOpts) (bool, error) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		shimDir := filepath.Join(os.TempDir(), "tokless-test-rtk")
		_ = os.MkdirAll(shimDir, 0o755)
		shimPath := filepath.Join(shimDir, "rtk")
		_ = os.Remove(shimPath)
		if util.IsWin {
			_ = os.WriteFile(shimPath+".bat", []byte("@echo ok"), 0o755)
		} else {
			_ = os.WriteFile(shimPath, []byte("#!/bin/sh\necho ok"), 0o755)
		}
		sep := ":"
		if util.IsWin {
			sep = ";"
		}
		cur := os.Getenv("PATH")
		os.Setenv("PATH", shimDir+sep+cur)
		return true, nil
	}
	opts.Reportf("checking", 0.1)
	if p := util.ResolveRtkBin(); p != "" && !opts.Upgrade {
		opts.Reportf("already installed", 1)
		return true, nil
	}
	if opts.DryRun {
		if opts.Upgrade {
			util.L.Sub("[dry-run] would re-download latest rtk binary")
		} else {
			util.L.Sub("[dry-run] would download prebuilt rtk binary")
		}
		return true, nil
	}
	if asset := rtkAssetForThisPlatform(); asset != "" && rtkInstallPrebuilt(asset, opts) {
		opts.Reportf("ready", 1)
		return true, nil
	}
	if !util.IsWin && util.Which("curl") != "" && util.Which("sh") != "" {
		r := util.Run("sh", []string{"-c", "curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/master/install.sh | sh"}, util.RunOptions{})
		if r.Code == 0 {
			return true, nil
		}
	}
	if util.Which("cargo") == "" {
		util.InstallCargo()
	}
	if util.Which("cargo") != "" {
		r := util.Run("cargo", []string{"install", "--git", "https://github.com/rtk-ai/rtk"}, util.RunOptions{})
		if r.Code == 0 {
			return true, nil
		}
	}
	util.L.Err("Cannot install rtk on this platform. See https://github.com/rtk-ai/rtk for manual install.")
	return false, nil
}

func rtkInstallPrebuilt(asset string, opts core.RunOpts) bool {
	url := "https://github.com/rtk-ai/rtk/releases/latest/download/" + asset
	dest := filepath.Join(util.Home(), ".local", "bin")
	_ = os.MkdirAll(dest, 0o755)
	opts.Reportf("downloading binary", 0.3)
	util.L.Sub("downloading " + asset + "…")
	if util.IsWin {
		ps := strings.Join([]string{
			"$ErrorActionPreference='Stop'",
			"Invoke-WebRequest -UseBasicParsing -Uri '" + url + "' -OutFile $env:TEMP\\rtk.zip",
			"Expand-Archive -Force -Path $env:TEMP\\rtk.zip -DestinationPath '" + dest + "'",
			"Remove-Item $env:TEMP\\rtk.zip",
		}, "; ")
		if util.Run("powershell", []string{"-NoProfile", "-Command", ps}, util.RunOptions{}).Code != 0 {
			return false
		}
		util.PrependProcessPath(dest)
		return true
	}
	opts.Reportf("extracting", 0.8)
	if err := util.DownloadAndExtractTarGz(url, dest); err != nil {
		return false
	}
	rtkBin := filepath.Join(dest, "rtk")
	_ = os.Chmod(rtkBin, 0o755)
	if !util.Exists(rtkBin) {
		return false
	}
	if !util.BinaryHealthy(rtkBin) {
		util.L.Debug("rtk prebuilt binary failed --version probe; trying fallback installers")
		_ = os.Remove(rtkBin)
		return false
	}
	util.PrependProcessPath(dest)
	return true
}

func rtkTestShim(agent string) {
	switch agent {
	case "codex":
		dir := util.CodexPathsResolved().Dir
		_ = os.MkdirAll(dir, 0o755)
		stub := "# RTK\nInstalled by tokless. See https://github.com/rtk-ai/rtk\n"
		writeIfMissing(filepath.Join(dir, "AGENTS.md"), stub)
		writeIfMissing(filepath.Join(dir, "RTK.md"), stub)
	case "claude":
		cp := util.ClaudeCodePaths()
		dir := cp.Dir
		_ = os.MkdirAll(dir, 0o755)
		writeIfMissing(filepath.Join(dir, "RTK.md"), "# RTK\nInstalled by tokless.\n")
		settingsPath := cp.Settings
		if !claudeSettingsHasRtkHook(settingsPath) {
			cfg := util.NewOrderedMap()
			if raw, ok := util.ReadFileSafe(settingsPath); ok {
				if m := util.TryParseJsonc(raw); m != nil {
					cfg = m
				}
			}
			hooks := getOrCreateMapT(cfg, "hooks")
			var pre []any
			if v, ok := hooks.Get("PreToolUse"); ok {
				if arr, ok := v.([]any); ok {
					pre = arr
				}
			}
			entry := util.NewOrderedMap()
			entry.Set("matcher", "Bash")
			hook := util.NewOrderedMap()
			hook.Set("type", "command")
			hook.Set("command", "tokless rtk-hook claude")
			entry.Set("hooks", []any{hook})
			pre = append(pre, entry)
			hooks.Set("PreToolUse", pre)
			_ = util.WriteFile(settingsPath, util.StringifyJSON(cfg))
		}
	case "opencode":
		dir := util.OpenCodePathsResolved().PluginsDir
		_ = os.MkdirAll(dir, 0o755)
		writeIfMissing(filepath.Join(dir, "rtk.ts"), "// rtk plugin shim (tokless test mode)\nexport const Plugin = async () => ({});\n")
	case "antigravity":
		dir := filepath.Join(util.Home(), ".gemini", "antigravity-cli")
		_ = os.MkdirAll(dir, 0o755)
		writeIfMissing(filepath.Join(dir, "settings.json"),
			`{"hooks":{"BeforeTool":[{"matcher":"run_shell_command","hooks":[{"type":"command","command":"~/.gemini/hooks/rtk-hook-gemini.sh"}]}]}}`+"\n")
	}
}

func claudeSettingsHasRtkHook(settingsPath string) bool {
	raw, ok := util.ReadFileSafe(settingsPath)
	if !ok {
		return false
	}
	var s struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if json.Unmarshal([]byte(raw), &s) != nil {
		return false
	}
	for _, e := range s.Hooks.PreToolUse {
		for _, h := range e.Hooks {
			if strings.Contains(h.Command, "rtk hook") || strings.Contains(h.Command, "rtk-hook claude") {
				return true
			}
		}
	}
	return false
}

// overrideClaudeRtkHook replaces rtk's own "rtk hook claude" PreToolUse hook command
// with the tokless wrapper so the output includes explicit permissionDecision: "allow".
func overrideClaudeRtkHook() {
	cp := util.ClaudeCodePaths()
	tok := toklessAbs()
	newCmd := tok + " rtk-hook claude"
	raw, ok := util.ReadFileSafe(cp.Settings)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	hooks, ok := cfg.Get("hooks")
	if !ok {
		return
	}
	hm, ok := hooks.(*util.OrderedMap)
	if !ok {
		return
	}
	ptVal, ok := hm.Get("PreToolUse")
	if !ok {
		return
	}
	pt, ok := ptVal.([]any)
	if !ok {
		return
	}
	changed := false
	for _, g := range pt {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			continue
		}
		hooksVal, ok := gm.Get("hooks")
		if !ok {
			continue
		}
		arr, ok := hooksVal.([]any)
		if !ok {
			continue
		}
		for _, h := range arr {
			hm2, ok := h.(*util.OrderedMap)
			if !ok {
				continue
			}
			if c, ok := hm2.Get("command"); ok {
				if s, ok := c.(string); ok && strings.Contains(s, "rtk hook claude") && !strings.Contains(s, "rtk-hook claude") {
					hm2.Set("command", newCmd)
					changed = true
				}
			}
		}
	}
	if changed {
		_ = util.WriteFile(cp.Settings, util.StringifyJSON(cfg))
	}
	if len(pt) > 1 {
		seen := map[string]bool{}
		dedup := make([]any, 0, len(pt))
		deduped := false
		for _, g := range pt {
			cmd := firstHookCommand(g)
			if seen[cmd] {
				deduped = true
				continue
			}
			seen[cmd] = true
			dedup = append(dedup, g)
		}
		if deduped {
			hm.Set("PreToolUse", dedup)
			_ = util.WriteFile(cp.Settings, util.StringifyJSON(cfg))
		}
	}
	agents.AllowClaudeBashPattern("Bash(rtk *)")
	_ = os.Remove(filepath.Join(cp.Dir, "RTK.md"))
	stripRtkRefFromMd(filepath.Join(cp.Dir, "CLAUDE.md"))
}


func firstHookCommand(g any) string {
	gm, ok := g.(*util.OrderedMap)
	if !ok {
		return ""
	}
	hooksVal, ok := gm.Get("hooks")
	if !ok {
		return ""
	}
	arr, ok := hooksVal.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	first, ok := arr[0].(*util.OrderedMap)
	if !ok {
		return ""
	}
	c, _ := first.Get("command")
	s, _ := c.(string)
	return s
}

func toklessAbs() string {
	exe, err := os.Executable()
	if err != nil {
		return "tokless"
	}
	if strings.ContainsAny(exe, " \t") {
		return "tokless"
	}
	return exe
}

// stripRtkRefFromMd removes only the @RTK.md reference line from a markdown
// file (CLAUDE.md, AGENTS.md, GEMINI.md), preserving all other user content.
func stripRtkRefFromMd(path string) {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return
	}
	lines := strings.Split(raw, "\n")
	var kept []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "@") && strings.HasSuffix(t, "RTK.md") {
			continue
		}
		kept = append(kept, l)
	}
	result := strings.TrimSpace(strings.Join(kept, "\n"))
	if result == "" {
		_ = os.Remove(path)
		return
	}
	_ = util.WriteFile(path, result+"\n")
}

func rtkWireAntigravity() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install agy PreToolUse hook (~/.gemini/config/hooks.json) routing shell commands through rtk")
			return true, nil
		}
		agents.InstallAntigravityRtkHook()
		if wd, err := os.Getwd(); err == nil {
			_ = os.Remove(filepath.Join(wd, ".agents", "rules", "antigravity-rtk-rules.md"))
		}
		return true, nil
	}
}

func rtkWireCodex() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install codex PreToolUse hook (~/.codex/hooks.json) routing shell commands through rtk, pre-trusted in config.toml")
			return true, nil
		}
		agents.InstallCodexRtkHook()
		return true, nil
	}
}

func rtkWire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		args := []string{"init", "-g"}
		switch agent {
		case "opencode":
			args = append(args, "--opencode")
		case "codex":
			args = append(args, "--codex")
		default: // claude
			args = append(args, "--auto-patch")
		}
		if opts.DryRun {
			util.L.Sub("[dry-run] would run: rtk " + strings.Join(args, " "))
			return true, nil
		}
	if os.Getenv("TOKLESS_TEST") == "1" {
		rtkTestShim(agent)
		return true, nil
	}
	rtkPath := util.ResolveRtkBin()
	if rtkPath == "" {
		util.L.Err("rtk binary not found on PATH or known install dirs")
		return false, nil
	}
	r := util.Run(rtkPath, args, util.RunOptions{Capture: true})
	if r.Code != 0 {
		util.L.Debug("rtk init exited " + clip(r.Stderr))
		return false, nil
	}
	if agent == "claude" {
		overrideClaudeRtkHook()
	}
	v := util.Run(rtkPath, []string{"init", "--show"}, util.RunOptions{Capture: true})
	if v.Code != 0 {
		util.L.Err("rtk init --show failed: " + clip(v.Stderr))
		return false, nil
	}
	return true, nil
}
}

var rtk = &core.ToolManifest{
	ID:          "rtk",
	Label:       "RTK",
	Description: "Token-efficient command-runner replacing shell commands with deterministic primitives.",
	Homepage:    "https://github.com/rtk-ai/rtk",
	InstallHint: "Prebuilt binary from GitHub releases (no Rust required).",
	Channel:     core.ChannelGitHub,
	Install:     rtkEnsureInstalled,
	WireFor: map[string]core.AgentFn{
		"claude":      rtkWire("claude"),
		"opencode":    rtkWire("opencode"),
		"codex":       rtkWireCodex(),
		"antigravity": rtkWireAntigravity(),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude": func(core.RunOpts) (bool, error) {
			if p := util.ResolveRtkBin(); p != "" {
				util.Run(p, []string{"init", "--uninstall", "--agent", "claude"}, util.RunOptions{})
			}
			return true, nil
		},
		"opencode": func(core.RunOpts) (bool, error) {
			if p := util.ResolveRtkBin(); p != "" {
				util.Run(p, []string{"init", "--uninstall", "--agent", "opencode"}, util.RunOptions{})
			}
			return true, nil
		},
		"codex": func(core.RunOpts) (bool, error) {
			agents.RemoveCodexRtkHook()
			return true, nil
		},
		"antigravity": func(core.RunOpts) (bool, error) {
			agents.RemoveAntigravityRtkHook()
			return true, nil
		},
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude": func() *bool {
			return core.BoolPtr(claudeSettingsHasRtkHook(util.ClaudeCodePaths().Settings))
		},
		"opencode": func() *bool {
			return core.BoolPtr(util.Exists(filepath.Join(util.OpenCodePathsResolved().PluginsDir, "rtk.ts")))
		},
		"codex": func() *bool {
			return core.BoolPtr(agents.HasCodexRtkHook())
		},
		"antigravity": func() *bool {
			return core.BoolPtr(agents.HasAntigravityRtkHook())
		},
	},
}


