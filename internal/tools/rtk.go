package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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
		_ = os.Remove(filepath.Join(dir, "RTK.md"))
	case "claude":
		cp := util.ClaudeCodePaths()
		dir := cp.Dir
		_ = os.MkdirAll(dir, 0o755)
		_ = os.Remove(filepath.Join(dir, "RTK.md"))
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
	case "copilot":
		agents.InstallCopilotRtkHook()
	case "pi":
		dir := filepath.Join(agents.PiAgentDirResolved(), "extensions")
		_ = os.MkdirAll(dir, 0o755)
		writeIfMissing(filepath.Join(dir, "rtk.ts"), "// rtk pi shim (tokless test)\n")
	case "omp":
		writeOmpRtkExtension()
	}
}

const ompRtkExtension = `import { spawn } from "node:child_process"
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent"
const TOKLESS = %s

export default function (pi: ExtensionAPI) {
  pi.on("tool_call", async (event: any) => {
    if (event?.toolName !== "bash" || typeof event?.input?.command !== "string") return
    const payload = { tool_name: "Bash", tool_input: { command: event.input.command } }
    const stdout = await new Promise<string>((resolve) => {
      let settled = false
      const finish = (value = "") => {
        if (settled) return
        settled = true
        resolve(value)
      }
      try {
        const child = spawn(TOKLESS, ["rtk-hook", "omp"], { stdio: ["pipe", "pipe", "ignore"] })
        let output = ""
        child.stdout.setEncoding("utf8")
        child.stdout.on("data", (chunk) => { output += chunk })
        child.once("error", () => finish())
        child.stdin.once("error", () => finish())
        child.once("close", (code) => finish(code === 0 ? output : ""))
        child.stdin.end(JSON.stringify(payload))
      } catch {
        finish()
      }
    })
    if (!stdout) return
    let rewritten
    try {
      rewritten = JSON.parse(stdout)?.hookSpecificOutput?.updatedInput?.command
    } catch {
      return
    }
    if (typeof rewritten !== "string") return
    return { input: { ...event.input, command: rewritten } }
  })
}
`

func ompRtkExtensionPath() string {
	return filepath.Join(agents.OmpAgentDirResolved(), "extensions", "tokless-rtk.ts")
}

func writeOmpRtkExtension() {
	_ = util.EnsureDir(filepath.Dir(ompRtkExtensionPath()))
	_ = util.WriteFile(ompRtkExtensionPath(), fmt.Sprintf(ompRtkExtension, strconv.Quote(util.ToklessAbs())))
}

func rtkWireOmp() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install OMP tool_call RTK extension")
			return true, nil
		}
		writeOmpRtkExtension()
		return agents.HasOmpRtkExtension(), nil
	}
}

const kiloRtkMarker = "tokless-kilo-rtk-v1"

const kiloRtkPlugin = `import type { Plugin } from "@kilocode/plugin"

// tokless-kilo-rtk-v1
const rtk = RTK_PATH_PLACEHOLDER
const server: Plugin = async ({ $ }) => ({
  "tool.execute.before": async (input, output) => {
    const tool = String(input?.tool ?? "").toLowerCase()
    if (tool !== "bash" && tool !== "shell") return
    if (!output || typeof output.args !== "object" || output.args === null) return
    const args = output.args as Record<string, unknown>
    const command = args.command
    if (typeof command !== "string") return
    try {
      const result = await $KILO_COMMAND_TEMPLATE.quiet().nothrow()
      const rewritten = String(result.stdout).trim()
      if (rewritten && rewritten !== command) args.command = rewritten
    } catch {}
  },
})

export default { id: "tokless-rtk", server }
`

func kiloRtkPath() string {
	return filepath.Join(util.KiloPathsResolved().PluginsDir, "rtk.ts")
}

func kiloLegacyRtkPath() string { return agents.KiloProjectFile("plugin", "tokless-rtk.ts") }

func kiloOldGlobalRtkPath() string {
	return filepath.Join(util.KiloPathsResolved().PluginsDir, "tokless-rtk.ts")
}

