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
	prevStashLen := len(loadProxyRouteStash("omp"))
	if ompYamlHasDuplicateProviders(raw) {
		return false, p
	}
	nativeRaw, nativeFound, nativeChanged, nativeStash, nativeOK := ompYamlWriteNativeRoutes(raw)
	if !nativeFound && prevStashLen > 0 {
		_ = saveProxyRouteStash("omp", nativeStash)
		return false, p
	}
	if nativeFound {
		if !nativeOK {
			return false, p
		}
		if !nativeChanged {
			_ = saveProxyRouteStash("omp", nativeStash)
			return false, p
		}
		if err := saveProxyRouteStash("omp", nativeStash); err != nil {
			return false, p
		}
		if err := ompWriteFile(p, nativeRaw); err != nil {
			return false, p
		}
		return true, p
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

// ompYamlHasDuplicateProviders reports whether any provider key appears
// more than once under providers:, which makes YAML last-wins ambiguous.
func ompYamlHasDuplicateProviders(raw string) bool {
	ref := ompYamlScanProvider(raw, "\x00probe")
	if ref.prov < 0 {
		return false
	}
	lines := strings.Split(raw, "\n")
	directIndent := -1
	for i := ref.prov + 1; i < ref.provEnd; i++ {
		if ind, _, _, _ := ompYamlKeyLine(lines[i]); ind > 0 {
			directIndent = ind
			break
		}
	}
	seen := map[string]int{}
	for i := ref.prov + 1; i < ref.provEnd; i++ {
		if ind, key, _, _ := ompYamlKeyLine(lines[i]); ind == directIndent && key != "" {
			seen[key]++
			if seen[key] > 1 {
				return true
			}
		}
	}
	return false
}

// ompYamlReadableAPI returns the provider's api value when its block is
// structurally readable, even when the transport is unsupported.
func ompYamlReadableAPI(raw, id string) (string, bool) {
	ref := ompYamlScanProvider(raw, id)
	if ref.provider < 0 || ref.duplicate {
		return "", false
	}
	i, exists := ref.fields["api"]
	if !exists {
		return "", false
	}
	lines := strings.Split(raw, "\n")
	_, _, hasValue, v := ompYamlKeyLine(lines[i])
	return v, hasValue
}

func ompYamlWriteNativeRoutes(raw string) (next string, found, changed bool, stash map[string]proxyRouteStashEntry, ok bool) {
	stash = loadProxyRouteStash("omp")
	next = raw
	routed := map[string]bool{}
	present := map[string]bool{}
	unsupported := map[string]bool{}
	for _, id := range ompYamlProviderNames(raw) {
		if id == "headroom" {
			continue
		}
		present[id] = true
		api, _, base, header, _, routeOK := ompYamlProviderRoute(next, id)
		if !routeOK {
			if a, readable := ompYamlReadableAPI(next, id); readable && proxyEndpointForAPI(a) == "" {
				unsupported[id] = true
			}
			continue
		}
		found = true
		endpoint := proxyEndpointForAPI(api)
		upstream := normalizedHeadroomUpstream(base, api)
		if sameProxyBase(base, endpoint) && header != "" {
			upstream = header
		}
		var entry proxyRouteStashEntry
		var routeChanged bool
		next, entry, routeChanged, _, ok = ompYamlWriteProviderRoute(next, id, endpoint, upstream)
		if !ok {
			return raw, found, false, stash, false
		}
		if entry.Provider != "" {
			if previous, exists := stash[id]; exists {
				entry = previous
			} else if !routeChanged {
				entry = proxyRouteStashEntry{}
			}
			if entry.Provider != "" {
				stash[id] = entry
			}
		}
		routed[id] = true
		changed = changed || routeChanged
	}
	for id := range stash {
		if !routed[id] && (!present[id] || unsupported[id]) {
			delete(stash, id)
		}
	}
	return next, found, changed, stash, true
}

func providerBaseFromOmp(raw, id string) string {
	_, _, base, _, _, _ := ompYamlProviderRoute(raw, id)
	return base
}

// RemoveOmpProxy removes only tokless-owned omp wiring and restores prior role.
func RemoveOmpProxy() bool {
	changed := false
	if stash := loadProxyRouteStash("omp"); len(stash) > 0 {
		if raw, ok := util.ReadFileSafe(ompModelsFile()); ok {
			remaining := map[string]proxyRouteStashEntry{}
			next := raw
			for id, entry := range stash {
				var restored bool
				next, restored = ompYamlRestoreProviderRoute(next, entry)
				if !restored {
					remaining[id] = entry
				} else {
					changed = true
				}
			}
			if changed && ompWriteFile(ompModelsFile(), next) != nil {
				return false
			}
			_ = saveProxyRouteStash("omp", remaining)
			// Continue below to clean legacy role state from older wiring.
		}
	}
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
	if ompYamlHasDuplicateProviders(models) {
		return false
	}
	if stash := loadProxyRouteStash("omp"); len(stash) > 0 {
		matched := 0
		for id, entry := range stash {
			api, _, base, header, _, routeOK := ompYamlProviderRoute(models, id)
			if routeOK && sameProxyBase(base, proxyEndpointForAPI(api)) && header == entry.Upstream {
				matched++
			}
		}
		if matched != len(stash) {
			return false
		}
		for _, id := range ompYamlProviderNames(models) {
			if id == "headroom" {
				continue
			}
			api, _, base, header, _, routeOK := ompYamlProviderRoute(models, id)
			if !routeOK || proxyEndpointForAPI(api) == "" {
				continue
			}
			entry, ok := stash[id]
			if !ok || !sameProxyBase(base, proxyEndpointForAPI(api)) || header != entry.Upstream {
				return false
			}
		}
		return true
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
			if len(rest) >= 2 && (rest[0] == '"' || rest[0] == '\'') && rest[len(rest)-1] == rest[0] {
				rest = rest[1 : len(rest)-1]
			}
			return len(m[1]), m[2], true, rest
		}
	}
	return len(m[1]), m[2], false, ""
}

