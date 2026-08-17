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
	env.Set("apiKey", os.Getenv("TOKLESS_OPENCODE_GO_KEY"))
	env.Set("model", "deepseek-v4-flash")
	env.Set("baseUrl", ProxyEndpointFor("cline"))
	m := util.NewOrderedMap()
	m.Set("settings", env)
	return m
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
	for key, want := range map[string]any{"provider": clineProviderName, "model": "deepseek-v4-flash", "baseUrl": ProxyEndpointFor("cline")} {
		have, present := settings.Get(key)
		if !present || !jsonValueEqual(have, want) {
			return false
		}
	}
	return true
}

func ConfigureClineProxy() (bool, string) {
	file := clineProvidersFile()
	raw, _ := util.ReadFileSafe(file)
	cfg, providers, err := clineProviderConfig(raw)
	if err != nil {
		return false, file
	}
	desired := clineDesiredProvider()
	changed := false
	if existing, ok := providers.Get(clineProviderName); ok {
		if !clineManagedValues(existing) {
			return false, file
		}
	} else {
		state := util.NewOrderedMap()
		if existing, ok := providers.Get(clineProviderName); ok {
			state.Set("provider", existing)
		} else {
			state.Set("provider", nil)
		}
		if v, ok := cfg.Get("lastUsedProvider"); ok {
			state.Set("lastUsedProvider", v)
		} else {
			state.Set("lastUsedProvider", nil)
		}
		_ = util.WriteFile(clineProviderStateFile(), util.StringifyJSON(state))
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
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
		return false, file
	}
	return true, file
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
		state, err := util.ParseJsonc(stateRaw)
		if err == nil {
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
		}
	} else {
		providers.Delete(clineProviderName)
		if v, ok := cfg.Get("lastUsedProvider"); ok && v == clineProviderName {
			cfg.Delete("lastUsedProvider")
		}
	}
	if err := util.WriteFile(file, util.StringifyJSON(cfg)); err != nil {
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
