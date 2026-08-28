package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// ConfigureCodexMcp upserts a [mcp_servers.<tool>] block in config.toml.
func ConfigureCodexMcp(toolID string) (changed bool, file string) {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, _ := util.ReadFileSafe(p.Config)
	raw = sweepStaleHookStateEntries(raw)
	var spawn util.McpSpawn
	if toolID == "codegraph" {
		spawn = util.WrapAutoIndex("codex", util.PickMcpSpawn("codegraph", "serve", "--mcp"))
	} else {
		spawn = util.McpSpawnFor(toolID)
	}
	section := "mcp_servers." + toolID
	if toolID == "context-mode" {
		section = "mcp_servers.context_mode"
	}
	block := util.NewTomlBlock(section)
	block.Set("command", spawn.Command)
	block.Set("args", spawn.Args)
	block.Set("enabled", true)
	block.Set("default_tools_approval_mode", "approve")
	next := util.UpsertBlock(raw, block, false)
	next = applyCodexApprovalPolicy(next)
	if next == raw {
		return false, p.Config
	}
	_ = util.WriteFile(p.Config, next)
	return true, p.Config
}

func codexHookStateHeader(key string) string {
	return util.TomlDottedTableHeader("hooks.state", key)
}

// sweepStaleHookStateEntries drops [hooks.state.*] blocks that are stale or use the
// legacy double-quoted header form for the current hooks.json path.
func sweepStaleHookStateEntries(raw string) string {
	current := codexHooksFile()
	re := regexp.MustCompile(`^\[hooks\.state\.(?:'([^']*)'|"([^"]*)")\]\s*$`)
	lines := strings.SplitAfter(raw, "\n")
	var out strings.Builder
	for i := 0; i < len(lines); {
		lineNoNL := strings.TrimRight(lines[i], "\r\n")
		m := re.FindStringSubmatch(lineNoNL)
		if m == nil {
			out.WriteString(lines[i])
			i++
			continue
		}
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), "[") {
			j++
		}
		key := m[1]
		if key == "" {
			key = m[2]
		}
		legacyDouble := m[2] != ""
		if strings.HasPrefix(key, current+":") && !legacyDouble {
			for ; i < j; i++ {
				out.WriteString(lines[i])
			}
			continue
		}
		i = j
	}
	return out.String()
}

// --- Codex headroom HTTP proxy ---

// Codex routing follows headroom's persistent-provider mechanism
// (headroom/providers/codex/install.py apply_provider_scope).
const codexProxyProvider = "model_providers.headroom"

const codexMarkerStart = "# --- Headroom persistent provider ---"
const codexMarkerEnd = "# --- end Headroom persistent provider ---"

func codexProxyBlock(endpoint string, byok *openCodeBYOK) *util.TomlBlock {
	block := util.NewTomlBlock(codexProxyProvider)
	block.Set("name", "Headroom persistent proxy")
	block.Set("base_url", endpoint)
	block.Set("wire_api", "responses")
	if byok == nil {
		block.Set("supports_websockets", false)
		if codexUsesChatGPTAuth() {
			block.Set("requires_openai_auth", true)
		}
		return block
	}
	block.Set("env_key", codexByokKeyVar)
	block.Set("env_http_headers", map[string]string{headroomBaseURLHeader: codexByokURLVar})
	block.Set("supports_websockets", false)
	return block
}

func codexUsesChatGPTAuth() bool {
	raw, ok := util.ReadFileSafe(filepath.Join(util.CodexPathsResolved().Dir, "auth.json"))
	if !ok {
		return false
	}
	var auth struct {
		Mode   string `json:"auth_mode"`
		Tokens struct {
			AccountID string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal([]byte(raw), &auth) != nil {
		return false
	}
	return strings.EqualFold(auth.Mode, "chatgpt") || strings.TrimSpace(auth.Tokens.AccountID) != ""
}

func codexProxyBlockWithBearer(endpoint string) *util.TomlBlock {
	block := util.NewTomlBlock(codexProxyProvider)
	block.Set("name", "Headroom persistent proxy")
	block.Set("base_url", endpoint)
	block.Set("experimental_bearer_token", "tokless")
	block.Set("supports_websockets", true)
	return block
}

func codexLegacyProxyBlock(endpoint string) *util.TomlBlock {
	block := util.NewTomlBlock(codexProxyProvider)
	block.Set("name", "Headroom persistent proxy")
	block.Set("base_url", endpoint)
	block.Set("env_key", "OPENAI_API_KEY")
	block.Set("supports_websockets", true)
	return block
}

func stripCodexManagedBlock(raw string) string {
	re := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(codexMarkerStart) + `.*?` + regexp.QuoteMeta(codexMarkerEnd))
	return re.ReplaceAllString(raw, "")
}

func stripCodexRootProviderAssignments(raw string) string {
	lines := strings.SplitAfter(raw, "\n")
	var out strings.Builder
	inRoot := true
	reHeader := regexp.MustCompile(`^[ \t]*(?:\[\[[^\]\r\n]+\]\]|\[[^\]\r\n]+\])[ \t]*(?:#.*)?$`)
	reModel := regexp.MustCompile(`^[ \t]*model_provider[ \t]*=`)
	reURL := regexp.MustCompile(`^[ \t]*openai_base_url[ \t]*=`)
	for _, line := range lines {
		noNL := strings.TrimRight(line, "\r\n")
		if inRoot && reHeader.MatchString(noNL) {
			inRoot = false
		}
		if inRoot && (reModel.MatchString(line) || reURL.MatchString(line)) {
			continue
		}
		out.WriteString(line)
	}
	return out.String()
}

func codexRootValue(raw, key string) string {
	re := regexp.MustCompile(`^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=[ \t]*"([^"]*)"`)
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}
		if m := re.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

func codexMarkedProxy(raw string) bool {
	return strings.Count(raw, codexMarkerStart) == 1 && strings.Count(raw, codexMarkerEnd) == 1 &&
		strings.Index(raw, codexMarkerStart) < strings.Index(raw, codexMarkerEnd)
}

// codexMarkedCurrentProxy matches a marker-wrapped section written by tokless
// in either flavor (OAuth-native or BYOK).
func codexMarkedCurrentProxy(raw, endpoint string) bool {
	_, _, ok := codexMatchedMarkedSection(raw, endpoint)
	return ok
}

// codexRootExtras returns top-level scalar lines inside the marked span.
func codexRootExtras(section string) []string {
	managed := map[string]bool{"model_provider": true, "openai_base_url": true}
	var extras []string
	inBlock := false
	for _, line := range strings.Split(section, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inBlock = true
			continue
		}
		if inBlock || t == "" || strings.HasPrefix(t, "#") || !strings.Contains(t, "=") {
			continue
		}
		key := strings.TrimSpace(t[:strings.Index(t, "=")])
		if !managed[key] {
			extras = append(extras, line)
		}
	}
	return extras
}

