package agents

import (
	"os"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// ProxyProtocol describes the wire protocol a client uses for proxy traffic.
type ProxyProtocol string

const (
	ProxyProtocolAnthropicNative  ProxyProtocol = "anthropic-native"
	ProxyProtocolOpenAICompatible ProxyProtocol = "openai-compatible"
	ProxyProtocolGeminiNative     ProxyProtocol = "gemini-native"
	ProxyProtocolNone             ProxyProtocol = "none"
)

// ProxyWireKind describes how tokless can reach an agent.
type ProxyWireKind string

const (
	ProxyWireManagedRoute     ProxyWireKind = "managed-route"
	ProxyWireAdditiveProvider ProxyWireKind = "additive-provider/model"
	ProxyWireManual           ProxyWireKind = "manual"
)

// ProxyConfigState is an observed, read-only configuration state.
type ProxyConfigState string

const (
	ProxyStateManaged      ProxyConfigState = "managed"
	ProxyStateUnconfigured ProxyConfigState = "unconfigured"
	ProxyStateAbsent       ProxyConfigState = "absent"
	ProxyStateForeignBYOK  ProxyConfigState = "foreign-byok"
	ProxyStateConflict     ProxyConfigState = "conflict"
	ProxyStateUnreadable   ProxyConfigState = "unreadable"
	ProxyStateUnknown      ProxyConfigState = "unknown"
)

// ProxyCapability is static metadata. Detail is intentionally bounded and non-secret.
type ProxyCapability struct {
	ID       string
	Protocol ProxyProtocol
	WireKind ProxyWireKind
}

// ProxyDetection combines static capability with observed local state.
type ProxyDetection struct {
	Capability ProxyCapability
	State      ProxyConfigState
	Detail     string
}

// Default Headroom cache mode preserves provider-prefix caching.
const ProxyCachePolicy = "cache mode preserves provider-prefix cache; semantic response cache disabled"

var proxyCapabilities = func() map[string]ProxyCapability {
	out := make(map[string]ProxyCapability, len(proxyAgentSpecs))
	for id, spec := range proxyAgentSpecs {
		out[id] = ProxyCapability{ID: spec.ID, Protocol: spec.Protocol, WireKind: spec.WireKind}
	}
	out["grok"] = ProxyCapability{ID: "grok", Protocol: ProxyProtocolOpenAICompatible, WireKind: ProxyWireAdditiveProvider}
	return out
}()

// ProxyCapabilities returns the static proxy capability registry.
func ProxyCapabilities() map[string]ProxyCapability {
	out := make(map[string]ProxyCapability, len(proxyCapabilities))
	for id, capability := range proxyCapabilities {
		out[id] = capability
	}
	return out
}

func proxyDetection(id, detail string, state ProxyConfigState) ProxyDetection {
	return ProxyDetection{Capability: proxyCapabilities[id], State: state, Detail: detail}
}

func readProxyConfig(path string) (string, error) {
	raw, err := os.ReadFile(path)
	return string(raw), err
}

// DetectProxy reads only documented proxy slots and never writes or probes.
func DetectProxy(id string) ProxyDetection {
	capability, ok := proxyCapabilities[id]
	if !ok {
		return ProxyDetection{Capability: ProxyCapability{ID: id, Protocol: ProxyProtocolNone, WireKind: ProxyWireManual}, State: ProxyStateUnknown, Detail: "unsupported agent"}
	}
	switch id {
	case "claude":
		return detectClaudeProxy(capability)
	case "omp":
		return detectOmpProxy(capability)
	case "codex":
		if CodexProxyWired() {
			return proxyDetection(id, "exact managed endpoint; model_providers.headroom through proxy", ProxyStateManaged)
		}
		raw, err := readProxyConfig(util.CodexPathsResolved().Config)
		if err != nil {
			if os.IsNotExist(err) {
				return proxyDetection(id, "config absent", ProxyStateAbsent)
			}
			return proxyDetection(id, "config unreadable", ProxyStateUnreadable)
		}
		if strings.TrimSpace(raw) == "" {
			return proxyDetection(id, "config empty", ProxyStateUnreadable)
		}
		if util.HasBlock(raw, codexProxyProvider) {
			return proxyDetection(id, "headroom provider differs", ProxyStateConflict)
		}
		return proxyDetection(id, "headroom provider not configured", ProxyStateUnconfigured)
	case "opencode":
		if OpenCodeProxyWired() {
			return proxyDetection(id, "managed transport plugin preserves provider endpoint and credentials", ProxyStateManaged)
		}
		spec := ProviderSpecActive()
		return detectProviderProxy(capability, util.OpenCodePathsResolved().Config, "provider", spec.Key, openCodeProxyProviderBlockFor(ProxyEndpointFor(id), spec))
	case "opencode2":
		if OpenCodeProxyV2Wired() {
			return proxyDetection(id, "managed provider mirror points at the proxy endpoint", ProxyStateManaged)
		}
		return detectOpenCodeV2NativeProxy(capability)
	case "kilo":
		if kiloTransportPluginWired() {
			return proxyDetection(id, "managed transport plugin preserves provider endpoint and credentials", ProxyStateManaged)
		}
		target := kiloTransportPluginTarget()
		raw, err := readProxyConfig(target)
		if err != nil {
			if os.IsNotExist(err) {
				return proxyDetection(id, "config absent", ProxyStateAbsent)
			}
			return proxyDetection(id, "config unreadable", ProxyStateUnreadable)
		}
		if util.HasJSONCComments(raw) {
			return proxyDetection(id, "config has JSONC comments", ProxyStateUnreadable)
		}
		return proxyDetection(id, "transport plugin not configured", ProxyStateUnconfigured)
	case "pi":
		if PiProxyWired() {
			return proxyDetection(id, "native BYOK providers routed through headroom", ProxyStateManaged)
		}
		return detectProviderProxy(capability, piModelsFile(), "providers", piProxyProvider, piProxyProviderEntry(ProxyEndpointFor(id)))
	case "droid":
		return detectDroidProxy(capability)
	case "grok":
		return detectGrokProxy(capability)
	case "copilot":
		return detectCopilotProxy(capability)
	case "cline":
		return detectClineProxy(capability)
	case "kimi":
		return detectKimiProxy(capability)
	case "zcode":
		return detectZcodeProxy(capability)
	case "dsh":
		return detectDshProxy(capability)
	case "cursor":
		return proxyDetection(id, "manual configuration not observable", ProxyStateUnknown)
	case "antigravity":
		return detectAntigravityProxy(capability)
	default:
		return proxyDetection(id, "unsupported agent", ProxyStateUnknown)
	}
}

func detectClaudeProxy(cap ProxyCapability) ProxyDetection {
	path := util.ClaudeCodePaths().Settings
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "settings file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "settings unreadable", ProxyStateUnreadable)
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return proxyDetection(cap.ID, "settings unreadable", ProxyStateUnreadable)
	}
	container, ok := mapChild(cfg, "env")
	if !ok {
		if _, present := cfg.Get("env"); present {
			return proxyDetection(cap.ID, "documented env slot unreadable", ProxyStateUnreadable)
		}
		return proxyDetection(cap.ID, "documented endpoint not configured", ProxyStateUnconfigured)
	}
	v, present := container.Get(claudeProxyEnvKey)
	if !present {
		return proxyDetection(cap.ID, "documented endpoint not configured", ProxyStateUnconfigured)
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return proxyDetection(cap.ID, "documented endpoint unreadable", ProxyStateUnreadable)
	}
	if s == ProxyEndpointFor(cap.ID) {
		return proxyDetection(cap.ID, "exact managed endpoint", ProxyStateManaged)
	}
	return proxyDetection(cap.ID, "documented endpoint differs", ProxyStateForeignBYOK)
}

