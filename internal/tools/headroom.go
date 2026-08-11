package tools

import (
	"context"
	"fmt"
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

func headroomNativeBuildRisk() bool {
	return headroomNativeBuildRiskFor(runtime.GOOS, runtime.GOARCH)
}

func headroomNativeBuildRiskFor(goos, goarch string) bool {
	return goos == "windows" || goos == "darwin" && goarch == "amd64"
}

func headroomFailure(stage string, result util.ExecResult) error {
	hint := ""
	if headroomNativeBuildRisk() {
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

func headroomEnsureInstalled(opts core.RunOpts) (bool, error) {
	if isTest() {
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

func headroomWire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		if !headroomCanConfigure(agent) {
			return false, fmt.Errorf("%s already has a non-Tokless headroom MCP entry; refusing to overwrite it", agent)
		}
		if !headroomConfigureMcp(agent) && !headroomMcpMatches(agent) {
			return false, nil
		}
		if agent != "cursor" {
			if agent == "kilo" {
				kiloWriteOwner("headroom")
			} else {
				WriteOwner(agent, "headroom")
			}
		}
		if agent == "copilot" {
			agents.ConfigureCopilotIdeMcp("headroom")
			agents.SyncCopilotIdeInstructions()
		}
		return headroomVerify(agent), nil
	}
}

func headroomConfigureMcp(agent string) bool {
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

func headroomUnwire(agent string) core.AgentFn {
	return func(opts core.RunOpts) (bool, error) {
		if opts.DryRun {
			return true, nil
		}
		owned := HasOwner(agent, "headroom")
		if agent == "kilo" {
			owned = kiloHasOwner("headroom")
		}
		if agent != "cursor" && (!owned || !headroomMcpMatches(agent)) {
			return false, nil
		}
		switch agent {
		case "claude":
			agents.RemoveClaudeMcp("headroom")
		case "opencode":
			agents.RemoveOpenCodeMcp("headroom")
		case "codex":
			p := util.CodexPathsResolved().Config
			raw, ok := util.ReadFileSafe(p)
			if ok {
				_ = util.WriteFile(p, util.RemoveBlock(raw, "mcp_servers.headroom"))
			}
		case "cursor":
			agents.RemoveCursorMcp("headroom")
		case "antigravity":
			agents.RemoveAntigravityMcp("headroom")
		case "copilot":
			agents.RemoveCopilotMcp("headroom")
			agents.RemoveCopilotIdeMcp("headroom")
		case "droid":
			agents.RemoveDroidMcp("headroom")
		case "grok":
			if _, err := agents.RemoveGrokMcp("headroom"); err != nil {
				return false, err
			}
		case "pi":
			agents.RemovePiMcp("headroom")
		case "omp":
			agents.RemoveOmpMcp("headroom")
		case "kilo":
			agents.RemoveKiloMcp("headroom")
		case "cline":
			agents.RemoveClineMcp("headroom")
		}
		if agent == "kilo" {
			kiloRemoveOwner("headroom")
		} else if agent != "cursor" {
			RemoveOwner(agent, "headroom")
		}
		return true, nil
	}
}

func headroomVerify(agent string) bool {
	if !isTest() && !util.HeadroomInstalled() {
		return false
	}
	switch agent {
	case "claude":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	case "opencode":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	case "codex":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	case "cursor":
		return headroomMcpMatches(agent)
	case "antigravity":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	case "copilot":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	case "droid":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	case "grok":
		return agents.GrokHeadroomMcpHas() && HasOwner(agent, "headroom")
	case "pi":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	case "omp":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	case "kilo":
		return headroomMcpMatches(agent) && kiloHasOwner("headroom")
	case "cline":
		return headroomMcpMatches(agent) && HasOwner(agent, "headroom")
	}
	return false
}

func headroomCanConfigure(agent string) bool {
	found, matches := headroomMcpState(agent)
	return !found || matches
}

func headroomMcpMatches(agent string) bool {
	found, matches := headroomMcpState(agent)
	return found && matches
}

func headroomMcpState(agent string) (found, matches bool) {
	spawn := util.McpSpawnFor("headroom")
	command := append([]string{spawn.Command}, spawn.Args...)
	switch agent {
	case "claude":
		return headroomJSONEntry(util.ClaudeCodePaths().GlobalJSON, "mcpServers", headroomStdioEntry(spawn))
	case "opencode":
		entry := util.NewOrderedMap()
		entry.Set("type", "local")
		entry.Set("command", toAny(command))
		entry.Set("enabled", true)
		return headroomJSONEntry(util.OpenCodePathsResolved().Config, "mcp", entry)
	case "codex":
		raw, _ := util.ReadFileSafe(util.CodexPathsResolved().Config)
		if !util.HasBlock(raw, "mcp_servers.headroom") {
			return false, false
		}
		entry := util.NewTomlBlock("mcp_servers.headroom")
		entry.Set("command", spawn.Command)
		entry.Set("args", spawn.Args)
		entry.Set("enabled", true)
		entry.Set("default_tools_approval_mode", "approve")
		return true, util.UpsertBlock(raw, entry, false) == raw
	case "cursor":
		return headroomJSONEntry(util.CursorGlobalMcpPath(), "mcpServers", headroomStdioEntry(spawn))
	case "antigravity":
		entry := util.NewOrderedMap()
		entry.Set("command", spawn.Command)
		entry.Set("args", toAny(spawn.Args))
		entry.Set("trust", true)
		return headroomJSONEntries([]string{util.AntigravityPathsResolved().McpConfigCLI, filepath.Join(util.Home(), ".gemini", "antigravity-cli", "mcp_config.json")}, "mcpServers", entry)
	case "copilot":
		cli := util.NewOrderedMap()
		cli.Set("type", "local")
		cli.Set("command", spawn.Command)
		cli.Set("args", toAny(spawn.Args))
		cli.Set("tools", []any{"*"})
		ide := headroomStdioEntry(spawn)
		foundCLI, matchesCLI := headroomJSONEntry(util.CopilotPathsResolved().McpConfig, "mcpServers", cli)
		foundIDE, matchesIDE := headroomJSONEntry(filepath.Join(agents.IdeProjectRoot(), ".vscode", "mcp.json"), "servers", ide)
		return foundCLI || foundIDE, foundCLI && matchesCLI && foundIDE && matchesIDE
	case "droid":
		entry := util.NewOrderedMap()
		entry.Set("command", spawn.Command)
		entry.Set("args", toAny(spawn.Args))
		entry.Set("enabledTools", toAny(agents.HeadroomDroidToolNames))
		return headroomJSONEntry(filepath.Join(util.Home(), ".factory", "mcp.json"), "mcpServers", entry)
	case "grok":
		return agents.GrokHeadroomMcpHas(), agents.GrokHeadroomMcpHas()
	case "pi":
		entry := util.NewOrderedMap()
		entry.Set("command", spawn.Command)
		entry.Set("args", toAny(spawn.Args))
		entry.Set("lifecycle", "lazy")
		entry.Set("directTools", true)
		return headroomJSONEntry(filepath.Join(agents.PiAgentDirResolved(), "mcp.json"), "mcpServers", entry)
	case "omp":
		return agents.OmpMcpHas("headroom"), agents.OmpMcpHas("headroom")
	case "kilo":
		return agents.KiloMcpConfigured("headroom"), agents.KiloMcpMatches("headroom", command)
	case "cline":
		return agents.ClineMcpConfigured("headroom"), agents.ClineMcpMatches("headroom", command)
	}
	return false, false
}

func headroomStdioEntry(spawn util.McpSpawn) *util.OrderedMap {
	entry := util.NewOrderedMap()
	entry.Set("type", "stdio")
	entry.Set("command", spawn.Command)
	entry.Set("args", toAny(spawn.Args))
	return entry
}

func headroomJSONEntries(paths []string, key string, expected *util.OrderedMap) (found, matches bool) {
	matches = true
	for _, path := range paths {
		seen, same := headroomJSONEntry(path, key, expected)
		if seen {
			found = true
			matches = matches && same
		}
	}
	return found, matches
}

func headroomJSONEntry(path, key string, expected *util.OrderedMap) (found, matches bool) {
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
	entry, found := m.Get("headroom")
	return found, found && util.StringifyJSON(entry) == util.StringifyJSON(expected)
}

var headroom = &core.ToolManifest{
	ID: "headroom", Label: "Headroom", Description: "On-demand MCP compression for large, self-contained text.",
	Homepage: "https://github.com/headroomlabs-ai/headroom", InstallHint: "Tokless-managed uv tool: headroom-ai[mcp] (Python 3.13).",
	Channel: core.ChannelBinary, NotTrackable: true, Install: headroomEnsureInstalled,
	WireFor: map[string]core.AgentFn{}, UnwireFor: map[string]core.AgentFn{}, VerifyFor: map[string]core.VerifyFn{},
}

func init() {
	for _, agent := range []string{"claude", "opencode", "codex", "cursor", "antigravity", "copilot", "droid", "grok", "pi", "omp", "kilo", "cline"} {
		headroom.WireFor[agent] = headroomWire(agent)
		headroom.UnwireFor[agent] = headroomUnwire(agent)
		headroom.VerifyFor[agent] = func(agent string) core.VerifyFn { return func() *bool { return core.BoolPtr(headroomVerify(agent)) } }(agent)
	}
}