func codexWithoutLines(section string, drop []string) string {
	var kept []string
	for _, line := range strings.Split(section, "\n") {
		skip := false
		for _, d := range drop {
			if line == d {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// codexMatchedMarkedSection reports whether the marker-wrapped section is one
// tokless authored, ignoring user-owned top-level scalar lines.
func codexMatchedMarkedSection(raw, endpoint string) (string, []string, bool) {
	if !codexMarkedProxy(raw) {
		return "", nil, false
	}
	start := strings.Index(raw, codexMarkerStart)
	end := strings.Index(raw, codexMarkerEnd) + len(codexMarkerEnd)
	section := strings.TrimSpace(raw[start:end])
	extras := codexRootExtras(section)
	core := strings.TrimSpace(codexWithoutLines(section, extras))
	ok := core == strings.TrimSpace(codexProxySection(endpoint, nil)) ||
		core == strings.TrimSpace(codexProxySection(endpoint, &openCodeBYOK{})) ||
		core == strings.TrimSpace(strings.Replace(
			codexProxySection(endpoint, nil),
			"supports_websockets = false",
			"supports_websockets = true",
			1,
		))
	return section, extras, ok
}

func codexInjectRootExtras(content string, extras []string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines)+len(extras))
	inSpan := false
	inserted := false
	for _, line := range lines {
		out = append(out, line)
		if inserted {
			continue
		}
		if strings.Contains(line, codexMarkerStart) {
			inSpan = true
			continue
		}
		if strings.Contains(line, codexMarkerEnd) {
			inSpan = false
			continue
		}
		if inSpan && strings.HasPrefix(strings.TrimSpace(line), "openai_base_url ") {
			out = append(out, extras...)
			inserted = true
		}
	}
	return strings.Join(out, "\n")
}

func codexMarkedLegacyBearerProxy(raw, endpoint string) bool {
	if !codexMarkedProxy(raw) {
		return false
	}
	start := strings.Index(raw, codexMarkerStart)
	end := strings.Index(raw, codexMarkerEnd) + len(codexMarkerEnd)
	want := regexp.MustCompile(`(?m)^experimental_bearer_token = "[^"]*"$`).ReplaceAllString(
		strings.TrimSpace(codexProxySectionWithBearer(endpoint)), `experimental_bearer_token = "__TOKLESS_BEARER__"`)
	actual := regexp.MustCompile(`(?m)^experimental_bearer_token = "[^"]*"$`).ReplaceAllString(
		strings.TrimSpace(raw[start:end]), `experimental_bearer_token = "__TOKLESS_BEARER__"`)
	return actual == want
}

func codexLegacyProxy(raw, endpoint string) bool {
	block := codexProviderBlock(raw)
	if block == "" {
		return false
	}
	return strings.TrimSpace(block) == strings.TrimSpace(util.RenderBlock(codexLegacyProxyBlock(endpoint)))
}

func codexProviderBlock(raw string) string {
	header := "[" + codexProxyProvider + "]"
	start := -1
	offset := 0
	for _, line := range strings.SplitAfter(raw, "\n") {
		if strings.TrimSpace(line) == header {
			start = offset
			break
		}
		offset += len(line)
	}
	if start < 0 {
		return ""
	}
	block := raw[start:]
	if next := regexp.MustCompile(`(?m)^\[`).FindStringIndex(block[len(header):]); next != nil {
		block = block[:len(header)+next[0]]
	}
	return block
}

func codexUnmarkedCurrentProxy(raw, endpoint string, byok *openCodeBYOK) bool {
	block := codexProviderBlock(raw)
	if block == "" {
		return false
	}
	return strings.TrimSpace(block) == strings.TrimSpace(util.RenderBlock(codexProxyBlock(endpoint, byok)))
}

func codexUnmarkedLegacyBearerProxy(raw, endpoint string) bool {
	block := codexProviderBlock(raw)
	if block == "" {
		return false
	}
	want := regexp.MustCompile(`(?m)^experimental_bearer_token = "[^"]*"$`).ReplaceAllString(
		strings.TrimSpace(util.RenderBlock(codexProxyBlockWithBearer(endpoint))), `experimental_bearer_token = "__TOKLESS_BEARER__"`)
	actual := regexp.MustCompile(`(?m)^experimental_bearer_token = "[^"]*"$`).ReplaceAllString(
		strings.TrimSpace(block), `experimental_bearer_token = "__TOKLESS_BEARER__"`)
	return actual == want
}

func codexProxyOwned(raw, endpoint string) bool {
	return codexMarkedCurrentProxy(raw, endpoint) || codexMarkedLegacyBearerProxy(raw, endpoint) || codexLegacyProxy(raw, endpoint) || codexUnmarkedCurrentProxy(raw, endpoint, codexByokFlavor(raw)) || codexUnmarkedLegacyBearerProxy(raw, endpoint)
}

// codexByokFlavor reports whether an existing headroom provider block was
// written in the BYOK flavor.
func codexByokFlavor(raw string) *openCodeBYOK {
	if strings.Contains(codexProviderBlock(raw), "env_http_headers") {
		return &openCodeBYOK{}
	}
	return nil
}

func insertCodexBlockAtRoot(content, block string) string {
	block = strings.TrimSpace(block)
	lines := strings.Split(content, "\n")
	headerRe := regexp.MustCompile(`^[ \t]*(?:\[\[[^\]\r\n]+\]\]|\[[^\]\r\n]+\])[ \t]*(?:#.*)?$`)
	for i, line := range lines {
		if headerRe.MatchString(line) {
			head := strings.Join(lines[:i], "\n")
			tail := strings.Join(lines[i:], "\n")
			head = strings.TrimRight(head, "\n")
			tail = strings.TrimLeft(tail, "\n")
			prefix := ""
			if head != "" {
				prefix = head + "\n\n"
			}
			return strings.TrimRight(prefix+block+"\n\n"+tail, "\n") + "\n"
		}
	}
	return strings.TrimLeft(strings.TrimRight(content, "\n")+"\n\n"+block+"\n", "\n")
}

func codexProxyWritable(raw, endpoint string) bool {
	for _, kv := range [][2]string{{"model_provider", "headroom"}, {"openai_base_url", endpoint}} {
		if have := codexRootValue(raw, kv[0]); have != "" && have != kv[1] {
			return false
		}
	}
	if !util.HasBlock(raw, codexProxyProvider) {
		return codexRootValue(raw, "model_provider") == "" && codexRootValue(raw, "openai_base_url") == ""
	}
	return codexProxyOwned(raw, endpoint)
}

// codexTakeoverTarget reports the BYOK provider tokless may take over the root
// model_provider.
func codexTakeoverTarget(raw string) *openCodeBYOK {
	id := codexRootValue(raw, "model_provider")
	if id == "" || id == "headroom" {
		return nil
	}
	for _, b := range byokProvidersCached() {
		if b.ID == id && stripV1Suffix(codexNamedProviderBaseURL(raw, id)) == stripV1Suffix(b.BaseURL) {
			bb := b
			return &bb
		}
	}
	return nil
}

func codexNamedProviderBaseURL(raw, id string) string {
	re := regexp.MustCompile(`(?s)\[model_providers\.` + regexp.QuoteMeta(id) + `\](.*?)(?:\n\[|\z)`)
	m := re.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	mm := regexp.MustCompile(`(?m)^\s*base_url\s*=\s*"([^"]*)"`)
	if v := mm.FindStringSubmatch(m[1]); v != nil {
		return v[1]
	}
	return ""
}

func stripV1Suffix(u string) string {
	return strings.TrimSuffix(strings.TrimSuffix(u, "/"), "/v1")
}

// codexPickBYOK chooses the BYOK provider codex should ride: an already-wired
// .env keeps its provider, else the user's current provider when discovered,
// else the first discovered one.
func codexPickBYOK(raw string) *openCodeBYOK {
	if codexUsesChatGPTAuth() {
		return nil
	}
	byoks := byokProvidersCached()
	if len(byoks) == 0 {
		return nil
	}
	if url := codexDotEnvValue(codexByokURLVar); url != "" {
		for i := range byoks {
			if stripV1Suffix(byoks[i].BaseURL) == stripV1Suffix(url) {
				return &byoks[i]
			}
		}
	}
	if id := codexRootValue(raw, "model_provider"); id != "" && id != "headroom" {
		for i := range byoks {
			if byoks[i].ID == id {
				return &byoks[i]
			}
		}
	}
	return &byoks[0]
}

// codexDotEnvValue reads one KEY=value line from CODEX_HOME/.env.
func codexDotEnvValue(key string) string {
	raw, _ := util.ReadFileSafe(codexDotEnvPath())
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), key+"=") {
			return strings.TrimSpace(line[len(key)+1:])
		}
	}
	return ""
}