func detectOmpProxy(cap ProxyCapability) ProxyDetection {
	if OmpProxyWired() {
		return proxyDetection(cap.ID, "native BYOK providers routed through headroom", ProxyStateManaged)
	}
	path := ompModelsFile()
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "models file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "models file unreadable", ProxyStateUnreadable)
	}
	if strings.TrimSpace(raw) == "" {
		return proxyDetection(cap.ID, "models file unreadable", ProxyStateUnreadable)
	}
	ref := ompYamlScan(raw)
	if ref.provider < 0 {
		return proxyDetection(cap.ID, "headroom provider not configured", ProxyStateUnconfigured)
	}
	if !ompYamlHeadroomManaged(raw) {
		return proxyDetection(cap.ID, "headroom provider differs", ProxyStateConflict)
	}
	return proxyDetection(cap.ID, "managed headroom provider; default role state observed separately", ProxyStateManaged)
}

func detectCodexProxy(cap ProxyCapability) ProxyDetection {
	path := util.CodexPathsResolved().Config
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "config file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "config unreadable", ProxyStateUnreadable)
	}
	if strings.TrimSpace(raw) == "" {
		return proxyDetection(cap.ID, "config unreadable", ProxyStateUnreadable)
	}
	return proxyDetection(cap.ID, "verified TOML parsing unavailable; config state unknown", ProxyStateUnknown)
}

