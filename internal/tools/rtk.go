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

func writeOmpRtkExtension() bool {
	if err := util.EnsureDir(filepath.Dir(ompRtkExtensionPath())); err != nil {
		return false
	}
	return util.WriteFile(ompRtkExtensionPath(), fmt.Sprintf(ompRtkExtension, strconv.Quote(util.ToklessPersistedAbs()))) == nil
}

func ompRtkExtensionValid() bool {
	raw, ok := util.ReadFileSafe(ompRtkExtensionPath())
	return ok && raw == fmt.Sprintf(ompRtkExtension, strconv.Quote(util.ToklessPersistedAbs()))
}

func rtkWireOmp() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install OMP tool_call RTK extension")
			return true, nil
		}
		if !writeOmpRtkExtension() {
			return false, nil
		}
		return ompRtkExtensionValid(), nil
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

export default server
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

func kiloRtkPluginSource(executable string) string {
	source := strings.Replace(kiloRtkPlugin, "RTK_PATH_PLACEHOLDER", strconv.Quote(executable), 1)
	return strings.Replace(source, "KILO_COMMAND_TEMPLATE", "`"+"${rtk} rtk-rewrite -- ${command}"+"`", 1)
}

func kiloRtkWire(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	path := kiloRtkPath()
	if path == "" {
		return false, nil
	}
	rtk := util.ToklessPersistedAbs()
	if rtk == "" || util.ResolveRtkBin() == "" {
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
	rtk := util.ToklessPersistedAbs()
	if rtk == "" {
		return false
	}
	return ok && strings.Contains(raw, kiloRtkMarker) &&
		strings.Contains(raw, `tool.execute.before`) && strings.Contains(raw, `const rtk = `+strconv.Quote(rtk)) &&
		strings.Contains(raw, "rtk-rewrite -- ${command}")
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
	return "#!/bin/sh\n# " + clineRtkMarker + "\nexec " + util.ShQuote(exe) + " rtk-hook cline\n"
}

func clineRtkWire(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		return true, nil
	}
	exe := util.ToklessPersistedAbs()
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
			util.L.Sub("[dry-run] would install Pi tool_call RTK extension")
			return true, nil
		}
		path := filepath.Join(agents.PiAgentDirResolved(), "extensions", "rtk.ts")
		if err := util.EnsureDir(filepath.Dir(path)); err != nil {
			return false, err
		}
		exe := util.ToklessPersistedAbs()
		if exe == "" {
			return false, nil
		}
		return util.WriteFile(path, fmt.Sprintf(piRtkExtension, strconv.Quote(exe))) == nil && agents.HasPiRtkExtension(), nil
	}
}

const openCodeRtkPlugin = `import { spawn } from "node:child_process"

const TOKLESS_BIN = %s

export const ToklessRtkPlugin = async () => ({
  "tool.execute.before": async (input: any, output: any) => {
    if (String(input?.tool ?? "").toLowerCase() !== "bash") return
    const command = output?.args?.command
    if (typeof command !== "string") return
    const rewritten = await new Promise<string>((resolve) => {
      const child = spawn(TOKLESS_BIN, ["rtk-rewrite", "--", command], { stdio: ["ignore", "pipe", "ignore"] })
      let stdout = ""
      child.stdout.setEncoding("utf8")
      child.stdout.on("data", (chunk) => { stdout += chunk })
      child.once("error", () => resolve(""))
      child.once("close", (code) => resolve(code === 0 ? stdout.trim() : ""))
    })
    if (rewritten && rewritten !== command) output.args.command = rewritten
  },
})

export default ToklessRtkPlugin
`

func openCodeRtkPluginPath() string {
	return filepath.Join(util.OpenCodePathsResolved().PluginsDir, "tokless-rtk.ts")
}