var (
	byokProvidersOnce sync.Once
	byokProvidersList []openCodeBYOK
)

func byokProvidersCached() []openCodeBYOK {
	byokProvidersOnce.Do(func() { byokProvidersList = DiscoverOpenCodeBYOK() })
	return byokProvidersList
}

// --- codex BYOK .env + takeover stash ---

const codexByokKeyVar = "TOKLESS_CODEX_API_KEY"
const codexByokURLVar = "TOKLESS_HEADROOM_BASE_URL"

func codexDotEnvPath() string {
	return filepath.Join(util.CodexPathsResolved().Dir, ".env")
}

func upsertCodexDotEnv(kv [][2]string, remove bool) bool {
	path := codexDotEnvPath()
	raw, _ := util.ReadFileSafe(path)
	var out []string
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if raw == "" {
			break
		}
		managed := false
		for _, k := range kv {
			if strings.HasPrefix(strings.TrimSpace(line), k[0]+"=") {
				managed = true
				break
			}
		}
		if managed && remove {
			continue
		}
		if !managed {
			out = append(out, line)
		}
	}
	if !remove {
		for _, k := range kv {
			out = append(out, k[0]+"="+k[1])
		}
	}
	next := strings.Join(out, "\n")
	if next != "" {
		next += "\n"
	}
	if next == raw {
		return true
	}
	if util.WriteFileMode(path, next, 0o600) != nil {
		return false
	}
	return os.Chmod(path, 0o600) == nil
}