func detectProviderProxy(cap ProxyCapability, path, containerKey, reserved string, desired any) ProxyDetection {
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "config file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "config unreadable", ProxyStateUnreadable)
	}
	if util.HasJSONCComments(raw) {
		return proxyDetection(cap.ID, "comment-bearing config not safely inspectable", ProxyStateUnreadable)
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return proxyDetection(cap.ID, "config unreadable", ProxyStateUnreadable)
	}
	providers, ok := mapChild(cfg, containerKey)
	if !ok {
		if _, present := cfg.Get(containerKey); present {
			return proxyDetection(cap.ID, "documented provider slot unreadable", ProxyStateUnreadable)
		}
		return proxyDetection(cap.ID, "reserved provider slot not configured", ProxyStateUnconfigured)
	}
	existing, present := providers.Get(reserved)
	if present {
		if jsonEqual(existing, desired) || util.StringifyJSON(existing) == util.StringifyJSON(desired) {
			return proxyDetection(cap.ID, "exact reserved provider", ProxyStateManaged)
		}
		return proxyDetection(cap.ID, "reserved provider differs", ProxyStateConflict)
	}
	if providers.Len() > 0 {
		return proxyDetection(cap.ID, "foreign provider present; reserved slot absent", ProxyStateUnconfigured)
	}
	return proxyDetection(cap.ID, "reserved provider slot not configured", ProxyStateUnconfigured)
}

// detectOpenCodeV2NativeProxy reads the native OpenCode v2 "providers" map.
func detectOpenCodeV2NativeProxy(cap ProxyCapability) ProxyDetection {
	raw, err := readProxyConfig(util.OpenCodePathsV2().Config)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "config file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "config file unreadable", ProxyStateUnreadable)
	}
	if util.HasJSONCComments(raw) {
		return proxyDetection(cap.ID, "comment-bearing config not safely inspectable", ProxyStateUnreadable)
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return proxyDetection(cap.ID, "config file unreadable", ProxyStateUnreadable)
	}
	providers, ok := mapChild(cfg, "providers")
	if !ok {
		if _, present := cfg.Get("providers"); present {
			return proxyDetection(cap.ID, "documented provider slot unreadable", ProxyStateUnreadable)
		}
		spec := ProviderSpecActive()
		return detectProviderProxy(cap, util.OpenCodePathsV2().Config, "provider", spec.Key, openCodeProxyProviderBlockFor(ProxyEndpointFor(cap.ID), spec))
	}
	if providers.Len() == 0 {
		return proxyDetection(cap.ID, "native provider map is empty", ProxyStateUnconfigured)
	}
	endpoint := openCodeProxyEndpointFor(cap.ID)
	for _, id := range providers.Keys() {
		value, _ := providers.Get(id)
		entry, ok := value.(*util.OrderedMap)
		if !ok {
			continue
		}
		base, _ := openCodeV2ProviderBaseAndKey(entry)
		if openCodeV2ProxyEndpoint(base, endpoint) {
			return proxyDetection(cap.ID, "native provider points at a proxy endpoint", ProxyStateConflict)
		}
	}
	return proxyDetection(cap.ID, "foreign native provider present; managed mirror absent", ProxyStateUnconfigured)
}

func detectDroidProxy(cap ProxyCapability) ProxyDetection {
	if DroidProxyWired() {
		return proxyDetection(cap.ID, "native BYOK custom models routed through headroom", ProxyStateManaged)
	}
	path := droidSettingsFile()
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "settings file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "settings unreadable", ProxyStateUnreadable)
	}
	if util.HasJSONCComments(raw) {
		return proxyDetection(cap.ID, "comment-bearing config not safely inspectable", ProxyStateUnreadable)
	}
	cfg := util.TryParseJsonc(raw)
	if cfg == nil {
		return proxyDetection(cap.ID, "settings unreadable", ProxyStateUnreadable)
	}
	v, present := cfg.Get("customModels")
	if !present {
		return proxyDetection(cap.ID, "reserved custom model absent", ProxyStateUnconfigured)
	}
	models, ok := v.([]any)
	if !ok {
		return proxyDetection(cap.ID, "customModels slot unreadable", ProxyStateUnreadable)
	}
	for _, entry := range models {
		m, ok := entry.(*util.OrderedMap)
		if !ok {
			continue
		}
		model, _ := m.Get("model")
		display, _ := m.Get("displayName")
		if model == proxyWireModel() && display == droidProxyDisplayName {
			if droidEntryManaged(m, droidProxyEntry(ProxyEndpointFor(cap.ID))) {
				return proxyDetection(cap.ID, "exact reserved custom model", ProxyStateManaged)
			}
			return proxyDetection(cap.ID, "reserved custom model differs", ProxyStateConflict)
		}
	}
	return proxyDetection(cap.ID, "reserved custom model absent", ProxyStateUnconfigured)
}

func detectManualEnv(cap ProxyCapability, key, endpoint string) ProxyDetection {
	value, set := os.LookupEnv(key)
	if !set || strings.TrimSpace(value) == "" {
		return proxyDetection(cap.ID, "manual endpoint not observed", ProxyStateUnknown)
	}
	if value == endpoint {
		return proxyDetection(cap.ID, "manual endpoint equals proxy; ownership unknown", ProxyStateUnknown)
	}
	return proxyDetection(cap.ID, "documented environment endpoint differs", ProxyStateForeignBYOK)
}

