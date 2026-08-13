package headroom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

const headroomPackage = "headroom-ai[mcp]"
const headroomProbeTimeout = 500 * time.Millisecond

var runHeadroom = func(command string, args, env []string, ctx context.Context) util.ExecResult {
	return util.Run(command, args, util.RunOptions{Capture: true, Env: env, Ctx: ctx})
}

func headroomInstallArgs(upgrade bool) []string {
	args := []string{"tool", "install"}
	if upgrade {
		args = append(args, "--upgrade")
	}
	return append(args, "--python", "3.13", headroomPackage)
}
func headroomPythonInstallArgs() []string { return []string{"python", "install", "3.13"} }
func headroomNativeBuildRisk() bool       { return headroomNativeBuildRiskFor(runtime.GOOS, runtime.GOARCH) }
func headroomNativeBuildRiskFor(goos, goarch string) bool {
	return goos == "windows" || goos == "darwin" && goarch == "amd64"
}

func headroomFailure(stage string, result util.ExecResult) error {
	return headroomFailureFor(stage, result, headroomNativeBuildRisk())
}

func headroomFailureFor(stage string, result util.ExecResult, nativeRisk bool) error {
	hint := ""
	if nativeRisk {
		hint = " Rust/native toolchain may be required on this platform. Manual: " + strings.Join(append([]string{util.HeadroomPathsResolved().UV}, headroomInstallArgs(false)...), " ")
	}
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail != "" {
		detail = ": " + strings.Split(detail, "\n")[0]
	}
	return fmt.Errorf("headroom %s failed%s%s", stage, detail, hint)
}

func headroomUV() (string, error) {
	p := util.HeadroomPathsResolved()
	if util.Exists(p.UV) && headroomUVWorks(p.UV) {
		return p.UV, nil
	}
	if system := util.Which("uv"); system != "" && headroomUVWorks(system) {
		return system, nil
	}
	if err := util.EnsureDir(filepath.Dir(p.UV)); err != nil {
		return "", err
	}
	var command string
	var args []string
	if util.IsWin {
		command = "powershell"
		args = []string{"-ExecutionPolicy", "ByPass", "-c", "$env:UV_INSTALL_DIR=$env:TOKLESS_HEADROOM_UV_INSTALL_DIR;$env:UV_NO_MODIFY_PATH=1;irm https://astral.sh/uv/install.ps1 | iex"}
	} else {
		command = "sh"
		args = []string{"-c", "curl -LsSf https://astral.sh/uv/install.sh | sh"}
	}
	env := append(util.HeadroomUVBootstrapEnv(), "TOKLESS_HEADROOM_UV_INSTALL_DIR="+filepath.Dir(p.UV))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result := runHeadroom(command, args, env, ctx)
	if result.Code != 0 || !util.Exists(p.UV) || !headroomUVWorks(p.UV) {
		return "", headroomFailure("uv bootstrap", result)
	}
	return p.UV, nil
}

func headroomUVWorks(uv string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return runHeadroom(uv, []string{"--version"}, util.HeadroomEnv(), ctx).Code == 0
}

func headroomServeProbe() error {
	ctx, cancel := context.WithTimeout(context.Background(), headroomProbeTimeout)
	defer cancel()
	started := time.Now()
	result := runHeadroom(util.HeadroomBin(), []string{"mcp", "serve"}, nil, ctx)
	if ctx.Err() == context.DeadlineExceeded && time.Since(started) >= headroomProbeTimeout {
		return nil
	}
	return headroomFailure("executable verification", result)
}

func EnsureInstalled(opts core.RunOpts) (bool, error) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would install headroom-ai[mcp] with managed Python 3.13")
		return true, nil
	}
	opts.Reportf("uv bootstrap", 0.1)
	uv, err := headroomUV()
	if err != nil {
		return false, err
	}
	if util.HeadroomInstalled() && !opts.Upgrade {
		if err := headroomServeProbe(); err != nil {
			return false, err
		}
		opts.Reportf("already installed", 1)
		return true, nil
	}
	opts.Reportf("managed Python", 0.3)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if result := runHeadroom(uv, headroomPythonInstallArgs(), util.HeadroomEnv(), ctx); result.Code != 0 {
		return false, headroomFailure("managed Python", result)
	}
	opts.Reportf("package install", 0.5)
	result := runHeadroom(uv, headroomInstallArgs(opts.Upgrade), util.HeadroomEnv(), ctx)
	if result.Code != 0 {
		return false, headroomFailure("package install", result)
	}
	if !util.HeadroomInstalled() {
		return false, fmt.Errorf("headroom executable verification failed: %s", util.HeadroomBin())
	}
	if err := headroomServeProbe(); err != nil {
		return false, err
	}
	opts.Reportf("ready", 1)
	return true, nil
}