// ompYamlRef locates the providers → anthropic → baseUrl chain in models.yml.
type ompYamlRef struct {
	prov, provEnd, provider, providerIndent, providerEnd int
	fields                                               map[string]int
	duplicate                                            bool
}

func ompYamlScan(raw string) ompYamlRef {
	return ompYamlScanProvider(raw, "headroom")
}

func ompYamlScanProvider(raw, wanted string) ompYamlRef {
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
	seen := 0
	for i := ref.prov + 1; i < ref.provEnd; i++ {
		if ind, key, _, _ := ompYamlKeyLine(lines[i]); key == wanted && ind == directIndent {
			seen++
			if seen == 1 {
				ref.provider, ref.providerIndent = i, ind
			}
		}
	}
	if seen > 1 {
		ref.duplicate = true
		ref.provider = -1
		return ref
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

func ompYamlProviderNames(raw string) []string {
	lines := strings.Split(raw, "\n")
	ref := ompYamlRef{prov: -1, provEnd: len(lines)}
	for i, line := range lines {
		if ind, key, _, _ := ompYamlKeyLine(line); ind == 0 && key == "providers" {
			ref.prov = i
			break
		}
	}
	if ref.prov < 0 {
		return nil
	}
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
	if directIndent < 0 {
		return nil
	}
	var names []string
	for i := ref.prov + 1; i < ref.provEnd; i++ {
		if ind, key, _, _ := ompYamlKeyLine(lines[i]); ind == directIndent && key != "" {
			names = append(names, key)
		}
	}
	return names
}

func ompYamlProviderRoute(raw, id string) (api, baseKey, base string, headerValue string, hadHeader bool, ok bool) {
	ref := ompYamlScanProvider(raw, id)
	if ref.provider < 0 || ref.duplicate {
		return "", "", "", "", false, false
	}
	lines := strings.Split(raw, "\n")
	api, apiOK := "", false
	if i, exists := ref.fields["api"]; exists {
		_, _, _, api = ompYamlKeyLine(lines[i])
		apiOK = true
	}
	if !apiOK || proxyEndpointForAPI(api) == "" {
		return "", "", "", "", false, false
	}
	for _, key := range []string{"baseUrl", "baseURL"} {
		if i, exists := ref.fields[key]; exists {
			_, _, _, base = ompYamlKeyLine(lines[i])
			baseKey = key
			break
		}
	}
	if baseKey == "" || strings.TrimSpace(base) == "" {
		return "", "", "", "", false, false
	}
	headerIndent := ref.providerIndent + 4
	for i := ref.provider + 1; i < ref.providerEnd; i++ {
		if strings.HasPrefix(lines[i], "\t") {
			return "", "", "", "", false, false
		}
		ind, key, hasValue, value := ompYamlKeyLine(lines[i])
		if ind == ref.providerIndent+2 && key == "headers" && hasValue {
			return "", "", "", "", false, false
		}
		if ind == headerIndent && key == headroomBaseURLHeader {
			return api, baseKey, base, value, true, true
		}
	}
	return api, baseKey, base, "", false, true
}

func ompYamlWriteProviderRoute(raw, id, endpoint, upstream string) (next string, original proxyRouteStashEntry, changed, found, ok bool) {
	_, baseKey, base, headerValue, hadHeader, routeOK := ompYamlProviderRoute(raw, id)
	if !routeOK {
		return raw, proxyRouteStashEntry{}, false, false, true
	}
	found = true
	if headerValue != "" && headerValue != upstream && !sameProxyBase(base, endpoint) {
		return raw, proxyRouteStashEntry{}, false, found, false
	}
	if sameProxyBase(base, endpoint) {
		if headerValue == upstream {
			return raw, proxyRouteStashEntry{Provider: id, BaseURL: base, Upstream: upstream, BaseKey: baseKey, HadHeader: hadHeader, Header: headerValue}, false, found, true
		}
		return raw, proxyRouteStashEntry{}, false, found, false
	}
	lines := strings.Split(raw, "\n")
	ref := ompYamlScanProvider(raw, id)
	baseIdx := ref.fields[baseKey]
	baseLine := lines[baseIdx]
	spliced := false
	if keyIdx := strings.Index(baseLine, baseKey+":"); keyIdx >= 0 {
		from := keyIdx + len(baseKey) + 1
		if i := strings.Index(baseLine[from:], base); i >= 0 {
			at := from + i
			lines[baseIdx] = baseLine[:at] + endpoint + baseLine[at+len(base):]
			spliced = true
		}
	}
	if !spliced {
		lines[baseIdx] = strings.Repeat(" ", ref.providerIndent+2) + baseKey + ": " + endpoint
		baseLine = ""
	}
	headerLineText := ""
	headerLine := -1
	for i := ref.provider + 1; i < ref.providerEnd; i++ {
		ind, key, _, _ := ompYamlKeyLine(lines[i])
		if ind == ref.providerIndent+2 && key == "headers" {
			prefix := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
			for j := i + 1; j < ref.providerEnd; j++ {
				if strings.TrimSpace(lines[j]) == "" {
					continue
				}
				childInd, childKey, _, _ := ompYamlKeyLine(lines[j])
				if childInd <= ind {
					break
				}
				if childInd == ind+2 && childKey == headroomBaseURLHeader {
					headerLine = j
					break
				}
			}
			if headerLine < 0 {
				lines = ompYamlInsertLine(lines, i+1, prefix+"  "+headroomBaseURLHeader+": "+upstream)
			} else if hdr := lines[headerLine]; headerValue != "" {
				hdrFrom := 0
				if k := strings.Index(hdr, headroomBaseURLHeader+":"); k >= 0 {
					hdrFrom = k + len(headroomBaseURLHeader) + 1
				}
				if j2 := strings.Index(hdr[hdrFrom:], headerValue); j2 >= 0 {
					headerLineText = lines[headerLine]
					at := hdrFrom + j2
					lines[headerLine] = hdr[:at] + upstream + hdr[at+len(headerValue):]
				} else {
					lines[headerLine] = prefix + "  " + headroomBaseURLHeader + ": " + upstream
				}
			} else {
				lines[headerLine] = prefix + "  " + headroomBaseURLHeader + ": " + upstream
			}
			return strings.Join(lines, "\n"), proxyRouteStashEntry{Provider: id, BaseURL: base, Upstream: upstream, BaseKey: baseKey, HadHeader: hadHeader, Header: headerValue, BaseLine: baseLine, HeaderLine: headerLineText}, true, found, true
		}
	}
	insertAt := ref.providerEnd
	for insertAt > ref.provider+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	lines = ompYamlInsertLines(lines, insertAt, strings.Repeat(" ", ref.providerIndent+2)+"headers:", strings.Repeat(" ", ref.providerIndent+4)+headroomBaseURLHeader+": "+upstream)
	return strings.Join(lines, "\n"), proxyRouteStashEntry{Provider: id, BaseURL: base, Upstream: upstream, BaseKey: baseKey, HadHeader: hadHeader, Header: headerValue, BaseLine: baseLine}, true, found, true
}

func ompYamlRestoreProviderRoute(raw string, entry proxyRouteStashEntry) (string, bool) {
	api, baseKey, base, headerValue, _, ok := ompYamlProviderRoute(raw, entry.Provider)
	if !ok || !sameProxyBase(base, proxyEndpointForAPI(api)) || headerValue != entry.Upstream {
		return raw, false
	}
	lines := strings.Split(raw, "\n")
	ref := ompYamlScanProvider(raw, entry.Provider)
	if baseIdx, exists := ref.fields[baseKey]; exists && entry.BaseLine != "" {
		lines[baseIdx] = entry.BaseLine
	} else if baseIdx, exists := ref.fields[baseKey]; exists {
		lines[baseIdx] = strings.Repeat(" ", ref.providerIndent+2) + baseKey + ": " + entry.BaseURL
	}
	for i := ref.provider + 1; i < ref.providerEnd; i++ {
		ind, key, _, _ := ompYamlKeyLine(lines[i])
		if ind != ref.providerIndent+2 || key != "headers" {
			continue
		}
		for j := i + 1; j < ref.providerEnd; j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			childInd, childKey, _, _ := ompYamlKeyLine(lines[j])
			if childInd <= ind {
				break
			}
			if childInd == ind+2 && childKey == headroomBaseURLHeader {
				if entry.HadHeader {
					if entry.HeaderLine != "" {
						lines[j] = entry.HeaderLine
					} else {
						lines[j] = strings.Repeat(" ", childInd) + headroomBaseURLHeader + ": " + entry.Header
					}
				} else {
					lines = append(lines[:j], lines[j+1:]...)
				}
				break
			}
		}
		break
	}
	return ompYamlRemoveEmptyHeaders(strings.Join(lines, "\n"), entry.Provider), true
}

func ompYamlRemoveEmptyHeaders(raw, id string) string {
	ref := ompYamlScanProvider(raw, id)
	if ref.provider < 0 {
		return raw
	}
	lines := strings.Split(raw, "\n")
	for i := ref.provider + 1; i < ref.providerEnd; i++ {
		ind, key, _, _ := ompYamlKeyLine(lines[i])
		if ind != ref.providerIndent+2 || key != "headers" {
			continue
		}
		end := i + 1
		for end < ref.providerEnd {
			if strings.TrimSpace(lines[end]) == "" {
				nxt := end + 1
				for nxt < ref.providerEnd && strings.TrimSpace(lines[nxt]) == "" {
					nxt++
				}
				if nxt >= ref.providerEnd {
					break
				}
				if childInd, _, _, _ := ompYamlKeyLine(lines[nxt]); childInd >= 0 && childInd <= ind {
					break
				}
				end = nxt
				continue
			}
			childInd, _, _, _ := ompYamlKeyLine(lines[end])
			if childInd >= 0 && childInd <= ind {
				break
			}
			end++
		}
		for j := i + 1; j < end; j++ {
			if _, childKey, _, _ := ompYamlKeyLine(lines[j]); childKey != "" {
				return raw
			}
		}
		return strings.Join(append(lines[:i], lines[end:]...), "\n")
	}
	return raw
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
			if childInd == ind+2 && childKey == "baseUrl" && value == util.HeadroomProxyURL() {
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
