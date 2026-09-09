package agents

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/util"
)

const clineProviderName = "openai-compatible"

func clineProvidersFile() string {
	return filepath.Join(util.ClinePathsResolved().DataDir, "settings", "providers.json")
}
func clineProviderStateFile() string {
	return filepath.Join(util.ToklessDataDir(), "cline-provider-prev")
}

// clineDesiredProvider is the managed openai-compatible provider entry.
func clineDesiredProvider() *util.OrderedMap {
	env := util.NewOrderedMap()
	env.Set("provider", clineProviderName)
	env.Set("apiKey", proxyWireKey())
	env.Set("model", "deepseek-v4-flash")
	env.Set("baseUrl", ProxyEndpointFor("cline"))
	m := util.NewOrderedMap()
	m.Set("settings", env)
	return m
}

func clineRoutedProvider(existing *util.OrderedMap) (*util.OrderedMap, bool) {
	settingsValue, ok := existing.Get("settings")
	if !ok {
		return nil, false
	}
	settings, ok := settingsValue.(*util.OrderedMap)
	if !ok {
		return nil, false
	}
	baseValue, ok := settings.Get("baseUrl")
	base, baseOK := baseValue.(string)
	if !ok || !baseOK || !isAbsoluteHTTP(base) || sameProxyBase(base, ProxyEndpointFor("cline")) {
		return nil, false
	}
	cloned, err := util.ParseJsonc(util.StringifyJSON(existing))
	if err != nil {
		return nil, false
	}
	routedValue, ok := cloned.Get("settings")
	if !ok {
		return nil, false
	}
	routed, ok := routedValue.(*util.OrderedMap)
	if !ok {
		return nil, false
	}
	headers := util.NewOrderedMap()
	if value, exists := routed.Get("headers"); exists {
		var headersOK bool
		headers, headersOK = value.(*util.OrderedMap)
		if !headersOK {
			return nil, false
		}
	}
	headers.Set(headroomBaseURLHeader, normalizedHeadroomUpstream(base, "openai-completions"))
	routed.Set("provider", clineProviderName)
	routed.Set("baseUrl", ProxyEndpointFor("cline"))
	routed.Set("headers", headers)
	return cloned, true
}

func clineProviderConfig(raw string) (*util.OrderedMap, *util.OrderedMap, error) {
	cfg, err := util.ParseJsonc(raw)
	if err != nil {
		return nil, nil, err
	}
	providers := util.NewOrderedMap()
	if v, ok := cfg.Get("providers"); ok {
		var good bool
		providers, good = v.(*util.OrderedMap)
		if !good {
			return nil, nil, os.ErrInvalid
		}
	} else {
		cfg.Set("providers", providers)
	}
	return cfg, providers, nil
}

func jsonValueEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func clineManagedValues(v any) bool {
	m, ok := v.(*util.OrderedMap)
	if !ok {
		return false
	}
	settingsAny, present := m.Get("settings")
	if !present {
		return false
	}
	settings, ok := settingsAny.(*util.OrderedMap)
	if !ok {
		return false
	}
	for key, want := range map[string]any{"provider": clineProviderName, "baseUrl": ProxyEndpointFor("cline")} {
		have, present := settings.Get(key)
		if !present || !jsonValueEqual(have, want) {
			return false
		}
	}
	if model, present := settings.Get("model"); !present || model == "" {
		return false
	}
	if headers, present := settings.Get("headers"); present {
		h, ok := headers.(*util.OrderedMap)
		if !ok {
			return false
		}
		if value, exists := h.Get(headroomBaseURLHeader); exists {
			upstream, ok := value.(string)
			return ok && upstream != ""
		}
	}
	return jsonValueEqual(settingsValue(settings, "model"), "deepseek-v4-flash")
}

func settingsValue(settings *util.OrderedMap, key string) any {
	v, _ := settings.Get(key)
	return v
}

