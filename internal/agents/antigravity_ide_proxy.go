package agents

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/HoangP8/tokless/internal/util"
)

const antigravityIDEEndpointKey = "jetski.cloudCodeUrl"

type antigravityIDEProxyState struct {
	Path     string `json:"path"`
	HadValue bool   `json:"hadValue"`
	Value    any    `json:"value,omitempty"`
}

func antigravityIDESettingsCandidates() []string {
	home := util.Home()
	switch goosForDetect {
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return []string{filepath.Join(base, "Antigravity IDE", "User", "settings.json"), filepath.Join(base, "Antigravity", "User", "settings.json")}
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		return []string{filepath.Join(base, "Antigravity IDE", "User", "settings.json"), filepath.Join(base, "Antigravity", "User", "settings.json")}
	default:
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return []string{filepath.Join(base, "Antigravity IDE", "User", "settings.json"), filepath.Join(base, "Antigravity", "User", "settings.json")}
	}
}

func antigravityIDESettingsFile() (string, bool) {
	for _, path := range antigravityIDESettingsCandidates() {
		if util.Exists(path) {
			return path, true
		}
	}
	for _, path := range antigravityIDESettingsCandidates() {
		if util.Exists(filepath.Dir(path)) {
			return path, true
		}
	}
	return "", false
}

func antigravityIDEStateFile() string {
	return filepath.Join(util.ToklessDataDir(), "antigravity-ide-proxy.json")
}

func antigravityIDEConfig() (path string, cfg *util.OrderedMap, raw string, ok bool) {
	path, applicable := antigravityIDESettingsFile()
	if !applicable {
		return "", nil, "", false
	}
	raw, exists := util.ReadFileSafe(path)
	if !exists {
		if util.Exists(path) {
			return path, nil, "", false
		}
		return path, util.NewOrderedMap(), "", true
	}
	if util.HasJSONCComments(raw) {
		return path, nil, raw, false
	}
	cfg = util.TryParseJsonc(raw)
	return path, cfg, raw, cfg != nil
}

func antigravityIDECompatible() bool {
	_, cfg, _, ok := antigravityIDEConfig()
	if !ok {
		return !antigravityIDEApplicable()
	}
	value, exists := cfg.Get(antigravityIDEEndpointKey)
	return !exists || value == "" || value == antigravityURL()
}

func antigravityIDEApplicable() bool {
	_, ok := antigravityIDESettingsFile()
	return ok
}

func configureAntigravityIDE() (bool, error) {
	path, cfg, _, ok := antigravityIDEConfig()
	if !ok {
		if !antigravityIDEApplicable() {
			return false, nil
		}
		return false, os.ErrInvalid
	}
	old, had := cfg.Get(antigravityIDEEndpointKey)
	if had && old != "" && old != antigravityURL() {
		return false, os.ErrExist
	}
	if AntigravityIDEProxyWired() {
		return false, nil
	}
	state, err := json.Marshal(antigravityIDEProxyState{Path: path, HadValue: had, Value: old})
	if err != nil {
		return false, err
	}
	if err := util.WriteFile(antigravityIDEStateFile(), string(state)); err != nil {
		return false, err
	}
	cfg.Set(antigravityIDEEndpointKey, antigravityURL())
	if err := util.WriteFile(path, util.StringifyJSON(cfg)); err != nil {
		_ = os.Remove(antigravityIDEStateFile())
		return false, err
	}
	return true, nil
}

func readAntigravityIDEState() (antigravityIDEProxyState, bool) {
	var state antigravityIDEProxyState
	raw, ok := util.ReadFileSafe(antigravityIDEStateFile())
	if !ok || json.Unmarshal([]byte(raw), &state) != nil {
		return state, false
	}
	for _, candidate := range antigravityIDESettingsCandidates() {
		if filepath.Clean(state.Path) == filepath.Clean(candidate) {
			return state, true
		}
	}
	return state, false
}

func removeAntigravityIDE() bool {
	state, ok := readAntigravityIDEState()
	if !ok {
		return false
	}
	raw, exists := util.ReadFileSafe(state.Path)
	if !exists {
		return os.Remove(antigravityIDEStateFile()) == nil
	}
	if util.HasJSONCComments(raw) {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	current, has := cfg.Get(antigravityIDEEndpointKey)
	if !has || current != antigravityURL() {
		return false
	}
	if state.HadValue {
		cfg.Set(antigravityIDEEndpointKey, state.Value)
	} else {
		cfg.Delete(antigravityIDEEndpointKey)
	}
	if cfg.Len() == 0 {
		if err := os.Remove(state.Path); err != nil && !os.IsNotExist(err) {
			return false
		}
	} else if err := util.WriteFile(state.Path, util.StringifyJSON(cfg)); err != nil {
		return false
	}
	return os.Remove(antigravityIDEStateFile()) == nil
}

func AntigravityIDEProxyWired() bool {
	state, ok := readAntigravityIDEState()
	if !ok {
		return false
	}
	raw, ok := util.ReadFileSafe(state.Path)
	if !ok {
		return false
	}
	cfg := util.TryParseJsonc(raw)
	value, ok := cfg.Get(antigravityIDEEndpointKey)
	return ok && value == antigravityURL()
}
