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
	if toolID == "context-mode" {
		return len(values) >= 3 && values[0] == "run-mcp" && values[1] == "--context-mode" && ompContextModeTarget(values[2:])
	}
	return toolID == "headroom" && len(values) == 5 && values[0] == "run-mcp" && values[1] == "--tool" && values[2] == "headroom" && ompHeadroomTarget(values[3:])
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

func ompHeadroomTarget(args []string) bool {
	return len(args) == 3 && isOmpBinary(args[0], "headroom") && args[1] == "mcp" && args[2] == "serve"
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
		spawn = util.McpSpawnFor(toolID)
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

// --- Oh My Pi (omp) headroom HTTP proxy ---

func ompModelsFile() string { return filepath.Join(OmpAgentDirResolved(), "models.yml") }

var ompWriteFile = util.WriteFile

// ConfigureOmpProxy points providers.anthropic.baseUrl at the proxy via a
// careful text edit that preserves the rest of models.yml.
func ConfigureOmpProxy() (changed bool, file string) {
	baseURL := ProxyEndpointFor("omp")
	p := ompModelsFile()
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false, p
	}
	next, ok := ompYamlWriteBaseURL(raw, baseURL)
	if !ok || next == raw {
		return false, p
	}
	if err := ompWriteFile(p, next); err != nil {
		return false, p
	}
	return true, p
}

// RemoveOmpProxy deletes providers.anthropic.baseUrl only when it still equals
// the url tokless set.
func RemoveOmpProxy() bool {
	baseURL := ProxyEndpointFor("omp")
	p := ompModelsFile()
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false
	}
	next, ok := ompYamlRemoveBaseURL(raw, baseURL)
	if !ok || next == raw {
		return false
	}
	if err := ompWriteFile(p, next); err != nil {
		return false
	}
	return true
}

// OmpProxyWired reports whether providers.anthropic.baseUrl is set to baseURL.
func OmpProxyWired() bool {
	baseURL := ProxyEndpointFor("omp")
	raw, ok := util.ReadFileSafe(ompModelsFile())
	if !ok {
		return false
	}
	return ompYamlBaseURL(raw) == baseURL
}

// ompYamlKeyRe matches a YAML mapping-key line: indent, key, then anything.
var ompYamlKeyRe = regexp.MustCompile(`^(\s*)([A-Za-z0-9_.-]+):(\s*)(.*)$`)

// ompYamlKeyLine parses a mapping-key line. (indent, key, hasValue, value).
func ompYamlKeyLine(line string) (int, string, bool, string) {
	m := ompYamlKeyRe.FindStringSubmatch(line)
	if m == nil {
		return -1, "", false, ""
	}
	rest := strings.TrimSpace(m[4])
	if rest != "" {
		if i := strings.Index(rest, " #"); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		if rest != "" {
			return len(m[1]), m[2], true, rest
		}
	}
	return len(m[1]), m[2], false, ""
}

// ompYamlRef locates the providers → anthropic → baseUrl chain in models.yml.
type ompYamlRef struct {
	prov, provEnd, ant, antIndent, antEnd, base int // line indexes; -1 when absent
}

func ompYamlScan(raw string) ompYamlRef {
	ref := ompYamlRef{prov: -1, ant: -1, antIndent: 0, antEnd: -1, base: -1}
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		if ind, key, _, _ := ompYamlKeyLine(l); ind == 0 && key == "providers" {
			ref.prov = i
			break
		}
	}
	if ref.prov == -1 {
		return ref
	}
	ref.provEnd = len(lines)
	for i := ref.prov + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if ind, _, _, _ := ompYamlKeyLine(lines[i]); ind == 0 {
			ref.provEnd = i
			break
		}
	}
	directIndent := -1
	for i := ref.prov + 1; i < ref.provEnd; i++ {
		if ind, _, _, _ := ompYamlKeyLine(lines[i]); ind > 0 {
			directIndent = ind
			break
		}
	}
	for i := ref.prov + 1; i < ref.provEnd; i++ {
		if ind, key, _, _ := ompYamlKeyLine(lines[i]); key == "anthropic" && ind == directIndent {
			ref.ant, ref.antIndent = i, ind
			break
		}
	}
	if ref.ant == -1 {
		ref.antIndent = 2
		ref.antEnd = ref.provEnd
		return ref
	}
	ref.antEnd = ref.provEnd
	for i := ref.ant + 1; i < ref.provEnd; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if ind, _, _, _ := ompYamlKeyLine(lines[i]); ind >= 0 && ind <= ref.antIndent {
			ref.antEnd = i
			break
		}
	}
	for i := ref.ant + 1; i < ref.antEnd; i++ {
		if ind, key, _, _ := ompYamlKeyLine(lines[i]); key == "baseUrl" && ind == ref.antIndent+2 {
			ref.base = i
			break
		}
	}
	return ref
}

// ompYamlBaseURL returns the current providers.anthropic.baseUrl value.
func ompYamlBaseURL(raw string) string {
	ref := ompYamlScan(raw)
	if ref.base == -1 {
		return ""
	}
	_, _, _, v := ompYamlKeyLine(strings.Split(raw, "\n")[ref.base])
	return v
}

// ompYamlWriteBaseURL sets providers.anthropic.baseUrl = url, preserving the
// rest byte-for-byte. ok=false when a differing value blocks the edit.
func ompYamlWriteBaseURL(raw, url string) (string, bool) {
	lines := strings.Split(raw, "\n")
	ref := ompYamlScan(raw)
	switch {
	case ref.base >= 0:
		if _, _, _, have := ompYamlKeyLine(lines[ref.base]); have == url {
			return raw, true
		} else if have != "" {
			return raw, false
		}
		lines[ref.base] = strings.Repeat(" ", ref.antIndent+2) + "baseUrl: " + url
	case ref.ant >= 0:
		line := strings.Repeat(" ", ref.antIndent+2) + "baseUrl: " + url
		lines = ompYamlInsertLine(lines, ref.antEnd, line)
	case ref.prov >= 0:
		lines = ompYamlInsertLines(lines, ref.provEnd, "  anthropic:", "    baseUrl: "+url)
	default:
		out := raw
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "providers:" + "\n" + "  anthropic:" + "\n" + "    baseUrl: " + url + "\n"
		return out, true
	}
	return strings.Join(lines, "\n"), true
}

// ompYamlRemoveBaseURL deletes providers.anthropic.baseUrl only when it still
// equals url; ok=false when absent or differing.
func ompYamlRemoveBaseURL(raw, url string) (string, bool) {
	lines := strings.Split(raw, "\n")
	ref := ompYamlScan(raw)
	if ref.base == -1 {
		return raw, false
	}
	if _, _, _, have := ompYamlKeyLine(lines[ref.base]); have != url {
		return raw, false
	}
	lines = append(lines[:ref.base], lines[ref.base+1:]...)
	return strings.Join(lines, "\n"), true
}

func ompYamlInsertLine(lines []string, at int, line string) []string {
	return append(lines[:at], append([]string{line}, lines[at:]...)...)
}

func ompYamlInsertLines(lines []string, at int, inserted ...string) []string {
	return append(lines[:at], append(inserted, lines[at:]...)...)
}
