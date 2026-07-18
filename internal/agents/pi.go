package agents

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// --- paths ---

func piAgentDir() string {
	if d := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); d != "" {
		if d == "~" {
			return util.Home()
		}
		if strings.HasPrefix(d, "~/") {
			return filepath.Join(util.Home(), d[2:])
		}
		if filepath.IsAbs(d) {
			return filepath.Clean(d)
		}
		if abs, err := filepath.Abs(d); err == nil {
			return abs
		}
		return d
	}
	return filepath.Join(util.Home(), ".pi", "agent")
}

func piSettingsFile() string  { return filepath.Join(piAgentDir(), "settings.json") }
func piExtensionsDir() string { return filepath.Join(piAgentDir(), "extensions") }
func piMcpFile() string       { return filepath.Join(piAgentDir(), "mcp.json") }

func PiAgentDirResolved() string { return piAgentDir() }
func PiSkillsDir() string        { return filepath.Join(piAgentDir(), "skills") }

func PiSkillHas(name string) bool {
	return util.Exists(filepath.Join(PiSkillsDir(), name, "SKILL.md"))
}

func HasPiRtkExtension() bool {
	return util.Exists(filepath.Join(piExtensionsDir(), "rtk.ts"))
}

func piKnownBinDirs() []string {
	dirs := []string{filepath.Join(util.Home(), ".pi", "agent", "bin")}
	dirs = append(dirs, util.ExpectedBinDirs()...)
	if goosForDetect == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "pi", "bin"))
		}
	}
	return dirs
}

func init() { core.RegisterAgent(pi) }

var pi = &core.AgentManifest{
	ID:        "pi",
	Label:     "Pi",
	Homepage:  "https://pi.dev",
	CLIBin:    "pi",
	ConfigDir: func() string { return piAgentDir() },
	Detect: func() core.Detection {
		return detectAgent("pi", piAgentDir(), piKnownBinDirs(), nil)
	},
}

// --- packages ---

const PiSrcMcpAdapter = "npm:pi-mcp-adapter"

func PiPackageList() []string { return []string{PiSrcMcpAdapter} }

var (
	piLegacyCaveman = []string{
		"git:github.com/JuliusBrussee/caveman", "npm:pi-caveman",
		"git:github.com/jonjonrankin/pi-caveman", "npm:@vanillagreen/pi-caveman", "npm:caveman-pi",
	}
	piLegacyPonytail = []string{
		"git:github.com/DietrichGebert/ponytail", "npm:pi-ponytail",
		"npm:@dietrichgebert/ponytail", "git:github.com/thelegendtubaguy/pi-ponytail",
	}
	piLegacyContextMode = []string{"npm:context-mode"}
	piLegacyCodegraph   = []string{
		"npm:@vndv/pi-codegraph", "npm:@estebanforge/pi-codegraph-enhanced",
		"npm:@alpsckr/pi-codegraph", "npm:@sean_pedersen/pi-codegraph",
		"npm:pi-codegraph-fix", "npm:pi-codegraph-extension", "git:github.com/vndv/pi-codegraph",
	}
)

func PiPurgeCavemanPackages() int     { return piPurge(piLegacyCaveman) }
func PiPurgePonytailPackages() int    { return piPurge(piLegacyPonytail) }
func PiPurgeContextModePackages() int { return piPurge(piLegacyContextMode) }
func PiPurgeCodegraphPackages() int   { return piPurge(piLegacyCodegraph) }

func piPurge(sources []string) int {
	n := 0
	for _, src := range sources {
		if PiSourceHas(src) && PiRemoveSource(src) {
			util.L.Sub("removed pi package " + src)
			n++
		}
	}
	return n
}

func piPackagesLoad() *util.OrderedMap {
	raw, ok := util.ReadFileSafe(piSettingsFile())
	if !ok {
		return util.NewOrderedMap()
	}
	if cfg := util.TryParseJsonc(raw); cfg != nil {
		return cfg
	}
	return util.NewOrderedMap()
}