func removeKiloOldGlobalRtk() {
	path := kiloOldGlobalRtkPath()
	if raw, ok := util.ReadFileSafe(path); ok && strings.Contains(raw, kiloRtkMarker) {
		_ = os.Remove(path)
	}
}

func removeKiloLegacyRtk() {
	path := kiloLegacyRtkPath()
	if raw, ok := util.ReadFileSafe(path); ok && strings.Contains(raw, kiloRtkMarker) {
		_ = os.Remove(path)
	}
}

func kiloForeignBackupPath(path string) string {
	base := path + ".foreign-backup"
	for i := 0; ; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", base, i)
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func kiloRtkPluginSource(rtk string) string {
	source := strings.Replace(kiloRtkPlugin, "RTK_PATH_PLACEHOLDER", strconv.Quote(rtk), 1)
	return strings.Replace(source, "KILO_COMMAND_TEMPLATE", "`"+"${rtk} rewrite ${command}"+"`", 1)
}

func kiloRtkWire(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	path := kiloRtkPath()
	if path == "" {
		return false, nil
	}
	rtk := util.ResolveRtkBin()
	if rtk == "" {
		return false, nil
	}
	rtk, err := filepath.Abs(rtk)
	if err != nil {
		return false, nil
	}
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return false, nil
	}
	source := kiloRtkPluginSource(rtk)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tokless-kilo-rtk-*")
	if err != nil {
		return false, nil
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(source); err != nil {
		_ = tmp.Close()
		return false, nil
	}
	if err := tmp.Chmod(0o644); err != nil || tmp.Close() != nil {
		return false, nil
	}

	backup := ""
	if raw, ok := util.ReadFileSafe(path); ok && !strings.Contains(raw, kiloRtkMarker) {
		backup = kiloForeignBackupPath(path)
		if err := os.Rename(path, backup); err != nil {
			return false, nil
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if backup != "" {
			_ = os.Rename(backup, path)
		}
		return false, nil
	}
	if !kiloRtkVerify() {
		_ = os.Remove(path)
		if backup != "" {
			_ = os.Rename(backup, path)
		}
		return false, nil
	}
	removeKiloOldGlobalRtk()
	removeKiloLegacyRtk()
	return true, nil
}

func kiloRtkUnwire(core.RunOpts) (bool, error) {
	path := kiloRtkPath()
	if raw, ok := util.ReadFileSafe(path); ok && strings.Contains(raw, kiloRtkMarker) {
		_ = os.Remove(path)
	}
	removeKiloOldGlobalRtk()
	removeKiloLegacyRtk()
	return true, nil
}

func kiloRtkVerify() bool {
	path := kiloRtkPath()
	raw, ok := util.ReadFileSafe(path)
	rtk := util.ResolveRtkBin()
	if rtk == "" {
		return false
	}
	rtk, err := filepath.Abs(rtk)
	return err == nil && ok && strings.Contains(raw, kiloRtkMarker) &&
		strings.Contains(raw, `tool.execute.before`) && strings.Contains(raw, `const rtk = `+strconv.Quote(rtk)) &&
		strings.Contains(raw, "${rtk} rewrite ${command}")
}

const clineRtkMarker = "tokless-cline-rtk-v1"

func clineRtkHookPath() string {
	name := "PreToolUse"
	if util.IsWin {
		name = "PreToolUse.cjs"
	}
	return filepath.Join(util.ClinePathsResolved().HooksDir, name)
}

// clineRtkHookScript: quoted tokless abs path so the hook works with spaces in path — no fallback.
func clineRtkHookScript(exe string) string {
	if util.IsWin {
		return "#!/usr/bin/env node\n// " + clineRtkMarker + "\n" +
			"// stdio inherit streams stdin/stdout straight through — no buffering, no re-encoding.\n" +
			"const { spawnSync } = require(\"child_process\");\n" +
			"const r = spawnSync(" + strconv.Quote(exe) + ", [\"rtk-hook\", \"cline\"], { stdio: \"inherit\" });\n" +
			"process.exit(typeof r.status === \"number\" ? r.status : 0);\n"
	}
	return "#!/bin/sh\n# " + clineRtkMarker + "\nexec " + shQuote(exe) + " rtk-hook cline\n"
}

// shQuote single-quotes s for POSIX sh, safe for embedded single quotes.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func clineRtkWire(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	exe := util.ToklessAbsStrict()
	if exe == "" {
		util.L.Err("cannot resolve absolute tokless path for Cline hook; refusing to install a PATH-dependent hook")
		return false, nil
	}
	hookPath := clineRtkHookPath()
	if raw, ok := util.ReadFileSafe(hookPath); ok && !strings.Contains(raw, clineRtkMarker) {
		util.L.Err("Cline PreToolUse hook already exists; refusing to overwrite: " + hookPath)
		return false, nil
	}
	content := clineRtkHookScript(exe)
	if err := util.EnsureDir(util.ClinePathsResolved().HooksDir); err != nil {
		return false, nil
	}
	tmp, err := os.CreateTemp(util.ClinePathsResolved().HooksDir, ".tokless-cline-rtk-*")
	if err != nil {
		return false, nil
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return false, nil
	}
	if err := tmp.Chmod(0o755); err != nil || tmp.Close() != nil {
		return false, nil
	}

	if err := os.Rename(tmpPath, hookPath); err != nil {
		if util.IsWin {
			_ = os.Remove(hookPath)
			err = os.Rename(tmpPath, hookPath)
		}
		if err != nil {
			return false, nil
		}
	}
	if !clineRtkVerify() {
		_ = os.Remove(hookPath)
		return false, nil
	}
	return true, nil
}

func clineRtkUnwire(core.RunOpts) (bool, error) {
	path := clineRtkHookPath()
	raw, ok := util.ReadFileSafe(path)
	if !ok || !strings.Contains(raw, clineRtkMarker) {
		return true, nil
	}
	if err := os.Remove(path); err != nil {
		return false, nil
	}
	restoreClineRtkBackup(path)
	return true, nil
}

// restoreClineRtkBackup renames the first foreign-backup back into place.
func restoreClineRtkBackup(path string) {
	base := path + ".foreign-backup"
	for i := 0; ; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", base, i)
		}
		if _, err := os.Stat(candidate); err == nil {
			_ = os.Rename(candidate, path)
			return
		} else if !os.IsNotExist(err) || i > 0 {
			return
		}
	}
}

