package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

const clineProviderName = "tokless-headroom"
const clineProviderType = "openai-compatible"
const clineLegacyProviderName = "openai-compatible"

func clineProvidersFile() string {
	return filepath.Join(util.ClinePathsResolved().DataDir, "settings", "providers.json")
}
func clineProviderStateFile() string {
	return filepath.Join(util.ToklessDataDir(), "cline-provider-prev")
}

// clineDesiredProvider is the managed openai-compatible provider entry.
func clineDesiredProvider() *util.OrderedMap {
	env := util.NewOrderedMap()
	env.Set("provider", clineProviderType)
	env.Set("apiKey", proxyWireKey())
	env.Set("model", "deepseek-v4-flash")
	env.Set("baseUrl", ProxyEndpointFor("cline"))
	m := util.NewOrderedMap()
	m.Set("settings", env)
	return m
}

func clineRoutedProvider(existing *util.OrderedMap, register bool) (*util.OrderedMap, bool, func()) {
	settingsValue, ok := existing.Get("settings")
	if !ok {
		return nil, false, nil
	}
	settings, ok := settingsValue.(*util.OrderedMap)
	if !ok {
		return nil, false, nil
	}
	baseValue, ok := settings.Get("baseUrl")
	base, baseOK := baseValue.(string)
	if !ok || !baseOK || !isAbsoluteHTTP(base) || sameProxyBase(base, ProxyEndpointFor("cline")) {
		return nil, false, nil
	}
	cloned, err := util.ParseJsonc(util.StringifyJSON(existing))
	if err != nil {
		return nil, false, nil
	}
	routedValue, ok := cloned.Get("settings")
	if !ok {
		return nil, false, nil
	}
	routed, ok := routedValue.(*util.OrderedMap)
	if !ok {
		return nil, false, nil
	}
	headers := util.NewOrderedMap()
	if value, exists := routed.Get("headers"); exists {
		var headersOK bool
		headers, headersOK = value.(*util.OrderedMap)
		if !headersOK {
			return nil, false, nil
		}
	}
	upstream := normalizedHeadroomUpstream(base, "openai-completions")
	var previousRoute BYOKRoute
	routeRegistered := false
	// Only fail if HTTPS upstream registration fails
	if register && strings.HasPrefix(upstream, "https://") {
		if _, _, prev, ok := registerBYOKRoute("cline", clineProviderName, "openai-completions", upstream); !ok {
			return nil, false, nil
		} else {
			previousRoute = prev
			routeRegistered = true
		}
	} else if register {
		// HTTP upstreams can't be routed through BYOK gateway, but config still works
		if _, _, prev, ok := registerBYOKRoute("cline", clineProviderName, "openai-completions", upstream); ok {
			previousRoute = prev
			routeRegistered = true
		}
	}
	rollback := func() {
		if routeRegistered {
			rollbackBYOKRouteLogged("cline", clineProviderName, previousRoute)
		}
	}
	headers.Set(headroomBaseURLHeader, upstream)
	routed.Set("provider", clineProviderType)
	routed.Set("baseUrl", ProxyEndpointFor("cline"))
	routed.Set("headers", headers)
	return cloned, true, rollback
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
	for key, want := range map[string]any{"provider": clineProviderType, "baseUrl": ProxyEndpointFor("cline")} {
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

// clineReservedSlot returns the managed reserved slot, preferring the
// namespaced one. A user-owned entry is never reported.
func clineReservedSlot(providers *util.OrderedMap) (string, any, bool) {
	for _, name := range []string{clineProviderName, clineLegacyProviderName} {
		if v, ok := providers.Get(name); ok && clineManagedValues(v) {
			return name, v, true
		}
	}
	return "", nil, false
}

func ConfigureClineProxy() (bool, string) {
	var changed bool
	var file string
	if err := withProxyRouteStashLock(func() error {
		changed, file = configureClineProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("cline proxy lock failed: " + err.Error())
	}
	return changed, file
}

func configureClineProxyLocked() (bool, string) {
	file := clineProvidersFile()
	raw, exists := util.ReadFileSafe(file)
	if !exists && util.Exists(file) {
		return false, file
	}
	statePath := clineProviderStateFile()
	stateRaw, stateExists := util.ReadFileSafe(statePath)
	stateContent := ""
	cfg, providers, err := clineProviderConfig(raw)
	if err != nil {
		return false, file
	}
	if selected, ok := cfg.Get("lastUsedProvider"); ok {
		selectedName, isString := selected.(string)
		if !isString || (selectedName != clineProviderName && selectedName != clineLegacyProviderName) {
			return false, file
		}
	}
	slot, existing, managed := clineReservedSlot(providers)
	changed := false
	committed := false
	defer func() {
		if !committed && stateContent != "" {
			if current, ok := util.ReadFileSafe(statePath); ok && current == stateContent {
				_ = os.Remove(statePath)
			}
		}
	}()
	switch {
	case managed:
		existingMap, existingOK := existing.(*util.OrderedMap)
		if !existingOK || !stateExists {
			return false, file
		}
		if stateRaw, ok := util.ReadFileSafe(statePath); !ok || !clineStateMatchesRoute(stateRaw, existing, existingMap) {
			return false, file
		}
		if slot == clineLegacyProviderName {
			if occupied, occupiedOK := providers.Get(clineProviderName); occupiedOK && !clineManagedValues(occupied) && !jsonValueEqual(occupied, existingMap) {
				return false, file
			}
			providers.Delete(clineLegacyProviderName)
			providers.Set(clineProviderName, existingMap)
			changed = true
		}
	default:
		if occupied, ok := providers.Get(clineProviderName); ok && !clineManagedValues(occupied) {
			return false, file
		}
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
		providers.Set(clineProviderName, clineDesiredProvider())
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
		return false, file
	}
	committed = true
	if managed && slot == clineLegacyProviderName {
		if err := clineMigrateBYOKRoute(); err != nil {
			util.L.Err("cline BYOK route migration failed: " + err.Error())
		}
	}
	return true, file
}

// clineMigrateBYOKRoute moves a legacy BYOK route onto the namespaced slot.
func clineMigrateBYOKRoute() error {
	previous, exists := util.ReadBYOKRoute("cline:" + clineLegacyProviderName)
	if !exists {
		return nil
	}
	if _, _, err := util.UpsertBYOKRoute("cline:"+clineProviderName, previous.Protocol, previous.Upstream); err != nil {
		return err
	}
	return deleteBYOKRoute("cline", clineLegacyProviderName)
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
	routed, ok, _ := clineRoutedProvider(originalMap, false)
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
	routed, ok, _ := clineRoutedProvider(original, false)
	return ok && jsonValueEqual(routed, current)
}

func RemoveClineProxy() (removed bool) {
	if err := withProxyRouteStashLock(func() error {
		removed = removeClineProxyLocked()
		return nil
	}); err != nil {
		util.L.Err("cline proxy lock failed: " + err.Error())
	}
	return removed
}

func removeClineProxyLocked() bool {
	file := clineProvidersFile()
	raw, ok := util.ReadFileSafe(file)
	if !ok {
		return false
	}
	cfg, providers, err := clineProviderConfig(raw)
	if err != nil {
		return false
	}
	slot, existing, managed := clineReservedSlot(providers)
	if !managed {
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
			providers.Set(slot, v)
		} else {
			providers.Delete(slot)
		}
		if v, exists := state.Get("lastUsedProvider"); exists && v != nil {
			cfg.Set("lastUsedProvider", v)
		} else {
			cfg.Delete("lastUsedProvider")
		}
	} else {
		providers.Delete(slot)
		if v, exists := cfg.Get("lastUsedProvider"); exists {
			if name, ok := v.(string); ok && (name == clineProviderName || name == clineLegacyProviderName) {
				cfg.Delete("lastUsedProvider")
			}
		}
	}
	var deletedRoutes []BYOKRoute
	for _, name := range []string{clineProviderName, clineLegacyProviderName} {
		if route, exists := util.ReadBYOKRoute("cline:" + name); exists {
			deletedRoutes = append(deletedRoutes, route)
			if err := deleteBYOKRoute("cline", name); err != nil {
				restoreDeletedBYOKRoutes([]BYOKRoute{route})
				return false
			}
		}
	}
	if err := clineWriteFileGuarded(file, util.StringifyJSON(cfg), raw, true); err != nil {
		restoreDeletedBYOKRoutes(deletedRoutes)
		return false
	}
	if err := os.Remove(clineProviderStateFile()); err != nil && !os.IsNotExist(err) {
		_ = clineWriteFileGuarded(file, raw, util.StringifyJSON(cfg), true)
		restoreDeletedBYOKRoutes(deletedRoutes)
		return false
	}
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
	_, _, managed := clineReservedSlot(providers)
	return managed
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
		legacy, legacyOK := providers.Get(clineLegacyProviderName)
		switch {
		case legacyOK && clineManagedValues(legacy):
			return proxyDetection(cap.ID, "legacy managed provider", ProxyStateManaged)
		case legacyOK:
			return proxyDetection(cap.ID, "legacy reserved provider differs", ProxyStateUnconfigured)
		}
		return proxyDetection(cap.ID, "reserved provider absent", ProxyStateUnconfigured)
	}
	if clineManagedValues(v) {
		return proxyDetection(cap.ID, "exact managed provider", ProxyStateManaged)
	}
	return proxyDetection(cap.ID, "reserved provider differs", ProxyStateConflict)
}
