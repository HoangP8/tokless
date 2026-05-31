package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
		return util.Run("powershell", []string{"-NoProfile", "-Command", ps}, util.RunOptions{}).Code == 0
	}
	tmp := filepath.Join(os.TempDir(), asset)
	if util.Run("sh", []string{"-c", "curl -fsSL '" + url + "' -o '" + tmp + "'"}, util.RunOptions{}).Code != 0 {
		return false
	}
	opts.Reportf("extracting", 0.8)
	ex := util.Run("sh", []string{"-c", "tar -xzf '" + tmp + "' -C '" + dest + "' && rm -f '" + tmp + "' && chmod +x '" + dest + "/rtk'"}, util.RunOptions{})
	return ex.Code == 0
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
		dir := filepath.Join(util.Home(), ".claude")
		_ = os.MkdirAll(dir, 0o755)
		writeIfMissing(filepath.Join(dir, "RTK.md"), "# RTK\nInstalled by tokless.\n")
		settingsPath := filepath.Join(dir, "settings.json")
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
		dir := filepath.Join(util.Home(), ".config", "opencode", "plugins")
		_ = os.MkdirAll(dir, 0o755)
		writeIfMissing(filepath.Join(dir, "rtk.ts"), "// rtk plugin shim (tokless test mode)\nexport const Plugin = async () => ({});\n")
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

func rtkWire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			flag := ""
			if agent == "opencode" {
				flag = " --opencode"
			} else if agent == "codex" {
				flag = " --codex"
			}
			util.L.Sub("[dry-run] would run: rtk init -g" + flag)
			return true, nil
		}
		if os.Getenv("TOKLESS_TEST") == "1" {
			rtkTestShim(agent)
			return true, nil
		}
		args := []string{"init", "-g"}
		if agent == "opencode" {
			args = append(args, "--opencode")
		} else if agent == "codex" {
			args = append(args, "--codex")
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
		"claude":   rtkWire("claude"),
		"opencode": rtkWire("opencode"),
		"codex":    rtkWire("codex"),
	},
	VerifyFor: map[string]core.VerifyFn{
		"claude": func() *bool {
			dir := filepath.Join(util.Home(), ".claude")
			return core.BoolPtr(claudeSettingsHasRtkHook(filepath.Join(dir, "settings.json")) || util.Exists(filepath.Join(dir, "RTK.md")))
		},
		"opencode": func() *bool {
			return core.BoolPtr(util.Exists(filepath.Join(util.Home(), ".config", "opencode", "plugins", "rtk.ts")))
		},
		"codex": func() *bool {
			return core.BoolPtr(util.Exists(filepath.Join(util.CodexPathsResolved().Dir, "RTK.md")))
		},
	},
}