func clineRtkVerify() bool {
	raw, ok := util.ReadFileSafe(clineRtkHookPath())
	return ok && strings.Contains(raw, clineRtkMarker) &&
		(strings.Contains(raw, "rtk-hook cline") || strings.Contains(raw, `["rtk-hook", "cline"]`))
}

// rtkWirePi: rtk init -g --agent pi.
func rtkWirePi() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would run: rtk init -g --agent pi")
			return true, nil
		}
		if os.Getenv("TOKLESS_TEST") == "1" {
			rtkTestShim("pi")
			return agents.HasPiRtkExtension(), nil
		}
		rtkPath := util.ResolveRtkBin()
		if rtkPath == "" {
			util.L.Err("rtk binary not found on PATH or known install dirs")
			return false, nil
		}
		r := util.Run(rtkPath, []string{"init", "-g", "--agent", "pi"}, util.RunOptions{Capture: true})
		if r.Code != 0 {
			util.L.Debug("rtk init --agent pi exited " + clip(r.Stderr))
			return false, nil
		}
		return normalizePiRtkExtension() && agents.HasPiRtkExtension(), nil
	}
}

// normalizePiRtkExtension removes imports from RTK's generated Pi extension.
func normalizePiRtkExtension() bool {
	path := filepath.Join(agents.PiAgentDirResolved(), "extensions", "rtk.ts")
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	next := strings.ReplaceAll(raw, `import type { ExtensionAPI } from "@earendil-works/pi-coding-agent"`+"\n", "")
	next = strings.ReplaceAll(next, `import { isToolCallEventType } from "@earendil-works/pi-coding-agent"`+"\n", "")
	next = strings.ReplaceAll(next, "pi: ExtensionAPI", "pi: any")
	next = strings.ReplaceAll(next, `if (!isToolCallEventType("bash", event)) return`, `if (event.toolName !== "bash") return`)
	next = strings.ReplaceAll(next, `      if (cmd.startsWith("rtk ")) return`+"\n", "")
	tokless := strconv.Quote(util.ToklessAbs())
	const anchor = `const result = await pi.exec("rtk", ["rewrite", cmd], {`
	const delegate = `const result = await pi.exec(TOKLESS_BIN, ["rtk-rewrite", "--", cmd], {`
	if strings.Contains(next, "TOKLESS_BIN") {
		next = regexp.MustCompile(`const TOKLESS_BIN = .*`).ReplaceAllString(next, "const TOKLESS_BIN = "+tokless)
	} else if strings.Contains(next, anchor) {
		next = strings.Replace(next, anchor,
			"const TOKLESS_BIN = "+tokless+"\n"+delegate, 1)
		next = strings.Replace(next, "\n    timeout: REWRITE_TIMEOUT_MS,", "", 1)
	} else {
		util.L.Debug("pi rtk.ts rewrite anchor not found; leaving upstream shim untouched")
		return false
	}
	if !strings.Contains(next, "rtk-rewrite") {
		return false
	}
	if next == raw {
		return true
	}
	return util.WriteFile(path, next) == nil
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

// removeClaudeRtkHookGroup surgically strips the tokless-managed PreToolUse
// group from ~/.claude/settings.json.
func removeClaudeRtkHookGroup() {
	cp := util.ClaudeCodePaths()
	raw, ok := util.ReadFileSafe(cp.Settings)
	if !ok {
		return
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return
	}
	hooksV, ok := cfg.Get("hooks")
	if !ok {
		return
	}
	hooks, ok := hooksV.(*util.OrderedMap)
	if !ok {
		return
	}
	preV, ok := hooks.Get("PreToolUse")
	if !ok {
		return
	}
	preArr, ok := preV.([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(preArr))
	changed := false
	for _, g := range preArr {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			out = append(out, g)
			continue
		}
		hooksV, ok := gm.Get("hooks")
		if !ok {
			out = append(out, g)
			continue
		}
		arr, ok := hooksV.([]any)
		if !ok {
			out = append(out, g)
			continue
		}
		kept := make([]any, 0, len(arr))
		for _, h := range arr {
			hm, ok := h.(*util.OrderedMap)
			if !ok {
				kept = append(kept, h)
				continue
			}
			c, _ := hm.Get("command")
			s, _ := c.(string)
			if claudeRtkHookManaged(s) {
				changed = true
				continue
			}
			kept = append(kept, h)
		}
		if len(kept) == 0 && len(arr) > 0 {
			continue
		}
		gm.Set("hooks", kept)
		out = append(out, gm)
	}
	if !changed {
		return
	}
	if len(out) == 0 {
		hooks.Delete("PreToolUse")
	} else {
		hooks.Set("PreToolUse", out)
	}
	if hooks.Len() == 0 {
		cfg.Delete("hooks")
	}
	_ = util.WriteFile(cp.Settings, util.StringifyJSON(cfg))
}

// overrideClaudeRtkHook replaces rtk's own "rtk hook claude" PreToolUse hook command
// with the tokless wrapper so the output includes explicit permissionDecision: "allow".
func overrideClaudeRtkHook() {
	cp := util.ClaudeCodePaths()
	newCmd := claudeRtkHookCommand(util.ToklessAbs())
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
				if s, ok := c.(string); ok && claudeRtkHookManaged(s) && s != newCmd {
					hm2.Set("command", newCmd)
					changed = true
				}
			}
		}
	}
	seenManaged := false
	dedup := make([]any, 0, len(pt))
	for _, g := range pt {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			dedup = append(dedup, g)
			continue
		}
		hooksVal, ok := gm.Get("hooks")
		arr, ok := hooksVal.([]any)
		if !ok {
			dedup = append(dedup, g)
			continue
		}
		kept := make([]any, 0, len(arr))
		for _, h := range arr {
			hm2, ok := h.(*util.OrderedMap)
			if !ok {
				kept = append(kept, h)
				continue
			}
			c, _ := hm2.Get("command")
			s, _ := c.(string)
			if s == newCmd {
				if seenManaged {
					changed = true
					continue
				}
				seenManaged = true
			}
			kept = append(kept, h)
		}
		if len(kept) == 0 && len(arr) > 0 {
			changed = true
			continue
		}
		gm.Set("hooks", kept)
		dedup = append(dedup, gm)
	}
	if len(dedup) != len(pt) {
		hm.Set("PreToolUse", dedup)
	}
	if changed {
		_ = util.WriteFile(cp.Settings, util.StringifyJSON(cfg))
	}
	agents.AllowClaudeBashPattern("Bash(rtk *)")
	_ = os.Remove(filepath.Join(cp.Dir, "RTK.md"))
	stripRtkRefFromMd(filepath.Join(cp.Dir, "CLAUDE.md"))
}