func ConfigureClineProxy() (bool, string) {
	file := clineProvidersFile()
	raw, exists := util.ReadFileSafe(file)
	if !exists && util.Exists(file) {
		return false, file
	}
	statePath := clineProviderStateFile()
	stateRaw, stateExists := util.ReadFileSafe(statePath)
	stateCreated := false
	cfg, providers, err := clineProviderConfig(raw)
	if err != nil {
		return false, file
	}
	desired := clineDesiredProvider()
	changed := false
	stateContent := ""
	if existing, ok := providers.Get(clineProviderName); ok {
		existingMap, existingOK := existing.(*util.OrderedMap)
		if !existingOK {
			return false, file
		}
		var routed *util.OrderedMap
		if clineManagedValues(existing) {
			if !stateExists {
				return false, file
			}
			routed = existingMap
		} else {
			var routedOK bool
			routed, routedOK = clineRoutedProvider(existingMap)
			if !routedOK {
				return false, file
			}
		}
		if !stateExists {
			state := util.NewOrderedMap()
			state.Set("provider", existing)
			if v, ok := cfg.Get("lastUsedProvider"); ok {
				state.Set("lastUsedProvider", v)
			} else {
				state.Set("lastUsedProvider", nil)
			}
			stateContent = util.StringifyJSON(state)
			if err := clineWriteFileGuarded(statePath, stateContent, stateRaw, stateExists); err != nil {
				return false, file
			}
			stateExists = true
			stateCreated = true
			changed = true
		}
		if stateRaw, ok := util.ReadFileSafe(statePath); !ok || !clineStateMatchesRoute(stateRaw, existing, routed) {
			return false, file
		}
		if !jsonValueEqual(existing, routed) {
			providers.Set(clineProviderName, routed)
			changed = true
		}
	} else {
		state := util.NewOrderedMap()
		state.Set("provider", nil)
		if v, ok := cfg.Get("lastUsedProvider"); ok {
			state.Set("lastUsedProvider", v)
		} else {
			state.Set("lastUsedProvider", nil)
		}
		stateContent = util.StringifyJSON(state)
		if err := clineWriteFileGuarded(statePath, stateContent, stateRaw, stateExists); err != nil {
			return false, file
		}
		stateCreated = true
		providers.Set(clineProviderName, desired)
		changed = true
	}
	if v, ok := cfg.Get("lastUsedProvider"); !ok || v != clineProviderName {
		cfg.Set("lastUsedProvider", clineProviderName)
		changed = true
	}
	if !changed {
		return false, file
	}
	if err := clineWriteFileGuarded(file, util.StringifyJSON(cfg), raw, exists); err != nil {
		if stateCreated {
			if current, ok := util.ReadFileSafe(statePath); ok && current == stateContent {
				_ = os.Remove(statePath)
			}
		}
		return false, file
	}
	return true, file
}

func clineStateMatchesRoute(raw string, original, current any) bool {
	state, err := util.ParseJsonc(raw)
	if err != nil {
		return false
	}
	originalValue, exists := state.Get("provider")
	if !exists || originalValue == nil {
		return clineManagedValues(current)
	}
	originalMap, ok := originalValue.(*util.OrderedMap)
	if !ok || !jsonValueEqual(originalMap, original) {
		return false
	}
	routed, ok := clineRoutedProvider(originalMap)
	return ok && jsonValueEqual(routed, current)
}

func clineStateOwnsCurrent(raw string, current any) bool {
	state, err := util.ParseJsonc(raw)
	if err != nil {
		return false
	}
	originalValue, exists := state.Get("provider")
	if !exists || originalValue == nil {
		return clineManagedValues(current)
	}
	original, ok := originalValue.(*util.OrderedMap)
	if !ok {
		return false
	}
	routed, ok := clineRoutedProvider(original)
	return ok && jsonValueEqual(routed, current)
}

func RemoveClineProxy() bool {
	file := clineProvidersFile()
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false
	}
	cfg, providers, err := clineProviderConfig(raw)
	if err != nil {
		return false
	}
	existing, ok := providers.Get(clineProviderName)
	if !ok || !clineManagedValues(existing) {
		return false
	}
	if stateRaw, stateOK := util.ReadFileSafe(clineProviderStateFile()); stateOK {
		if !clineStateOwnsCurrent(stateRaw, existing) {
			return false
		}
		state, err := util.ParseJsonc(stateRaw)
		if err != nil {
			return false
		}
		if v, exists := state.Get("provider"); exists && v != nil {
			providers.Set(clineProviderName, v)
		} else {
			providers.Delete(clineProviderName)
		}
		if v, exists := state.Get("lastUsedProvider"); exists && v != nil {
			cfg.Set("lastUsedProvider", v)
		} else {
			cfg.Delete("lastUsedProvider")
		}
	} else {
		return false
	}
	if err := clineWriteFileGuarded(file, util.StringifyJSON(cfg), raw, true); err != nil {
		return false
	}
	_ = os.Remove(clineProviderStateFile())
	return true
}

func ClineProxyWired() bool {
	raw, ok := util.ReadFileSafe(clineProvidersFile())
	if !ok {
		return false
	}
	_, providers, err := clineProviderConfig(raw)
	if err != nil {
		return false
	}
	v, ok := providers.Get(clineProviderName)
	return ok && clineManagedValues(v)
}

func detectClineProxy(cap ProxyCapability) ProxyDetection {
	raw, err := readProxyConfig(clineProvidersFile())
	if err != nil {
		if os.IsNotExist(err) {
			return proxyDetection(cap.ID, "providers file absent", ProxyStateAbsent)
		}
		return proxyDetection(cap.ID, "providers unreadable", ProxyStateUnreadable)
	}
	_, providers, err := clineProviderConfig(raw)
	if err != nil {
		return proxyDetection(cap.ID, "providers unreadable", ProxyStateUnreadable)
	}
	v, ok := providers.Get(clineProviderName)
	if !ok {
		return proxyDetection(cap.ID, "reserved provider absent", ProxyStateUnconfigured)
	}
	if clineManagedValues(v) {
		return proxyDetection(cap.ID, "exact managed provider", ProxyStateManaged)
	}
	return proxyDetection(cap.ID, "reserved provider differs", ProxyStateConflict)
}
