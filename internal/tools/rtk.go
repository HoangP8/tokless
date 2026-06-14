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
		dest := filepath.Join(util.Home(), ".local", "bin")
		_ = os.MkdirAll(dest, 0o755)
		rtkPath := filepath.Join(dest, "rtk")
		_ = os.Remove(rtkPath)
		_ = os.WriteFile(rtkPath, []byte("#!/bin/sh\necho ok"), 0o755)
		sep := ":"
		if util.IsWin {
			sep = ";"
		}
		cur := os.Getenv("PATH")
		if !strings.Contains(sep+cur+sep, sep+dest+sep) {
			os.Setenv("PATH", dest+sep+cur)
		}
		return true, nil
	}
	opts.Reportf("checking", 0.1)
	if util.Which("rtk") != "" && !opts.Upgrade {
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
			hook.Set("command", "rtk hook claude")
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
			if strings.Contains(h.Command, "rtk hook") {
				return true
			}
		}
	}
	return false
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
		r := util.Run("rtk", args, util.RunOptions{Capture: true})
		if r.Code != 0 {
			util.L.Debug("rtk init exited " + clip(r.Stderr))
			return false, nil
		}
		v := util.Run("rtk", []string{"init", "--show"}, util.RunOptions{Capture: true})
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
		"codex":       rtkWire("codex"),
		"antigravity": rtkWireAntigravity(),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude": func(core.RunOpts) (bool, error) {
			util.Run("rtk", []string{"init", "--uninstall", "--agent", "claude"}, util.RunOptions{})
			return true, nil
		},
		"opencode": func(core.RunOpts) (bool, error) {
			util.Run("rtk", []string{"init", "--uninstall", "--agent", "opencode"}, util.RunOptions{})
			return true, nil
		},
		"codex": func(core.RunOpts) (bool, error) {
			util.Run("rtk", []string{"init", "--uninstall", "--agent", "codex"}, util.RunOptions{})
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
			return core.BoolPtr(util.Exists(filepath.Join(util.CodexPathsResolved().Dir, "RTK.md")))
		},
		"antigravity": func() *bool {
			return core.BoolPtr(agents.HasAntigravityRtkHook())
		},
	},
}
