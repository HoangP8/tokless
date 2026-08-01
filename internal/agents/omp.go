package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func ompAgentDir() string {
	root := ompConfigRoot()
	profile := ompProfile()
	if profile != "" {
		return filepath.Join(root, "profiles", profile, "agent")
	}
	if dir := ompEnvPath("PI_CODING_AGENT_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(root, "agent")
}

func ompConfigRoot() string {
	if root := os.Getenv("PI_CONFIG_DIR"); root != "" {
		if root == "~" {
			return util.Home()
		}
		if strings.HasPrefix(root, "~/") {
			return filepath.Join(util.Home(), root[2:])
		}
		if filepath.IsAbs(root) {
			return filepath.Clean(root)
		}
		return filepath.Join(util.Home(), root)
	}
	return filepath.Join(util.Home(), ".omp")
}

var ompProfileName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func ompProfile() string {
	profile, set := os.LookupEnv("OMP_PROFILE")
	if !set {
		profile = os.Getenv("PI_PROFILE")
	}
	profile = strings.TrimSpace(profile)
	if profile == "" || profile == "default" || !ompProfileName.MatchString(profile) || strings.HasSuffix(profile, ".") || ompReservedProfile(profile) {
		return ""
	}
	return profile
}

func ompReservedProfile(profile string) bool {
	base := strings.ToUpper(strings.SplitN(profile, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '0' && base[3] <= '9'
	}
	return false
}

func ompEnvPath(name string) string {
	dir := strings.TrimSpace(os.Getenv(name))
	if dir == "" {
		return ""
	}
	if dir == "~" {
		return util.Home()
	}
	if strings.HasPrefix(dir, "~/") {
		return filepath.Join(util.Home(), dir[2:])
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func ompMcpFile() string          { return filepath.Join(ompAgentDir(), "mcp.json") }
func ompExtensionsDir() string    { return filepath.Join(ompAgentDir(), "extensions") }
func OmpAgentDirResolved() string { return ompAgentDir() }

func init() { core.RegisterAgent(omp) }

var omp = &core.AgentManifest{
	ID:        "omp",
	Label:     "Oh My Pi",
	Homepage:  "https://github.com/can1357/oh-my-pi",
	CLIBin:    "omp",
	ConfigDir: ompAgentDir,
	Detect: func() core.Detection {
		return detectAgent("omp", ompAgentDir(), util.ExpectedBinDirs(), nil)
	},
}

func ConfigureOmpMcp(toolID string) (changed bool, file string) {
	f := ompMcpFile()
	_ = util.EnsureDir(filepath.Dir(f))
	raw, _ := util.ReadFileSafe(f)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers := getOrCreateMap(cfg, "mcpServers")
	entry := ompMcpEntry(toolID)
	if existing, ok := servers.Get(toolID); ok {
		if ompMcpEqual(existing, entry) || !ompMcpManaged(toolID, existing) {
			return false, f
		}
	}
	servers.Set(toolID, entry)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(f, next)
		return true, f
	}
	return false, f
}

func ompMcpEqual(existing any, desired *util.OrderedMap) bool {
	em, ok := existing.(*util.OrderedMap)
	if !ok {
		return false
	}
	if enabled, ok := em.Get("enabled"); ok && enabled == false {
		return false
	}
	for _, key := range []string{"type", "command", "args"} {
		want, wanted := desired.Get(key)
		got, has := em.Get(key)
		if wanted != has {
			return false
		}
		if !wanted {
			continue
		}
		if key == "args" {
			values, valid := ompStrings(got)
			wantValues, wantValid := ompStrings(want)
			if !valid || !wantValid || !argsEq(toAnySlice(values), wantValues) {
				return false
			}
		} else if got != want {
			return false
		}
	}
	return true
}

func ompMcpManaged(toolID string, existing any) bool {
	em, ok := existing.(*util.OrderedMap)
	if !ok {
		return false
	}
	typ, _ := em.Get("type")
	command, _ := em.Get("command")
	args, _ := em.Get("args")
	cmd, _ := command.(string)
	if typ != "stdio" || !isToklessCommand(cmd) {
		return false
	}
	values, valid := ompStrings(args)
	if !valid {
		return false
	}
	if toolID == "codegraph" {
		return len(values) >= 6 && values[0] == "run-mcp" && values[1] == "--agent" && values[2] == "omp" && ompCodegraphTarget(values[3:])
	}
	return len(values) >= 3 && values[0] == "run-mcp" && values[1] == "--context-mode" && ompContextModeTarget(values[2:])
}

func ompCodegraphTarget(args []string) bool {
	if len(args) == 3 && isOmpBinary(args[0], "codegraph") {
		return args[1] == "serve" && args[2] == "--mcp"
	}
	if len(args) == 5 && isOmpCmd(args[0]) && args[1] == "/c" && isOmpBinary(args[2], "codegraph") {
		return args[3] == "serve" && args[4] == "--mcp"
	}
	if len(args) == 7 && isOmpCmd(args[0]) && args[1] == "/c" && isOmpBinary(args[2], "npx") {
		return args[3] == "--no-install" && args[4] == "@colbymchenry/codegraph" && args[5] == "serve" && args[6] == "--mcp"
	}
	return len(args) == 5 && isOmpBinary(args[0], "npx") && args[1] == "--no-install" && args[2] == "@colbymchenry/codegraph" && args[3] == "serve" && args[4] == "--mcp"
}

func ompContextModeTarget(args []string) bool {
	if len(args) == 1 && isOmpBinary(args[0], "context-mode") {
		return true
	}
	if len(args) == 3 && isOmpCmd(args[0]) && args[1] == "/c" && isOmpBinary(args[2], "context-mode") {
		return true
	}
	return len(args) == 3 && isOmpBinary(args[0], "npx") && args[1] == "--no-install" && args[2] == "context-mode" ||
		len(args) == 5 && isOmpCmd(args[0]) && args[1] == "/c" && isOmpBinary(args[2], "npx") && args[3] == "--no-install" && args[4] == "context-mode"
}

func isOmpCmd(command string) bool { return isOmpBinary(command, "cmd") }

func isOmpBinary(command, name string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/"))), ".exe")
	return base == name || base == name+".cmd" || base == name+".bat"
}

func isToklessCommand(command string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/")))
	return base == "tokless" || base == "tokless.exe"
}

func ompStrings(v any) ([]string, bool) {
	a, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		s, ok := x.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func RemoveOmpMcp(toolID string) bool {
	raw, ok := util.ReadFileSafe(ompMcpFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcpServers")
	if !ok {
		return false
	}
	servers, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	entry, ok := servers.Get(toolID)
	if !ok || !ompMcpManaged(toolID, entry) {
		return false
	}
	servers.Delete(toolID)
	_ = util.WriteFile(ompMcpFile(), util.StringifyJSON(cfg))
	return true
}

func OmpMcpHas(toolID string) bool {
	raw, ok := util.ReadFileSafe(ompMcpFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("mcpServers")
	if !ok {
		return false
	}
	servers, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	entry, ok := servers.Get(toolID)
	if !ok {
		return false
	}
	return ompMcpEqual(entry, ompMcpEntry(toolID))
}

func ompMcpEntry(toolID string) *util.OrderedMap {
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("omp", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.PickMcpSpawn("context-mode")
	}
	entry := util.NewOrderedMap()
	entry.Set("type", "stdio")
	entry.Set("command", spawn.Command)
	entry.Set("args", toAnySlice(spawn.Args))
	return entry
}

func HasOmpRtkExtension() bool {
	return util.Exists(filepath.Join(ompExtensionsDir(), "tokless-rtk.ts"))
}
