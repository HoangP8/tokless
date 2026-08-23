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
	if toolID == "headroom" {
		return false, ompMcpFile()
	}
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
	return false
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

func ompModelsFile() string    { return filepath.Join(OmpAgentDirResolved(), "models.yml") }
func ompConfigFile() string    { return filepath.Join(OmpAgentDirResolved(), "config.yml") }
func ompRoleStateFile() string { return filepath.Join(util.ToklessDataDir(), "omp-role-prev") }

var ompWriteFile = util.WriteFile

const ompRoleModel = "deepseek-v4-flash"

func ompManagedHeadroomFields() map[string]string {
	return map[string]string{
		"baseUrl": ProxyEndpointFor("opencode"),
		"apiKey":  "TOKLESS_OPENCODE_GO_KEY",
		"api":     "openai-completions",
	}
}

const ompDiscoveryType = "openai-models-list"

func ompRoleTarget() string { return "headroom/" + ompRoleModel + ":high" }

// ConfigureOmpProxy wires omp through its additive OpenAI-compatible provider.
func ConfigureOmpProxy() (changed bool, file string) {
	p := ompModelsFile()
	raw, ok := util.ReadFileSafe(p)
	if !ok {
		return false, p
	}
	next, ok := ompYamlWriteHeadroom(raw)
	if !ok {
		return false, p
	}
	config := ompConfigFile()
	configRaw, ok := util.ReadFileSafe(config)
	if !ok {
		configRaw = ""
	}
	roleNext, rolePrev, roleChanged, ok := ompYamlWriteRole(configRaw, ompRoleTarget())
	if !ok {
		return false, p
	}
	if next != raw {
		if err := ompWriteFile(p, next); err != nil {
			return false, p
		}
		changed = true
	}
	if roleChanged {
		if err := util.WriteFile(ompRoleStateFile(), rolePrev); err != nil {
			return false, p
		}
		if err := ompWriteFile(config, roleNext); err != nil {
			return false, p
		}
		changed = true
	}
	return changed, p
}

// RemoveOmpProxy removes only tokless-owned omp wiring and restores prior role.
func RemoveOmpProxy() bool {
	changed := false
	if raw, ok := util.ReadFileSafe(ompModelsFile()); ok {
		next, headroomRemoved := ompYamlRemoveHeadroom(raw)
		next, legacyRemoved := ompYamlRemoveLegacyAnthropic(next)
		if (headroomRemoved || legacyRemoved) && ompWriteFile(ompModelsFile(), next) == nil {
			changed = true
		}
	}
	if raw, ok := util.ReadFileSafe(ompConfigFile()); ok {
		if prev, ok := util.ReadFileSafe(ompRoleStateFile()); ok {
			if next, did := ompYamlRestoreRole(raw, prev); did && ompWriteFile(ompConfigFile(), next) == nil {
				changed = true
			}
			if err := os.Remove(ompRoleStateFile()); err == nil {
				changed = true
			}
		} else if next, did := ompYamlRemoveManagedRole(raw); did && ompWriteFile(ompConfigFile(), next) == nil {
			changed = true
		}
	}
	return changed
}

// OmpProxyWired reports whether managed provider and default role are present.
func OmpProxyWired() bool {
	models, ok := util.ReadFileSafe(ompModelsFile())
	if !ok {
		return false
	}
	config, ok := util.ReadFileSafe(ompConfigFile())
	if !ok {
		return false
	}
	return ompYamlHeadroomManaged(models) && ompYamlRole(config) == ompRoleTarget()
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
	prov, provEnd, provider, providerIndent, providerEnd int
	fields                                               map[string]int
}

func ompYamlScan(raw string) ompYamlRef {
	ref := ompYamlRef{prov: -1, provider: -1, providerEnd: -1, fields: map[string]int{}}
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
		if ind, key, _, _ := ompYamlKeyLine(lines[i]); key == "headroom" && ind == directIndent {
			ref.provider, ref.providerIndent = i, ind
			break
		}
	}
	if ref.provider == -1 {
		return ref
	}
	ref.providerEnd = ref.provEnd
	for i := ref.provider + 1; i < ref.provEnd; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if ind, _, _, _ := ompYamlKeyLine(lines[i]); ind >= 0 && ind <= ref.providerIndent {
			ref.providerEnd = i
			break
		}
	}
	for i := ref.provider + 1; i < ref.providerEnd; i++ {
		if ind, key, _, _ := ompYamlKeyLine(lines[i]); ind == ref.providerIndent+2 {
			ref.fields[key] = i
		}
	}
	return ref
}

func ompYamlHeadroomManaged(raw string) bool {
	ref := ompYamlScan(raw)
	if ref.provider < 0 {
		return false
	}
	lines := strings.Split(raw, "\n")
	want := ompManagedHeadroomFields()
	for key, value := range want {
		i, ok := ref.fields[key]
		if !ok {
			return false
		}
		_, _, _, have := ompYamlKeyLine(lines[i])
		if have != value {
			return false
		}
	}
	discovery, ok := ref.fields["discovery"]
	if !ok {
		return false
	}
	if _, _, _, value := ompYamlKeyLine(lines[discovery]); value != "" {
		return false
	}
	typeOK := false
	for i := discovery + 1; i < ref.providerEnd; i++ {
		ind, key, _, value := ompYamlKeyLine(lines[i])
		if ind <= ref.providerIndent+2 {
			break
		}
		if ind == ref.providerIndent+4 && key == "type" && value == "openai-models-list" {
			typeOK = true
			break
		}
	}
	if !typeOK {
		return false
	}
	return true
}