func piPackageEntrySource(entry any) string {
	if s, ok := entry.(string); ok {
		return s
	}
	if m, ok := entry.(*util.OrderedMap); ok {
		if v, ok := m.Get("source"); ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

// piSourceIdentity strips npm/git version pins for equality checks.
func piSourceIdentity(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	if strings.HasPrefix(source, "https://github.com/") || strings.HasPrefix(source, "http://github.com/") {
		rest := source
		if i := strings.Index(rest, "://"); i >= 0 {
			rest = rest[i+3:]
		}
		rest = strings.TrimSuffix(rest, ".git")
		if at := strings.IndexByte(rest, '@'); at >= 0 {
			rest = rest[:at]
		}
		if i := strings.IndexByte(rest, '#'); i >= 0 {
			rest = rest[:i]
		}
		return "git:" + rest
	}
	if strings.HasPrefix(source, "npm:") {
		rest := source[4:]
		if strings.HasPrefix(rest, "@") {
			slash := strings.IndexByte(rest, '/')
			if slash < 0 {
				return "npm:" + rest
			}
			if at := strings.IndexByte(rest[slash+1:], '@'); at >= 0 {
				return "npm:" + rest[:slash+1+at]
			}
			return "npm:" + rest
		}
		if at := strings.IndexByte(rest, '@'); at >= 0 {
			return "npm:" + rest[:at]
		}
		return "npm:" + rest
	}
	if strings.HasPrefix(source, "git:") {
		rest := source[4:]
		if strings.HasPrefix(rest, "git@") {
			if at := strings.LastIndexByte(rest, '@'); at > 4 && strings.Count(rest, "@") > 1 {
				rest = rest[:at]
			}
			return "git:" + rest
		}
		if at := strings.IndexByte(rest, '@'); at >= 0 {
			rest = rest[:at]
		}
		return "git:" + rest
	}
	return source
}

func PiSourceHas(source string) bool {
	want := piSourceIdentity(source)
	if want == "" {
		return false
	}
	v, ok := piPackagesLoad().Get("packages")
	if !ok {
		return false
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, entry := range arr {
		if piSourceIdentity(piPackageEntrySource(entry)) == want {
			return true
		}
	}
	return false
}

func PiPackageSettingsSource(source string) string {
	want := piSourceIdentity(source)
	if want == "" {
		return ""
	}
	v, ok := piPackagesLoad().Get("packages")
	if !ok {
		return ""
	}
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	for _, entry := range arr {
		src := piPackageEntrySource(entry)
		if piSourceIdentity(src) == want {
			return src
		}
	}
	return ""
}

func getOrCreateArr(m *util.OrderedMap, key string) []any {
	if v, ok := m.Get(key); ok {
		if a, ok := v.([]any); ok {
			return a
		}
	}
	return []any{}
}

func piPackagesAdd(source string) bool {
	if !strings.Contains(source, ":") {
		source = "npm:" + source
	}
	source = piSourceIdentity(source)
	if source == "" {
		return false
	}
	cfg := piPackagesLoad()
	arr := getOrCreateArr(cfg, "packages")
	for _, entry := range arr {
		if piSourceIdentity(piPackageEntrySource(entry)) == source {
			return false
		}
	}
	cfg.Set("packages", append(arr, source))
	return util.WriteFile(piSettingsFile(), util.StringifyJSON(cfg)) == nil
}

func piPackagesRemove(source string) bool {
	if !strings.Contains(source, ":") {
		source = "npm:" + source
	}
	want := piSourceIdentity(source)
	cfg := piPackagesLoad()
	v, ok := cfg.Get("packages")
	if !ok {
		return false
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	kept := make([]any, 0, len(arr))
	removed := false
	for _, entry := range arr {
		if piSourceIdentity(piPackageEntrySource(entry)) == want {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !removed {
		return false
	}
	if len(kept) == 0 {
		cfg.Delete("packages")
	} else {
		cfg.Set("packages", kept)
	}
	return util.WriteFile(piSettingsFile(), util.StringifyJSON(cfg)) == nil
}

func piIsPinned(src string) bool {
	id := piSourceIdentity(src)
	return id != "" && src != id
}

var piPackageWarnOnce sync.Once

func warnPiPackage() {
	piPackageWarnOnce.Do(func() {
		util.L.Warn("Pi packages execute arbitrary code — review sources before install")
	})
}

func clipStr(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func piUnpinIfNeeded(source string) bool {
	exact := PiPackageSettingsSource(source)
	if exact == "" || !piIsPinned(exact) {
		return true
	}
	util.L.Sub("unpinning " + exact + " → " + source)
	if os.Getenv("TOKLESS_TEST") == "1" || util.Which("pi") == "" {
		_ = piPackagesRemove(source)
		return piPackagesAdd(source)
	}
	_ = util.Run("pi", []string{"remove", exact}, util.RunOptions{Capture: true})
	warnPiPackage()
	r := util.Run("pi", []string{"install", source}, util.RunOptions{Capture: true})
	if r.Code != 0 {
		util.L.Debug("pi reinstall unpinned " + source + " failed: " + clipStr(r.Stderr))
		return false
	}
	if got := PiPackageSettingsSource(source); piIsPinned(got) {
		_ = piPackagesRemove(source)
		_ = piPackagesAdd(source)
	}
	return PiSourceHas(source) && !piIsPinned(PiPackageSettingsSource(source))
}

// PiInstallSource installs unpinned source via `pi install`.
func PiInstallSource(source string) bool {
	if !strings.Contains(source, ":") {
		source = "npm:" + source
	}
	source = piSourceIdentity(source)
	if source == "" {
		return false
	}
	if PiSourceHas(source) {
		return piUnpinIfNeeded(source)
	}
	if os.Getenv("TOKLESS_TEST") == "1" {
		_ = util.EnsureDir(piAgentDir())
		return piPackagesAdd(source)
	}
	if util.Which("pi") == "" {
		util.L.Err("pi CLI not found on PATH — install pi first")
		return false
	}
	warnPiPackage()
	util.L.Sub("installing " + source)
	r := util.Run("pi", []string{"install", source}, util.RunOptions{Capture: true})
	if r.Code != 0 {
		util.L.Debug("pi install " + source + " failed: " + clipStr(r.Stderr))
		return false
	}
	if got := PiPackageSettingsSource(source); piIsPinned(got) {
		_ = piUnpinIfNeeded(source)
	}
	return PiSourceHas(source)
}

// PiRemoveSource removes via `pi remove`.
func PiRemoveSource(source string) bool {
	if !strings.Contains(source, ":") {
		source = "npm:" + source
	}
	exact := source
	if got := PiPackageSettingsSource(source); got != "" {
		exact = got
	}
	if os.Getenv("TOKLESS_TEST") == "1" || util.Which("pi") == "" {
		return piPackagesRemove(source)
	}
	if !PiSourceHas(source) {
		return true
	}
	r := util.Run("pi", []string{"remove", exact}, util.RunOptions{Capture: true})
	if r.Code != 0 {
		util.L.Debug("pi remove " + exact + " failed: " + clipStr(r.Stderr))
		return piPackagesRemove(source)
	}
	return !PiSourceHas(source)
}

// PiUpdatePackages refreshes tokless-managed sources only.
func PiUpdatePackages() {
	if os.Getenv("TOKLESS_TEST") == "1" {
		for _, src := range PiPackageList() {
			if PiSourceHas(src) {
				_ = piUnpinIfNeeded(src)
			}
		}
		return
	}
	if util.Which("pi") == "" {
		return
	}
	for _, src := range PiPackageList() {
		if !PiSourceHas(src) {
			continue
		}
		_ = piUnpinIfNeeded(src)
		util.L.Sub("pi update " + src)
		r := util.Run("pi", []string{"update", src}, util.RunOptions{Capture: true})
		if r.Code != 0 {
			util.L.Debug("pi update " + src + " failed: " + clipStr(r.Stderr))
		}
	}
}

// --- MCP (~/.pi/agent/mcp.json) ---

func ConfigurePiMcp(toolID string) (changed bool, file string) {
	spawn := util.PickMcpSpawn(toolID, "serve", "--mcp")
	f := piMcpFile()
	_ = util.EnsureDir(filepath.Dir(f))
	raw, _ := util.ReadFileSafe(f)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	servers := getOrCreateMap(cfg, "mcpServers")

	entry := util.NewOrderedMap()
	entry.Set("command", spawn.Command)
	if len(spawn.Args) > 0 {
		entry.Set("args", spawn.Args)
	}
	entry.Set("lifecycle", "lazy")
	entry.Set("directTools", true)

	if existing, ok := servers.Get(toolID); ok {
		if em, ok := existing.(*util.OrderedMap); ok {
			ec, _ := em.Get("command")
			ea, _ := em.Get("args")
			el, _ := em.Get("lifecycle")
			ed, _ := em.Get("directTools")
			if ec == spawn.Command && argsEq(ea, spawn.Args) && el == "lazy" && ed == true {
				return false, f
			}
		}
	}
	servers.Set(toolID, entry)
	next := util.StringifyJSON(cfg)
	if next != raw {
		_ = util.WriteFile(f, next)
		return true, f
	}
	return false, f
}

func RemovePiMcp(toolID string) bool {
	raw, ok := util.ReadFileSafe(piMcpFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	servers, ok := cfg.Get("mcpServers")
	if !ok {
		return false
	}
	sm, ok := servers.(*util.OrderedMap)
	if !ok {
		return false
	}
	if _, ok := sm.Get(toolID); !ok {
		return false
	}
	sm.Delete(toolID)
	_ = util.WriteFile(piMcpFile(), util.StringifyJSON(cfg))
	return true
}

func PiMcpHas(toolID string) bool {
	raw, ok := util.ReadFileSafe(piMcpFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	if s, ok := cfg.Get("mcpServers"); ok {
		if sm, ok := s.(*util.OrderedMap); ok {
			_, found := sm.Get(toolID)
			return found
		}
	}
	return false
}

func PiMcpHasAny() bool {
	raw, ok := util.ReadFileSafe(piMcpFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	s, ok := cfg.Get("mcpServers")
	if !ok {
		return false
	}
	sm, ok := s.(*util.OrderedMap)
	return ok && sm.Len() > 0
}