func writeCodexByokDotEnv(b *openCodeBYOK) bool {
	return upsertCodexDotEnv([][2]string{
		{codexByokKeyVar, b.APIKey},
		{codexByokURLVar, stripV1Suffix(b.BaseURL)},
	}, false)
}

type codexStash struct {
	ProviderID    string `json:"provider_id"`
	OpenAIBaseURL string `json:"openai_base_url,omitempty"`
}

func codexStashPath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "codex.proxy.stash.json")
}

func loadCodexStash() (codexStash, bool) {
	raw, ok := util.ReadFileSafe(codexStashPath())
	if !ok {
		return codexStash{}, true
	}
	var s codexStash
	if json.Unmarshal([]byte(raw), &s) != nil || s.ProviderID == "" {
		return codexStash{}, false
	}
	return s, true
}

func saveCodexStash(s codexStash) bool {
	b, err := json.Marshal(s)
	if err != nil {
		return false
	}
	return util.WriteFileMode(codexStashPath(), string(b), 0o600) == nil
}

func clearCodexStash() error {
	if err := os.Remove(codexStashPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func restoreCodexConfig(path, raw string, existed bool) error {
	if existed {
		return util.WriteFile(path, raw)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ConfigureCodexProxy injects headroom's persistent-provider scope into
// config.toml.
func ConfigureCodexProxy() (changed bool, file string) {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		raw = ""
	}
	endpoint := ProxyEndpointFor("codex")
	byok := codexPickBYOK(raw)
	dotEnvPath := codexDotEnvPath()
	dotEnvRaw, dotEnvExisted := util.ReadFileSafe(dotEnvPath)
	tookOver := false
	stashRaw, stashExists := util.ReadFileSafe(codexStashPath())
	if !codexProxyWritable(raw, endpoint) {
		takeover := codexTakeoverTarget(raw)
		if takeover == nil {
			return false, p.Config
		}
		if !saveCodexStash(codexStash{
			ProviderID:    codexRootValue(raw, "model_provider"),
			OpenAIBaseURL: codexRootValue(raw, "openai_base_url"),
		}) {
			return false, p.Config
		}
		byok = takeover
		tookOver = true
	}
	var rootExtras []string
	original := raw
	if codexMarkedCurrentProxy(raw, endpoint) {
		_, rootExtras, _ = codexMatchedMarkedSection(raw, endpoint)
		raw = stripCodexManagedBlock(raw)
	} else if codexMarkedLegacyBearerProxy(raw, endpoint) {
		raw = stripCodexManagedBlock(raw)
	} else if codexLegacyProxy(raw, endpoint) || codexUnmarkedCurrentProxy(raw, endpoint, codexByokFlavor(raw)) || codexUnmarkedLegacyBearerProxy(raw, endpoint) {
		raw = stripCodexRootProviderAssignments(raw)
		raw = util.RemoveBlock(raw, codexProxyProvider)
	}
	if tookOver {
		raw = stripCodexRootProviderAssignments(raw)
	}
	if util.HasBlock(raw, codexProxyProvider) {
		return false, p.Config
	}
	next := insertCodexBlockAtRoot(raw, codexProxySection(endpoint, byok))
	if len(rootExtras) > 0 {
		next = codexInjectRootExtras(next, rootExtras)
	}
	if next == original {
		return false, p.Config
	}
	if util.WriteFile(p.Config, next) != nil {
		if tookOver {
			_ = restoreCodexConfig(codexStashPath(), stashRaw, stashExists)
		}
		return false, p.Config
	}
	if byok != nil && !writeCodexByokDotEnv(byok) {
		if err := restoreCodexConfig(p.Config, original, ok); err != nil {
			return false, p.Config
		}
		_ = restoreCodexConfig(dotEnvPath, dotEnvRaw, dotEnvExisted)
		if tookOver {
			_ = restoreCodexConfig(codexStashPath(), stashRaw, stashExists)
		}
		return false, p.Config
	}
	_ = os.Chmod(p.Config, 0o600)
	return true, p.Config
}

// RemoveCodexProxy drops the provider scope only while its values still match
// what tokless set.
func RemoveCodexProxy() bool {
	p := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return false
	}
	endpoint := ProxyEndpointFor("codex")
	if !codexProxyOwned(raw, endpoint) {
		return false
	}
	next := raw
	if codexMarkedCurrentProxy(next, endpoint) || codexMarkedLegacyBearerProxy(next, endpoint) {
		if codexMarkedCurrentProxy(next, endpoint) {
			_, rootExtras, _ := codexMatchedMarkedSection(next, endpoint)
			start := strings.Index(next, codexMarkerStart)
			end := strings.Index(next, codexMarkerEnd) + len(codexMarkerEnd)
			replacement := ""
			if len(rootExtras) > 0 {
				replacement = strings.Join(rootExtras, "\n") + "\n"
			}
			next = next[:start] + replacement + next[end:]
		} else {
			next = stripCodexManagedBlock(next)
		}
	} else {
		next = stripCodexRootProviderAssignments(next)
		next = util.RemoveBlock(next, codexProxyProvider)
	}
	stash, stashOK := loadCodexStash()
	if !stashOK {
		return false
	}
	if stash.ProviderID != "" {
		next = util.SetTomlTopKey(next, "model_provider", stash.ProviderID)
		if stash.OpenAIBaseURL != "" {
			next = util.SetTomlTopKey(next, "openai_base_url", stash.OpenAIBaseURL)
		} else {
			next = util.RemoveTomlTopKey(next, "openai_base_url")
		}
	}
	next = strings.TrimSpace(next)
	if next != "" {
		next += "\n"
	}
	if next == raw {
		return false
	}
	dotEnvPath := filepath.Join(p.Dir, ".env")
	dotEnvRaw, dotEnvExisted := util.ReadFileSafe(dotEnvPath)
	if util.WriteFile(p.Config, next) != nil {
		return false
	}
	if !upsertCodexDotEnv([][2]string{{codexByokKeyVar, ""}, {codexByokURLVar, ""}}, true) {
		_ = restoreCodexConfig(p.Config, raw, true)
		return false
	}
	if stash.ProviderID != "" {
		if err := clearCodexStash(); err != nil {
			_ = restoreCodexConfig(p.Config, raw, true)
			_ = restoreCodexConfig(dotEnvPath, dotEnvRaw, dotEnvExisted)
			return false
		}
	}
	return true
}

func codexProxySection(endpoint string, byok *openCodeBYOK) string {
	return codexMarkerStart + "\n" + `model_provider = "headroom"` + "\n" + `openai_base_url = "` + endpoint + "\"\n\n" + strings.TrimSpace(util.RenderBlock(codexProxyBlock(endpoint, byok))) + "\n" + codexMarkerEnd + "\n"
}

func codexProxySectionWithBearer(endpoint string) string {
	return codexMarkerStart + "\n" + `model_provider = "headroom"` + "\n" + `openai_base_url = "` + endpoint + "\"\n\n" + strings.TrimSpace(util.RenderBlock(codexProxyBlockWithBearer(endpoint))) + "\n" + codexMarkerEnd + "\n"
}

func codexHasManagedHeadroomBlock(raw, endpoint string) bool {
	return codexMarkedCurrentProxy(raw, endpoint) || codexUnmarkedCurrentProxy(raw, endpoint, codexByokFlavor(raw))
}

func CodexProxyWired() bool {
	raw, ok := util.ReadFileSafe(util.CodexPathsResolved().Config)
	if !ok {
		return false
	}
	endpoint := ProxyEndpointFor("codex")
	if codexRootValue(raw, "model_provider") != "headroom" {
		return false
	}
	if codexRootValue(raw, "openai_base_url") != endpoint {
		return false
	}
	return codexHasManagedHeadroomBlock(raw, endpoint)
}

// --- Codex rtk PreToolUse hook ---

const (
	codexHookMatcher     = "Bash|apply_patch|ctx_.*|codegraph_.*"
	codexHookTimeout     = 10
	codexPermHookMatcher = "Bash|apply_patch"
	codexPermHookTimeout = 5
)

func codexHooksFile() string {
	return filepath.Join(util.CodexPathsResolved().Dir, "hooks.json")
}

// RemoveCodexRtkInstruction removes the legacy RTK include from AGENTS.md.
func RemoveCodexRtkInstruction() {
	p := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(p.Instructions)
	if !ok {
		return
	}
	legacy := "@" + filepath.Join(p.Dir, "RTK.md")
	var out strings.Builder
	changed := false
	for _, line := range strings.SplitAfter(raw, "\n") {
		if strings.TrimSpace(strings.TrimRight(line, "\r\n")) == legacy {
			changed = true
			continue
		}
		out.WriteString(line)
	}
	if changed {
		_ = util.WriteFile(p.Instructions, out.String())
	}
}

// codexHookCommand is the command Codex runs for every Bash tool call.
func codexHookCommand() string {
	return toklessCommand("rtk-hook", "codex")
}

func codexPermHookCommand() string {
	return toklessCommand("codex-perm", "codex")
}

// codexHookTrustHash reproduces Codex's hook-trust hash.
func codexHookTrustHash(command string) string {
	handler := map[string]interface{}{
		"async":   false,
		"command": command,
		"timeout": codexHookTimeout,
		"type":    "command",
	}
	identity := map[string]interface{}{
		"event_name": "pre_tool_use",
		"matcher":    codexHookMatcher,
		"hooks":      []interface{}{handler},
	}
	b, _ := json.Marshal(identity)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func codexPermHookTrustHash(command string) string {
	handler := map[string]interface{}{
		"async":   false,
		"command": command,
		"timeout": codexPermHookTimeout,
		"type":    "command",
	}
	identity := map[string]interface{}{
		"event_name": "permission_request",
		"matcher":    codexPermHookMatcher,
		"hooks":      []interface{}{handler},
	}
	b, _ := json.Marshal(identity)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func codexGroupHasRtk(group *util.OrderedMap) bool {
	return codexGroupHasManaged(group, "rtk-hook", "codex")
}

func codexGroupHasPerm(group *util.OrderedMap) bool {
	return codexGroupHasManaged(group, "codex-perm", "codex")
}

func codexGroupHasManaged(group *util.OrderedMap, args ...string) bool {
	hooksObj, _ := group.Get("hooks")
	arr, ok := hooksObj.([]interface{})
	if !ok {
		return false
	}
	for _, h := range arr {
		hm, ok := h.(*util.OrderedMap)
		if !ok {
			continue
		}
		cmd, _ := hm.Get("command")
		s, _ := cmd.(string)
		if toklessManagedCommand(s, args...) {
			return true
		}
	}
	return false
}

type codexHookPos struct{ group, hook int }

func codexTransformManagedGroups(groups []interface{}, matcher string, args []string, desired *util.OrderedMap) ([]interface{}, codexHookPos, map[codexHookPos]codexHookPos, []codexHookPos) {
	out := make([]interface{}, 0, len(groups)+1)
	desiredPos := codexHookPos{-1, -1}
	moved := map[codexHookPos]codexHookPos{}
	var removed []codexHookPos
	var desiredHook any
	if desired != nil {
		if hooksObj, ok := desired.Get("hooks"); ok {
			if hooks, ok := hooksObj.([]interface{}); ok && len(hooks) > 0 {
				desiredHook = hooks[0]
			}
		}
	}
	for oldGroup, g := range groups {
		gm, ok := g.(*util.OrderedMap)
		if !ok {
			out = append(out, g)
			continue
		}
		groupMatcher, _ := gm.Get("matcher")
		hooksObj, _ := gm.Get("hooks")
		hooks, ok := hooksObj.([]interface{})
		if !ok {
			out = append(out, g)
			continue
		}
		kept := make([]interface{}, 0, len(hooks))
		for oldHook, h := range hooks {
			hm, isMap := h.(*util.OrderedMap)
			managed := false
			if groupMatcher == matcher && isMap {
				cmd, _ := hm.Get("command")
				command, _ := cmd.(string)
				managed = toklessManagedCommand(command, args...)
			}
			if managed {
				if desiredHook != nil && desiredPos.group == -1 {
					desiredPos = codexHookPos{len(out), len(kept)}
					kept = append(kept, desiredHook)
				} else {
					removed = append(removed, codexHookPos{oldGroup, oldHook})
				}
				continue
			}
			newPos := codexHookPos{len(out), len(kept)}
			moved[codexHookPos{oldGroup, oldHook}] = newPos
			kept = append(kept, h)
		}
		if len(kept) > 0 {
			gm.Set("hooks", kept)
			out = append(out, gm)
		} else if len(hooks) == 0 {
			out = append(out, gm)
		}
	}
	if desiredHook != nil && desiredPos.group == -1 {
		desiredPos = codexHookPos{len(out), 0}
		out = append(out, desired)
	}
	return out, desiredPos, moved, removed
}

func codexRewriteHookState(raw, hooksFile, event string, moved map[codexHookPos]codexHookPos, removed []codexHookPos) string {
	for _, pos := range removed {
		key := hooksFile + ":" + event + ":" + strconv.Itoa(pos.group) + ":" + strconv.Itoa(pos.hook)
		raw = util.RemoveBlock(raw, codexHookStateHeader(key))
	}
	type move struct{ old, new, placeholder string }
	var moves []move
	for oldPos, newPos := range moved {
		if oldPos == newPos {
			continue
		}
		oldKey := hooksFile + ":" + event + ":" + strconv.Itoa(oldPos.group) + ":" + strconv.Itoa(oldPos.hook)
		newKey := hooksFile + ":" + event + ":" + strconv.Itoa(newPos.group) + ":" + strconv.Itoa(newPos.hook)
		placeholder := "hooks.state.'__tokless_move_" + strconv.Itoa(len(moves)) + "__'"
		oldHeader := "[" + codexHookStateHeader(oldKey) + "]"
		if strings.Contains(raw, oldHeader) {
			raw = strings.Replace(raw, oldHeader, "["+placeholder+"]", 1)
			moves = append(moves, move{placeholder: placeholder, new: codexHookStateHeader(newKey)})
		}
	}
	for _, m := range moves {
		raw = strings.Replace(raw, "["+m.placeholder+"]", "["+m.new+"]", 1)
	}
	return raw
}

func codexRtkGroup(command string) *util.OrderedMap {
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", command)
	hook.Set("timeout", codexHookTimeout)

	group := util.NewOrderedMap()
	group.Set("matcher", codexHookMatcher)
	group.Set("hooks", []interface{}{hook})
	return group
}

func codexPermGroup(command string) *util.OrderedMap {
	hook := util.NewOrderedMap()
	hook.Set("type", "command")
	hook.Set("command", command)
	hook.Set("timeout", codexPermHookTimeout)

	group := util.NewOrderedMap()
	group.Set("matcher", codexPermHookMatcher)
	group.Set("hooks", []interface{}{hook})
	return group
}

// InstallCodexRtkHook merges the rtk PreToolUse hook into ~/.codex/hooks.json.
func InstallCodexRtkHook() {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	command := codexHookCommand()

	hooksFile := codexHooksFile()
	raw, _ := util.ReadFileSafe(hooksFile)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	var preArr []interface{}
	if v, ok := hooks.Get("PreToolUse"); ok {
		preArr, _ = v.([]interface{})
	}
	preArr, pos, moved, removed := codexTransformManagedGroups(preArr, codexHookMatcher, []string{"rtk-hook", "codex"}, codexRtkGroup(command))
	hooks.Set("PreToolUse", preArr)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}

	craw, _ := util.ReadFileSafe(p.Config)
	craw = sweepStaleHookStateEntries(craw)
	craw = codexRewriteHookState(craw, hooksFile, "pre_tool_use", moved, removed)
	key := hooksFile + ":pre_tool_use:" + strconv.Itoa(pos.group) + ":" + strconv.Itoa(pos.hook)
	block := util.NewTomlBlock(codexHookStateHeader(key))
	block.Set("trusted_hash", codexHookTrustHash(command))
	cnext := util.UpsertBlock(craw, block, false)
	cnext = applyCodexApprovalPolicy(cnext)
	features := util.NewTomlBlock("features")
	features.Set("hooks", true)
	cnext = util.UpsertBlock(cnext, features, false)
	if cnext != craw {
		_ = util.WriteFile(p.Config, cnext)
	}

	_ = os.Remove(filepath.Join(p.Dir, "RTK.md"))

	InstallCodexPermissionHook()
	InstallCodexRulesAllowlist()
}

// RemoveCodexRtkHook removes the rtk group from hooks.json and its trust entry.
func RemoveCodexRtkHook() {
	p := util.CodexPathsResolved()
	hooksFile := codexHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			if hooks, ok := mapChild(cfg, "hooks"); ok {
				if v, ok := hooks.Get("PreToolUse"); ok {
					if preArr, ok := v.([]interface{}); ok {
						kept, _, moved, removed := codexTransformManagedGroups(preArr, codexHookMatcher, []string{"rtk-hook", "codex"}, nil)
						if len(removed) > 0 {
							hooks.Set("PreToolUse", kept)
							_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
							craw, _ := util.ReadFileSafe(p.Config)
							cnext := codexRewriteHookState(craw, hooksFile, "pre_tool_use", moved, removed)
							if cnext != craw {
								_ = util.WriteFile(p.Config, cnext)
							}
						}
					}
				}
			}
		}
	}
	RemoveCodexPermissionHook()
	RemoveCodexRulesAllowlist()
	codexCleanupOrphanedConfig()
}

// HasCodexRtkHook reports whether the rtk hook is present in hooks.json.
func HasCodexRtkHook() bool {
	raw, ok := util.ReadFileSafe(codexHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		return false
	}
	v, ok := hooks.Get("PreToolUse")
	if !ok {
		return false
	}
	preArr, ok := v.([]interface{})
	if !ok {
		return false
	}
	for _, g := range preArr {
		if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasRtk(gm) {
			return true
		}
	}
	return false
}

// InstallCodexPermissionHook merges a PermissionRequest group into hooks.json + pre-seeds trust.
func InstallCodexPermissionHook() {
	p := util.CodexPathsResolved()
	_ = util.EnsureDir(p.Dir)
	command := codexPermHookCommand()
	hooksFile := codexHooksFile()
	raw, _ := util.ReadFileSafe(hooksFile)
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		cfg = util.NewOrderedMap()
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		hooks = util.NewOrderedMap()
		cfg.Set("hooks", hooks)
	}
	var permArr []interface{}
	if v, ok := hooks.Get("PermissionRequest"); ok {
		permArr, _ = v.([]interface{})
	}
	permArr, pos, moved, removed := codexTransformManagedGroups(permArr, codexPermHookMatcher, []string{"codex-perm", "codex"}, codexPermGroup(command))
	hooks.Set("PermissionRequest", permArr)
	if next := util.StringifyJSON(cfg); next != raw {
		_ = util.WriteFile(hooksFile, next)
	}
	craw, _ := util.ReadFileSafe(p.Config)
	craw = sweepStaleHookStateEntries(craw)
	craw = codexRewriteHookState(craw, hooksFile, "permission_request", moved, removed)
	key := hooksFile + ":permission_request:" + strconv.Itoa(pos.group) + ":" + strconv.Itoa(pos.hook)
	block := util.NewTomlBlock(codexHookStateHeader(key))
	block.Set("trusted_hash", codexPermHookTrustHash(command))
	cnext := util.UpsertBlock(craw, block, false)
	if cnext != craw {
		_ = util.WriteFile(p.Config, cnext)
	}
}