func rtkWireClaude() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install Claude Bash PreToolUse hook")
			return true, nil
		}
		exe := util.ToklessPersistedAbs()
		if exe == "" {
			return false, nil
		}
		cp := util.ClaudeCodePaths()
		if err := util.EnsureDir(cp.Dir); err != nil {
			return false, err
		}
		cfg := util.NewOrderedMap()
		if raw, ok := util.ReadFileSafe(cp.Settings); ok {
			cfg = util.TryParseJsonc(raw)
			if cfg == nil {
				return false, nil
			}
		}
		hooks := getOrCreateMapT(cfg, "hooks")
		pre, _ := hooks.Get("PreToolUse")
		arr, _ := pre.([]any)
		command := claudeRtkHookCommand(exe)
		managed := false
		for _, value := range arr {
			group, ok := value.(*util.OrderedMap)
			if !ok {
				continue
			}
			matcher, _ := group.Get("matcher")
			if matcher != "Bash" {
				continue
			}
			hooksValue, _ := group.Get("hooks")
			hookArr, _ := hooksValue.([]any)
			for _, hookValue := range hookArr {
				hook, ok := hookValue.(*util.OrderedMap)
				if !ok {
					continue
				}
				if current, _ := hook.Get("command"); claudeRtkHookManaged(fmt.Sprint(current)) {
					hook.Set("command", command)
					managed = true
				}
			}
		}
		if !managed {
			group := util.NewOrderedMap()
			group.Set("matcher", "Bash")
			hook := util.NewOrderedMap()
			hook.Set("type", "command")
			hook.Set("command", command)
			group.Set("hooks", []any{hook})
			arr = append(arr, group)
			hooks.Set("PreToolUse", arr)
		}
		if err := util.WriteFile(cp.Settings, util.StringifyJSON(cfg)); err != nil {
			return false, err
		}
		agents.AllowClaudeBashPattern("Bash(rtk *)")
		return claudeSettingsHasRtkHook(cp.Settings), nil
	}
}

func rtkWireOpenCode() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install OpenCode tool.execute.before RTK plugin")
			return true, nil
		}
		exe := util.ToklessPersistedAbs()
		if exe == "" {
			return false, nil
		}
		path := openCodeRtkPluginPath()
		if err := util.EnsureDir(filepath.Dir(path)); err != nil {
			return false, err
		}
		if err := util.WriteFile(path, fmt.Sprintf(openCodeRtkPlugin, strconv.Quote(exe))); err != nil {
			return false, err
		}
		return rtkOpenCodePluginValid(), nil
	}
}

func rtkOpenCodePluginValid() bool {
	raw, ok := util.ReadFileSafe(openCodeRtkPluginPath())
	return ok && strings.Contains(raw, "tool.execute.before") && strings.Contains(raw, "rtk-rewrite") && strings.Contains(raw, "TOKLESS_BIN")
}

const piRtkExtension = `const TOKLESS_BIN = %s

export default function (pi: any) {
  pi.on("tool_call", async (event: any) => {
    if (event?.toolName !== "bash" || typeof event?.input?.command !== "string") return
    const command = event.input.command
    try {
      const result = await pi.exec(TOKLESS_BIN, ["rtk-rewrite", "--", command])
      if (result.code !== 0 || typeof result.stdout !== "string") return
      const rewritten = result.stdout.trim()
      if (!rewritten || rewritten === command) return
      return { input: { ...event.input, command: rewritten } }
    } catch {}
  })
}
`

func writePiRtkExtension(path string) bool {
	exe := util.ToklessPersistedAbs()
	return exe != "" && util.WriteFile(path, fmt.Sprintf(piRtkExtension, strconv.Quote(exe))) == nil
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
	tokless := strconv.Quote(util.ToklessPersistedAbs())
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
	newCmd := claudeRtkHookCommand(util.ToklessPersistedAbs())
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

func rtkWireDroid() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install droid PreToolUse hook (~/.factory/hooks.json) routing Execute commands through rtk")
			return true, nil
		}
		if !agents.InstallDroidRtkHook() {
			return false, nil
		}
		return agents.HasDroidRtkHook(), nil
	}
}