func claudeRtkHookCommand(exe string) string {
	return util.PersistedToklessCommand(exe, "rtk-hook", "claude")
}

func claudeRtkHookManaged(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 3 && fields[0] == "rtk" && fields[1] == "hook" && fields[2] == "claude" {
		return true
	}
	if len(fields) != 3 || fields[1] != "rtk-hook" || fields[2] != "claude" {
		return false
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(fields[0], "\\", "/")))
	return base == "tokless" || base == "tokless.exe"
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

func rtkWireDroid() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install droid PreToolUse hook (~/.factory/hooks.json) routing Execute commands through rtk")
			return true, nil
		}
		agents.InstallDroidRtkHook()
		if wd, err := os.Getwd(); err == nil {
			_ = os.Remove(filepath.Join(wd, ".agents", "rules", "droid-rtk-rules.md"))
		}
		return true, nil
	}
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
		agents.RemoveCodexRtkInstruction()
		agents.InstallCodexRtkHook()
		return true, nil
	}
}

func rtkWireGrok() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install grok PreToolUse hook (~/.grok/hooks/tokless-rtk.json) routing shell commands through rtk")
			return true, nil
		}
		if err := agents.InstallGrokRtkHook(); err != nil {
			return false, err
		}
		if err := agents.InstallGrokCodegraphSessionHook(); err != nil {
			return false, err
		}
		return agents.HasGrokRtkHook() && agents.HasGrokCodegraphSessionHook(), nil
	}
}