// RemoveCodexPermissionHook removes the PermissionRequest group and its trust entry.
func RemoveCodexPermissionHook() {
	p := util.CodexPathsResolved()
	hooksFile := codexHooksFile()
	raw, ok := util.ReadFileSafe(hooksFile)
	if ok {
		if cfg := util.TryParseJsonc(raw); cfg != nil {
			if hooks, ok := mapChild(cfg, "hooks"); ok {
				if v, ok := hooks.Get("PermissionRequest"); ok {
					if permArr, ok := v.([]interface{}); ok {
						kept, _, moved, removed := codexTransformManagedGroups(permArr, codexPermHookMatcher, []string{"codex-perm", "codex"}, nil)
						if len(removed) > 0 {
							hooks.Set("PermissionRequest", kept)
							_ = util.WriteFile(hooksFile, util.StringifyJSON(cfg))
							craw, _ := util.ReadFileSafe(p.Config)
							cnext := codexRewriteHookState(craw, hooksFile, "permission_request", moved, removed)
							if cnext != craw {
								_ = util.WriteFile(p.Config, cnext)
							}
						}
					}
				}
			}
		}
	}
}

// HasCodexPermissionHook reports whether the PermissionRequest hook is present.
func HasCodexPermissionHook() bool {
	raw, ok := util.ReadFileSafe(codexHooksFile())
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return false
	}
	hooks, ok := mapChild(cfg, "hooks")
	if !ok {
		return false
	}
	v, ok := hooks.Get("PermissionRequest")
	if !ok {
		return false
	}
	permArr, ok := v.([]interface{})
	if !ok {
		return false
	}
	for _, g := range permArr {
		if gm, ok := g.(*util.OrderedMap); ok && codexGroupHasPerm(gm) {
			return true
		}
	}
	return false
}

