package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

const (
	rtkHooksTable      = "hooks"
	rtkSuppressKey     = "suppress_hook_warning"
	rtkSuppressStateFN = "rtk-suppress-hook-warning.json"
)

type rtkSuppressState struct {
	Path         string `json:"path"`
	HadValue     bool   `json:"hadValue"`
	Value        string `json:"value,omitempty"`
	TableCreated bool   `json:"tableCreated,omitempty"`
	FileCreated  bool   `json:"fileCreated,omitempty"`
}

func rtkConfigFile() string {
	return filepath.Join(util.UserConfigDir(), "rtk", "config.toml")
}

func rtkSuppressStateFile() string {
	return filepath.Join(util.ToklessDataDir(), rtkSuppressStateFN)
}

func validRtkConfigPath(p string) bool {
	return p != "" && filepath.IsAbs(p) &&
		filepath.Base(p) == "config.toml" && filepath.Base(filepath.Dir(p)) == "rtk"
}

func readRtkSuppressState() (rtkSuppressState, bool) {
	var st rtkSuppressState
	raw, ok := util.ReadFileSafe(rtkSuppressStateFile())
	if !ok || json.Unmarshal([]byte(raw), &st) != nil {
		return st, false
	}
	if !validRtkConfigPath(st.Path) {
		return st, false
	}
	return st, true
}

func writeRtkSuppressState(st rtkSuppressState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return util.WriteFile(rtkSuppressStateFile(), string(b))
}

// ensureRtkHookWarningSuppressed sets the flag and records the prior value.
func ensureRtkHookWarningSuppressed() (bool, error) {
	if st, ok := readRtkSuppressState(); ok {
		if util.Exists(st.Path) {
			return false, nil
		}
		_ = os.Remove(rtkSuppressStateFile())
	}
	path := rtkConfigFile()
	raw, existed := util.ReadFileSafe(path)
	if !existed {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			util.L.Sub("rtk config: " + path + " could not be read; left unchanged")
			return false, nil
		}
	}
	current, has := util.TomlBlockRawField(raw, rtkHooksTable, rtkSuppressKey)
	if has && current != "false" {
		return false, nil
	}
	out, tableCreated, ok := util.SetTomlBlockKey(raw, rtkHooksTable, rtkSuppressKey, "true")
	if !ok {
		util.L.Sub("rtk config: cannot set " + rtkSuppressKey + " in " + path + " (unsupported TOML shape)")
		return false, nil
	}
	st := rtkSuppressState{Path: path, HadValue: has, Value: current, TableCreated: tableCreated, FileCreated: !existed}
	if err := writeRtkSuppressState(st); err != nil {
		return false, err
	}
	if err := util.WriteFile(path, out); err != nil {
		_ = os.Remove(rtkSuppressStateFile())
		return false, err
	}
	return true, nil
}

// cleanupRtkHookWarningSuppression restores the config after the last hook.
func cleanupRtkHookWarningSuppression() {
	st, ok := readRtkSuppressState()
	if !ok || rtkHookWired() {
		return
	}
	if raw, exists := util.ReadFileSafe(st.Path); exists {
		current, has := util.TomlBlockRawField(raw, rtkHooksTable, rtkSuppressKey)
		switch {
		case !has || current != "true":
		case st.HadValue:
			out, _, ok := util.SetTomlBlockKey(raw, rtkHooksTable, rtkSuppressKey, st.Value)
			if !ok {
				util.L.Sub("rtk config: left suppress_hook_warning unchanged (file shape not editable)")
				return
			}
			if err := util.WriteFile(st.Path, out); err != nil {
				util.L.Sub("rtk config: " + err.Error())
				return
			}
		default:
			out, removed, empty, ok := util.RemoveTomlBlockKey(raw, rtkHooksTable, rtkSuppressKey)
			if !ok {
				util.L.Sub("rtk config: left suppress_hook_warning unchanged (file shape not editable)")
				return
			}
			if removed {
				if st.TableCreated && empty {
					if out2, ok := util.RemoveTomlBlock(out, rtkHooksTable); ok {
						out = out2
					}
				}
				if err := util.WriteFile(st.Path, out); err != nil {
					util.L.Sub("rtk config: " + err.Error())
					return
				}
			}
		}
		if st.FileCreated {
			if cur, ok := util.ReadFileSafe(st.Path); ok && strings.TrimSpace(cur) == "" {
				_ = os.Remove(st.Path)
			}
		}
	}
	_ = os.Remove(rtkSuppressStateFile())
}

// rtkHookWired reports whether any agent still routes commands through rtk.
var rtkHookWired = func() bool {
	for _, verify := range rtk.VerifyFor {
		if verify == nil {
			continue
		}
		if wired := verify(); wired != nil && *wired {
			return true
		}
	}
	return false
}

var rtkSuppressHintShown bool

func rtkSuppressDryRunHint() {
	if rtkSuppressHintShown {
		return
	}
	raw, _ := util.ReadFileSafe(rtkConfigFile())
	current, has := util.TomlBlockRawField(raw, rtkHooksTable, rtkSuppressKey)
	if has && current != "false" {
		return
	}
	if _, _, ok := util.SetTomlBlockKey(raw, rtkHooksTable, rtkSuppressKey, "true"); !ok {
		return
	}
	rtkSuppressHintShown = true
	util.L.Sub("[dry-run] would set hooks.suppress_hook_warning = true in " + rtkConfigFile())
}

// wrapRtkHookWarning keeps the warning in sync on every lifecycle path.
func wrapRtkHookWarning(t *core.ToolManifest) {
	for id, fn := range t.WireFor {
		wire := fn
		t.WireFor[id] = func(opts core.RunOpts) (bool, error) {
			ok, err := wire(opts)
			if !ok || err != nil {
				return ok, err
			}
			if opts.DryRun {
				rtkSuppressDryRunHint()
				return ok, nil
			}
			if _, werr := ensureRtkHookWarningSuppressed(); werr != nil {
				util.L.Sub("rtk config: " + werr.Error())
			}
			return ok, nil
		}
	}
	for id, fn := range t.UnwireFor {
		unwire := fn
		t.UnwireFor[id] = func(opts core.RunOpts) (bool, error) {
			ok, err := unwire(opts)
			if !opts.DryRun && err == nil {
				cleanupRtkHookWarningSuppression()
			}
			return ok, err
		}
	}
}

func init() { wrapRtkHookWarning(rtk) }