func rtkWireCopilot() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install Copilot preToolUse hook (~/.copilot/hooks/tokless-rtk.json + .github/hooks/tokless-rtk.json)")
			return true, nil
		}
		if os.Getenv("TOKLESS_TEST") == "1" {
			rtkTestShim("copilot")
			agents.InstallCopilotIdeRtkHook()
			return agents.HasCopilotRtkHook() && agents.HasCopilotIdeRtkHook(), nil
		}
		agents.InstallCopilotRtkHook()
		agents.InstallCopilotIdeRtkHook()
		return agents.HasCopilotRtkHook() && agents.HasCopilotIdeRtkHook(), nil
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
	Description: "Command output compression and formatting utility.",
	Homepage:    "https://github.com/rtk-ai/rtk",
	InstallHint: "Prebuilt binary from GitHub releases (no Rust required).",
	Channel:     core.ChannelGitHub,
	Install:     rtkEnsureInstalled,
	WireFor: map[string]core.AgentFn{
		"claude":   rtkWire("claude"),
		"opencode": rtkWire("opencode"),
		"codex":    rtkWireCodex(),
		"cursor": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			if !agents.InstallCursorRtkHook() || !agents.ConfigureCursorRtkPermissions() {
				return false, nil
			}
			WriteOwner("cursor", "rtk")
			return agents.HasCursorRtkHook() && agents.HasCursorRtkPermissions() && HasOwner("cursor", "rtk"), nil
		},
		"antigravity": rtkWireAntigravity(),
		"copilot":     rtkWireCopilot(),
		"droid":       rtkWireDroid(),
		"pi":          rtkWirePi(),
		"omp":         rtkWireOmp(),
		"kilo":        kiloRtkWire,
		"cline":       clineRtkWire,
		"grok":        rtkWireGrok(),
	},
	UnwireFor: map[string]core.AgentFn{
		"claude": func(core.RunOpts) (bool, error) {
			if os.Getenv("TOKLESS_TEST") != "1" {
				if p := util.ResolveRtkBin(); p != "" {
					util.Run(p, []string{"init", "--uninstall", "--agent", "claude"}, util.RunOptions{})
				}
			}
			removeClaudeRtkHookGroup()
			agents.DisallowClaudeBashPattern("Bash(rtk *)")
			RemoveOwner("claude", "rtk")
			return true, nil
		},
		"opencode": func(core.RunOpts) (bool, error) {
			if os.Getenv("TOKLESS_TEST") != "1" {
				if p := util.ResolveRtkBin(); p != "" {
					util.Run(p, []string{"init", "--uninstall", "--agent", "opencode"}, util.RunOptions{})
				}
			}
			RemoveOwner("opencode", "rtk")
			return true, nil
		},
		"codex": func(core.RunOpts) (bool, error) {
			agents.RemoveCodexRtkHook()
			RemoveOwner("codex", "rtk")
			return true, nil
		},
		"cursor": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			if !agents.RemoveCursorRtkHook() || !agents.RemoveCursorRtkPermissions() {
				return false, nil
			}
			RemoveOwner("cursor", "rtk")
			return true, nil
		},
		"antigravity": func(core.RunOpts) (bool, error) {
			agents.RemoveAntigravityRtkHook()
			agents.RemoveAntigravityEntry("command(rtk)")
			agents.RemoveAntigravityEntry("command(rtk )")
			RemoveOwner("antigravity", "rtk")
			return true, nil
		},
		"copilot": func(core.RunOpts) (bool, error) {
			agents.RemoveCopilotRtkHook()
			agents.RemoveCopilotIdeRtkHook()
			RemoveOwner("copilot", "rtk")
			return true, nil
		},
		"droid": func(core.RunOpts) (bool, error) {
			agents.RemoveDroidRtkHook()
			RemoveOwner("droid", "rtk")
			return true, nil
		},
		"pi": func(core.RunOpts) (bool, error) {
			if os.Getenv("TOKLESS_TEST") != "1" {
				if p := util.ResolveRtkBin(); p != "" {
					util.Run(p, []string{"init", "--uninstall", "--agent", "pi"}, util.RunOptions{})
				}
			}
			_ = os.Remove(filepath.Join(agents.PiAgentDirResolved(), "extensions", "rtk.ts"))
			RemoveOwner("pi", "rtk")
			return true, nil
		},
		"omp": func(core.RunOpts) (bool, error) {
			if !agents.HasOmpRtkExtension() {
				return false, nil
			}
			_ = os.Remove(ompRtkExtensionPath())
			return !agents.HasOmpRtkExtension(), nil
		},
		"kilo":  kiloRtkUnwire,
		"cline": clineRtkUnwire,
		"grok": func(core.RunOpts) (bool, error) {
			agents.RemoveGrokRtkHook()
			agents.RemoveGrokCodegraphSessionHook()
			RemoveOwner("grok", "rtk")
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
		"cursor": func() *bool {
			return core.BoolPtr(agents.HasCursorRtkHook() && agents.HasCursorRtkPermissions() && HasOwner("cursor", "rtk"))
		},
		"antigravity": func() *bool {
			return core.BoolPtr(agents.HasAntigravityRtkHook())
		},
		"copilot": func() *bool {
			return core.BoolPtr(agents.HasCopilotRtkHook() && agents.HasCopilotIdeRtkHook())
		},
		"droid": func() *bool {
			return core.BoolPtr(agents.HasDroidRtkHook())
		},
		"pi": func() *bool {
			return core.BoolPtr(agents.HasPiRtkExtension())
		},
		"omp":   func() *bool { return core.BoolPtr(agents.HasOmpRtkExtension()) },
		"kilo":  func() *bool { return core.BoolPtr(kiloRtkVerify()) },
		"cline": func() *bool { return core.BoolPtr(clineRtkVerify()) },
		"grok": func() *bool {
			return core.BoolPtr(agents.HasGrokRtkHook() && agents.HasGrokCodegraphSessionHook())
		},
	},
}