func codexRulesFile() string {
	return filepath.Join(util.CodexPathsResolved().Dir, "rules", "default.rules")
}

// InstallCodexRulesAllowlist writes the shell allowlist to ~/.codex/rules/default.rules.
// Surgical: only writes when the file does not exist — never overwrites user's rules.
func InstallCodexRulesAllowlist() {
	rulesFile := codexRulesFile()
	if util.Exists(rulesFile) {
		return
	}
	_ = util.EnsureDir(filepath.Dir(rulesFile))
	_ = util.WriteFile(rulesFile, `# tokless-managed codex allowlist — our tools pre-approved, everything else prompts.

prefix_rule(pattern = ["rtk"], decision = "allow")
prefix_rule(pattern = ["tokless"], decision = "allow")
prefix_rule(pattern = ["git"], decision = "allow")
prefix_rule(pattern = ["cd"], decision = "allow")
prefix_rule(pattern = ["ls"], decision = "allow")
prefix_rule(pattern = ["node"], decision = "allow")
prefix_rule(pattern = ["npm"], decision = "allow")
prefix_rule(pattern = ["npx"], decision = "allow")
prefix_rule(pattern = ["context-mode"], decision = "allow")
prefix_rule(pattern = ["codegraph"], decision = "allow")
prefix_rule(pattern = ["cat"], decision = "allow")
prefix_rule(pattern = ["head"], decision = "allow")
prefix_rule(pattern = ["tail"], decision = "allow")
prefix_rule(pattern = ["grep"], decision = "allow")
prefix_rule(pattern = ["find"], decision = "allow")
prefix_rule(pattern = ["pwd"], decision = "allow")
prefix_rule(pattern = ["which"], decision = "allow")
prefix_rule(pattern = ["echo"], decision = "allow")
prefix_rule(pattern = ["bash"], decision = "allow")
prefix_rule(pattern = ["sh"], decision = "allow")
`)
}