func rtkWireAntigravity() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install agy PreToolUse hook (~/.gemini/config/hooks.json) routing shell commands through rtk")
			return true, nil
		}
		if !agents.InstallAntigravityRtkHook() {
			return false, nil
		}
		return agents.HasAntigravityRtkHook(), nil
	}
}

func rtkWireCodex() core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			util.L.Sub("[dry-run] would install codex PreToolUse hook (~/.codex/hooks.json) routing shell commands through rtk, pre-trusted in config.toml")
			return true, nil
		}
		if !agents.InstallCodexRtkHook() {
			return false, nil
		}
		return agents.HasCodexRtkHook(), nil
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
		err := withCopilotTransaction(func() error {
			if os.Getenv("TOKLESS_TEST") == "1" {
				agents.InstallCopilotRtkHook()
				if err := agents.InstallCopilotIdeRtkHookSafe(); err != nil {
					return err
				}
				return nil
			}
			if err := agents.InstallCopilotRtkHookSafe(); err != nil {
				return err
			}
			return agents.InstallCopilotIdeRtkHookSafe()
		})
		if err != nil {
			return false, err
		}
		return agents.HasCopilotRtkHook() && agents.HasCopilotIdeRtkHook(), nil
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
		"claude":   rtkWireClaude(),
		"opencode": rtkWireOpenCode(),
		"codex":    rtkWireCodex(),
		"cursor": func(opts core.RunOpts) (bool, error) {
			if opts.DryRun {
				return true, nil
			}
			if !agents.InstallCursorRtkHook() || !agents.ConfigureCursorRtkPermissions() {
				return false, nil
			}
			return agents.HasCursorRtkHook() && agents.HasCursorRtkPermissions(), nil
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
			removeClaudeRtkHookGroup()
			agents.DisallowClaudeBashPattern("Bash(rtk *)")
			RemoveOwner("claude", "rtk")
			return true, nil
		},
		"opencode": func(core.RunOpts) (bool, error) {
			if err := os.Remove(openCodeRtkPluginPath()); err != nil && !os.IsNotExist(err) {
				return false, err
			}
			RemoveOwner("opencode", "rtk")
			return !rtkOpenCodePluginValid(), nil
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
			err := withCopilotTransaction(func() error {
				if err := agents.RemoveCopilotRtkHookSafe(); err != nil {
					return err
				}
				if err := agents.RemoveCopilotIdeRtkHookSafe(); err != nil {
					return err
				}
				return RemoveOwnerSafe("copilot", "rtk")
			})
			return err == nil, err
		},
		"droid": func(core.RunOpts) (bool, error) {
			agents.RemoveDroidRtkHook()
			RemoveOwner("droid", "rtk")
			return true, nil
		},
		"pi": func(core.RunOpts) (bool, error) {
			path := filepath.Join(agents.PiAgentDirResolved(), "extensions", "rtk.ts")
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return false, err
			}
			RemoveOwner("pi", "rtk")
			return !agents.HasPiRtkExtension(), nil
		},
		"omp": func(core.RunOpts) (bool, error) {
			if err := os.Remove(ompRtkExtensionPath()); err != nil && !os.IsNotExist(err) {
				return false, err
			}
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
			return core.BoolPtr(rtkOpenCodePluginValid())
		},
		"codex": func() *bool {
			return core.BoolPtr(agents.HasCodexRtkHook())
		},
		"cursor": func() *bool {
			return core.BoolPtr(agents.HasCursorRtkHook() && agents.HasCursorRtkPermissions())
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
		"omp":   func() *bool { return core.BoolPtr(ompRtkExtensionValid()) },
		"kilo":  func() *bool { return core.BoolPtr(kiloRtkVerify()) },
		"cline": func() *bool { return core.BoolPtr(clineRtkVerify()) },
		"grok": func() *bool {
			return core.BoolPtr(agents.HasGrokRtkHook())
		},
	},
}