func ompYamlWriteHeadroom(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	ref := ompYamlScan(raw)
	want := ompManagedHeadroomFields()
	if ref.provider >= 0 {
		for _, key := range []string{"apiKey", "baseUrl", "api"} {
			value := want[key]
			if i, exists := ref.fields[key]; exists {
				_, _, _, have := ompYamlKeyLine(lines[i])
				if have != "" && have != value {
					return raw, false
				}
			}
		}
		for _, key := range []string{"apiKey", "baseUrl", "api"} {
			value := want[key]
			if i, exists := ref.fields[key]; exists {
				lines[i] = strings.Repeat(" ", ref.providerIndent+2) + key + ": " + value
			} else {
				lines = ompYamlInsertLine(lines, ref.providerEnd, strings.Repeat(" ", ref.providerIndent+2)+key+": "+value)
				ref.providerEnd++
			}
		}
		if i, exists := ref.fields["discovery"]; exists {
			lines[i] = strings.Repeat(" ", ref.providerIndent+2) + "discovery:"
		} else {
			lines = ompYamlInsertLines(lines, ref.providerEnd, strings.Repeat(" ", ref.providerIndent+2)+"discovery:", strings.Repeat(" ", ref.providerIndent+4)+"type: openai-models-list")
		}
		return strings.Join(lines, "\n"), true
	}
	if ref.prov >= 0 {
		return strings.Join(ompYamlInsertLines(lines, ref.provEnd, "  headroom:", "    baseUrl: "+want["baseUrl"], "    apiKey: "+want["apiKey"], "    api: "+want["api"], "    discovery:", "      type: "+ompDiscoveryType), "\n"), true
	}
	{
		out := raw
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "providers:\n  headroom:\n    baseUrl: " + want["baseUrl"] + "\n    apiKey: " + want["apiKey"] + "\n    api: " + want["api"] + "\n    discovery:\n      type: " + ompDiscoveryType + "\n"
		return out, true
	}
}

func ompYamlRemoveHeadroom(raw string) (string, bool) {
	ref := ompYamlScan(raw)
	if ref.provider < 0 || !ompYamlHeadroomManaged(raw) {
		return raw, false
	}
	lines := strings.Split(raw, "\n")
	return strings.Join(append(lines[:ref.provider], lines[ref.providerEnd:]...), "\n"), true
}

func ompYamlRemoveLegacyAnthropic(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		ind, key, _, _ := ompYamlKeyLine(line)
		if ind != 2 || key != "anthropic" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			childInd, childKey, _, value := ompYamlKeyLine(lines[j])
			if childInd <= ind {
				break
			}
			if childInd == ind+2 && childKey == "baseUrl" && value == ProxyEndpointFor("omp") {
				return strings.Join(append(lines[:j], lines[j+1:]...), "\n"), true
			}
		}
	}
	return raw, false
}

func ompYamlRole(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		_, key, _, _ := ompYamlKeyLine(line)
		if key != "modelRoles" {
			continue
		}
		for _, child := range lines[i+1:] {
			ind, k, _, v := ompYamlKeyLine(child)
			if ind > 0 && k == "default" {
				return v
			}
			if ind == 0 {
				break
			}
		}
	}
	return ""
}

func ompYamlWriteRole(raw, target string) (string, string, bool, bool) {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		ind, key, _, _ := ompYamlKeyLine(line)
		if ind != 0 || key != "modelRoles" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			childInd, childKey, _, value := ompYamlKeyLine(lines[j])
			if childInd == 0 {
				break
			}
			if childKey == "default" {
				if value == target {
					return raw, "", false, true
				}
				lines[j] = strings.Repeat(" ", childInd) + "default: " + target
				return strings.Join(lines, "\n"), value, true, true
			}
		}
		return strings.Join(ompYamlInsertLine(lines, i+1, "  default: "+target), "\n"), "", true, true
	}
	return strings.Join(ompYamlInsertLines(lines, len(lines), "modelRoles: ", "  default: "+target), "\n"), "", true, true
}

func ompYamlRestoreRole(raw, previous string) (string, bool) {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		ind, key, _, _ := ompYamlKeyLine(line)
		if ind != 0 || key != "modelRoles" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			ci, ck, _, _ := ompYamlKeyLine(lines[j])
			if ci == 0 {
				break
			}
			if ck == "default" {
				lines[j] = strings.Repeat(" ", ci) + "default: " + previous
				return strings.Join(lines, "\n"), true
			}
		}
	}
	return raw, false
}

func ompYamlRemoveManagedRole(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		ind, key, _, _ := ompYamlKeyLine(line)
		if ind != 0 || key != "modelRoles" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			ci, ck, _, value := ompYamlKeyLine(lines[j])
			if ci == 0 {
				break
			}
			if ck == "default" && value == ompRoleTarget() {
				return strings.Join(append(lines[:j], lines[j+1:]...), "\n"), true
			}
		}
	}
	return raw, false
}

func ompYamlInsertLine(lines []string, at int, line string) []string {
	return append(lines[:at], append([]string{line}, lines[at:]...)...)
}

func ompYamlInsertLines(lines []string, at int, inserted ...string) []string {
	return append(lines[:at], append(inserted, lines[at:]...)...)
}