// RemoveCodexRulesAllowlist removes the allowlist file only if it carries our
// marker — never deletes a user-authored rules file.
func RemoveCodexRulesAllowlist() {
	if !HasCodexRulesAllowlist() {
		return
	}
	_ = os.Remove(codexRulesFile())
	_ = os.Remove(filepath.Dir(codexRulesFile())) // ok if non-empty
}

// HasCodexRulesAllowlist reports whether the allowlist file exists with our marker.
func HasCodexRulesAllowlist() bool {
	if !util.Exists(codexRulesFile()) {
		return false
	}
	raw, ok := util.ReadFileSafe(codexRulesFile())
	if !ok {
		return false
	}
	return strings.Contains(raw, "tokless-managed codex allowlist")
}

func applyCodexApprovalPolicy(raw string) string {
	if util.GetTomlTopKey(raw, "approval_policy") == "" {
		return util.SetTomlTopKey(raw, "approval_policy", "on-request")
	}
	return raw
}

// codexCleanupOrphanedConfig removes tokless-injected top-level config keys
// when no tokless-managed hooks remain in hooks.json.
func codexCleanupOrphanedConfig() {
	p := util.CodexPathsResolved()
	raw, ok := util.ReadFileSafe(p.Config)
	if !ok {
		return
	}
	changed := false
	if !codexHasAnyToklessHook() {
		if util.HasBlock(raw, "features") {
			raw = util.RemoveBlock(raw, "features")
			changed = true
		}
		if v := util.GetTomlTopKey(raw, "approval_policy"); v == "on-request" {
			raw = util.RemoveTomlTopKey(raw, "approval_policy")
			changed = true
		}
	}
	if changed {
		_ = util.WriteFile(p.Config, raw)
	}
}