func ConfigureMcp(agent string) bool {
	switch agent {
	case "claude":
		agents.ConfigureClaudeMcp("headroom")
		return true
	case "opencode":
		agents.ConfigureOpenCodeMcp("headroom")
		return true
	case "codex":
		agents.ConfigureCodexMcp("headroom")
		return true
	case "cursor":
		changed, _ := agents.ConfigureCursorMcp("headroom")
		return changed || agents.CursorMcpHas("headroom")
	case "antigravity":
		agents.ConfigureAntigravityMcp("headroom")
		return true
	case "copilot":
		agents.ConfigureCopilotMcp("headroom")
		return true
	case "droid":
		agents.ConfigureDroidMcp("headroom")
		return true
	case "grok":
		_, _, err := agents.ConfigureGrokMcp("headroom")
		return err == nil
	case "pi":
		agents.ConfigurePiMcp("headroom")
		return true
	case "omp":
		changed, _ := agents.ConfigureOmpMcp("headroom")
		return changed || agents.OmpMcpHas("headroom")
	case "kilo":
		spawn := util.McpSpawnFor("headroom")
		_, _, err := agents.ConfigureKiloMcpSafe("headroom", append([]string{spawn.Command}, spawn.Args...))
		return err == nil
	case "cline":
		spawn := util.McpSpawnFor("headroom")
		_, _, err := agents.ConfigureClineMcpSafe("headroom", append([]string{spawn.Command}, spawn.Args...))
		return err == nil
	}
	return false
}

func McpMatches(agent string) bool   { found, matches := McpState(agent); return found && matches }
func CanConfigure(agent string) bool { found, matches := McpState(agent); return !found || matches }

func McpState(agent string) (found, matches bool) {
	spawn := util.McpSpawnFor("headroom")
	command := append([]string{spawn.Command}, spawn.Args...)
	entry := func() *util.OrderedMap {
		m := util.NewOrderedMap()
		m.Set("type", "stdio")
		m.Set("command", spawn.Command)
		m.Set("args", toAny(spawn.Args))
		return m
	}
	jsonEntry := func(path, key string, expected *util.OrderedMap) (bool, bool) {
		raw, ok := util.ReadFileSafe(path)
		if !ok {
			return false, false
		}
		cfg := util.TryParseJsonc(raw)
		if cfg == nil {
			return false, false
		}
		v, ok := cfg.Get(key)
		if !ok {
			return false, false
		}
		m, ok := v.(*util.OrderedMap)
		if !ok {
			return false, false
		}
		value, ok := m.Get("headroom")
		return ok, ok && util.StringifyJSON(value) == util.StringifyJSON(expected)
	}
	switch agent {
	case "claude":
		return jsonEntry(util.ClaudeCodePaths().GlobalJSON, "mcpServers", entry())
	case "opencode":
		m := util.NewOrderedMap()
		m.Set("type", "local")
		m.Set("command", toAny(command))
		m.Set("enabled", true)
		return jsonEntry(util.OpenCodePathsResolved().Config, "mcp", m)
	case "codex":
		raw, _ := util.ReadFileSafe(util.CodexPathsResolved().Config)
		if !util.HasBlock(raw, "mcp_servers.headroom") {
			return false, false
		}
		m := util.NewTomlBlock("mcp_servers.headroom")
		m.Set("command", spawn.Command)
		m.Set("args", spawn.Args)
		m.Set("enabled", true)
		m.Set("default_tools_approval_mode", "approve")
		return true, util.UpsertBlock(raw, m, false) == raw
	case "cursor":
		return jsonEntry(util.CursorGlobalMcpPath(), "mcpServers", entry())
	case "antigravity":
		m := util.NewOrderedMap()
		m.Set("command", spawn.Command)
		m.Set("args", toAny(spawn.Args))
		m.Set("trust", true)
		found, matches = false, true
		for _, path := range []string{util.AntigravityPathsResolved().McpConfigCLI, filepath.Join(util.Home(), ".gemini", "antigravity-cli", "mcp_config.json")} {
			seen, same := jsonEntry(path, "mcpServers", m)
			if seen {
				found = true
				matches = matches && same
			}
		}
		return found, matches
	case "copilot":
		cli := util.NewOrderedMap()
		cli.Set("type", "local")
		cli.Set("command", spawn.Command)
		cli.Set("args", toAny(spawn.Args))
		cli.Set("tools", []any{"*"})
		foundCLI, matchesCLI := jsonEntry(util.CopilotPathsResolved().McpConfig, "mcpServers", cli)
		foundIDE, matchesIDE := jsonEntry(filepath.Join(agents.IdeProjectRoot(), ".vscode", "mcp.json"), "servers", entry())
		return foundCLI || foundIDE, foundCLI && matchesCLI && foundIDE && matchesIDE
	case "droid":
		m := util.NewOrderedMap()
		m.Set("command", spawn.Command)
		m.Set("args", toAny(spawn.Args))
		m.Set("enabledTools", toAny(agents.HeadroomDroidToolNames))
		return jsonEntry(filepath.Join(util.Home(), ".factory", "mcp.json"), "mcpServers", m)
	case "grok":
		return agents.GrokHeadroomMcpHas(), agents.GrokHeadroomMcpHas()
	case "pi":
		m := util.NewOrderedMap()
		m.Set("command", spawn.Command)
		m.Set("args", toAny(spawn.Args))
		m.Set("lifecycle", "lazy")
		m.Set("directTools", true)
		return jsonEntry(filepath.Join(agents.PiAgentDirResolved(), "mcp.json"), "mcpServers", m)
	case "omp":
		return agents.OmpMcpHas("headroom"), agents.OmpMcpHas("headroom")
	case "kilo":
		return agents.KiloMcpConfigured("headroom"), agents.KiloMcpMatches("headroom", command)
	case "cline":
		return agents.ClineMcpConfigured("headroom"), agents.ClineMcpMatches("headroom", command)
	}
	return false, false
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