// detectKimiProxy reports Kimi gateway state; unstashed gateway providers are conflicts.
func detectKimiProxy(cap ProxyCapability) ProxyDetection {
	if KimiProxyWired() {
		return proxyDetection(cap.ID, "native OpenAI-compatible providers routed through Headroom", ProxyStateManaged)
	}
	path := util.KimiPathsResolved().Config
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "config absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "config unreadable", ProxyStateUnreadable)
	}
	if strings.TrimSpace(raw) == "" {
		return proxyDetection(cap.ID, "config empty", ProxyStateUnconfigured)
	}
	if kimiHasGatewayProvider(raw) {
		return proxyDetection(cap.ID, "provider points at BYOK gateway without a verified stash", ProxyStateConflict)
	}
	return proxyDetection(cap.ID, "OpenAI-compatible provider not routed through BYOK gateway", ProxyStateUnconfigured)
}

// detectZcodeProxy reports ZCode proxy state; unstashed proxy providers are conflicts.
func detectZcodeProxy(cap ProxyCapability) ProxyDetection {
	if ZcodeProxyWired() {
		return proxyDetection(cap.ID, "native OpenAI-compatible providers routed through Headroom", ProxyStateManaged)
	}
	path := zcodeProviderConfigPath()
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "config absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "config unreadable", ProxyStateUnreadable)
	}
	if strings.TrimSpace(raw) == "" {
		return proxyDetection(cap.ID, "config empty", ProxyStateUnconfigured)
	}
	if util.HasJSONCComments(raw) {
		return proxyDetection(cap.ID, "comment-bearing config not safely inspectable", ProxyStateUnreadable)
	}
	if zcodeHasGatewayProvider(raw) {
		if zcodeGatewayRecoverable(raw) {
			return proxyDetection(cap.ID, "legacy gateway route recoverable from BYOK route table", ProxyStateUnconfigured)
		}
		return proxyDetection(cap.ID, "provider points at BYOK gateway without a verified stash", ProxyStateConflict)
	}
	return proxyDetection(cap.ID, "provider not routed through Headroom", ProxyStateUnconfigured)
}

// detectDshProxy reports DSH proxy state; unstashed proxy providers are conflicts.
func detectDshProxy(cap ProxyCapability) ProxyDetection {
	if DshProxyWired() {
		return proxyDetection(cap.ID, "native OpenAI-compatible providers routed through Headroom", ProxyStateManaged)
	}
	path := dshPatchFile()
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "cordis patch absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "cordis patch unreadable", ProxyStateUnreadable)
	}
	if strings.TrimSpace(raw) == "" {
		return proxyDetection(cap.ID, "cordis patch empty", ProxyStateUnconfigured)
	}
	if dshHasGatewayProvider(raw) {
		if dshGatewayRecoverable(raw) {
			return proxyDetection(cap.ID, "legacy gateway route recoverable from BYOK route table", ProxyStateUnconfigured)
		}
		return proxyDetection(cap.ID, "provider points at BYOK gateway without a verified stash", ProxyStateConflict)
	}
	return proxyDetection(cap.ID, "provider not routed through Headroom", ProxyStateUnconfigured)
}

func detectAntigravityProxy(cap ProxyCapability) ProxyDetection {
	path := antigravityEnvFile()
	raw, err := readProxyConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "automatic route not configured", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "env file unreadable", ProxyStateUnreadable)
	}
	value := antigravityEnvValue(raw, antigravityProxyEnvKey)
	cloud := antigravityEnvValue(raw, antigravityCloudCodeKey)
	if value == "" && cloud == "" {
		return proxyDetection(cap.ID, "automatic route not configured", ProxyStateUnconfigured)
	}
	if value == ProxyEndpointFor(cap.ID) && cloud == ProxyEndpointFor(cap.ID) {
		if AntigravityProxyWired() {
			surfaces := "CLI launcher"
			if antigravityIDEApplicable() {
				surfaces += " and IDE setting"
			}
			return proxyDetection(cap.ID, "managed "+surfaces, ProxyStateManaged)
		}
		if !antigravityShimCompatible() {
			return proxyDetection(cap.ID, "existing agy launcher is not Tokless-owned", ProxyStateConflict)
		}
		if !antigravityIDECompatible() {
			return proxyDetection(cap.ID, "IDE endpoint differs or settings are not safely editable", ProxyStateForeignBYOK)
		}
		return proxyDetection(cap.ID, "automatic route incomplete", ProxyStateUnconfigured)
	}
	return proxyDetection(cap.ID, "documented endpoint differs", ProxyStateForeignBYOK)
}