// codexHasAnyToklessHook reports whether any tokless-managed hook group
// is still present in hooks.json.
func codexHasAnyToklessHook() bool {
	return HasCodexRtkHook() || HasCodexPermissionHook()
}

// mapChild fetches an OrderedMap child by key.
func mapChild(m *util.OrderedMap, key string) (*util.OrderedMap, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	om, ok := v.(*util.OrderedMap)
	return om, ok
}

func codexKnownBinDirs() []string {
	var dirs []string
	if d := os.Getenv("CODEX_INSTALL_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if util.IsWin {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs, filepath.Join(la, "Programs", "OpenAI", "Codex", "bin"))
		}
	}
	dirs = append(dirs,
		filepath.Join(util.Home(), ".local", "bin"),
		filepath.Join(util.Home(), ".cargo", "bin"),
	)
	return dirs
}

var codex = &core.AgentManifest{
	ID:        "codex",
	Label:     "Codex",
	Homepage:  "https://github.com/openai/codex",
	CLIBin:    "codex",
	ConfigDir: func() string { return util.CodexPathsResolved().Dir },
	Detect: func() core.Detection {
		return detectVSCodeAgent("codex", util.CodexPathsResolved().Dir, codexKnownBinDirs(), "openai.chatgpt")
	},
}

// Register wires all agents into the core registry.
func Register() {
	core.RegisterAgent(claude)
	core.RegisterAgent(opencode)
	core.RegisterAgent(codex)
	core.RegisterAgent(cursor)
	core.RegisterAgent(antigravity)
	core.RegisterAgent(copilot)
	core.RegisterAgent(grok)
	core.RegisterAgent(kilo)
	core.RegisterAgent(cline)
}
