package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

type copilotProviderConfig struct {
	ManagedBy string `json:"managed_by"`
	Type      string `json:"type"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
}

func copilotHeadroomCommand() string {
	if util.HeadroomInstalled() {
		return util.HeadroomBin()
	}
	if bin := util.Which("headroom"); bin != "" {
		return bin
	}
	return "headroom"
}

func copilotShimPath() string {
	dir := filepath.Dir(util.ToklessPersistedAbs())
	if dir == "." || dir == "" {
		dirs := util.ExpectedBinDirs()
		if len(dirs) > 0 {
			dir = dirs[0]
		} else {
			dir = filepath.Join(util.Home(), ".local", "bin")
		}
	}
	name := "copilot"
	if util.IsWin {
		name += ".cmd"
	}
	return filepath.Join(dir, name)
}

func copilotStashedPath() string { return copilotShimPath() + ".real" }

func copilotStashMarkerPath() string { return copilotStashedPath() + ".tokless" }

func copilotShimDigestPath() string { return copilotShimPath() + ".digest" }

// CopilotShimDigestPath returns the sidecar used to validate managed shim ownership.
func CopilotShimDigestPath() string { return copilotShimDigestPath() }

func copilotStashMarker(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]) + "\n"
}

func copilotShimText() string {
	tokless := util.ToklessPersistedAbs()
	if util.IsWin {
		return "@echo off\r\nrem tokless:copilot-shim\r\nsetlocal\r\n\"" + tokless + "\" __copilot %*\r\nexit /b %ERRORLEVEL%\r\n"
	}
	return "#!/bin/sh\n# tokless:copilot-shim\nexec " + shellQuote(tokless) + " __copilot \"$@\"\n"
}

func copilotShimOwned(raw string) bool {
	if raw == copilotShimText() {
		return true
	}
	digest, ok := util.ReadFileSafe(copilotShimDigestPath())
	if !ok {
		return false
	}
	return strings.TrimSpace(digest) == strings.TrimSpace(copilotStashMarker(raw))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func copilotConfigPath() string {
	return filepath.Join(util.ToklessDataDir(), "copilot.json")
}

func CopilotProviderConfigPath() string { return copilotConfigPath() }

func saveCopilotProviderConfig() error {
	baseURL := strings.TrimSpace(os.Getenv("COPILOT_PROVIDER_BASE_URL"))
	if baseURL == "" {
		return nil
	}
	cfg := copilotProviderConfig{
		ManagedBy: "tokless",
		Type:      strings.TrimSpace(os.Getenv("COPILOT_PROVIDER_TYPE")),
		BaseURL:   baseURL,
		Model:     strings.TrimSpace(os.Getenv("COPILOT_PROVIDER_MODEL_ID")),
	}
	if cfg.Type == "" {
		cfg.Type = "openai"
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := util.EnsureDir(util.ToklessDataDir()); err != nil {
		return err
	}
	if raw, ok := util.ReadFileSafe(copilotConfigPath()); ok {
		var existing copilotProviderConfig
		if json.Unmarshal([]byte(raw), &existing) != nil || existing.ManagedBy != "tokless" {
			return fmt.Errorf("refusing to overwrite foreign Copilot provider metadata %s", copilotConfigPath())
		}
	}
	return writeCopilotFileMode(copilotConfigPath(), string(b)+"\n", 0o600)
}

func loadCopilotProviderConfig() (copilotProviderConfig, bool) {
	raw, ok := util.ReadFileSafe(copilotConfigPath())
	if !ok {
		return copilotProviderConfig{}, false
	}
	var cfg copilotProviderConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil || cfg.ManagedBy != "tokless" || cfg.BaseURL == "" {
		return copilotProviderConfig{}, false
	}
	return cfg, true
}

func installCopilotShim() (bool, error) {
	path := copilotShimPath()
	want := copilotShimText()
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("refusing to replace non-regular Copilot command %s", path)
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if raw, ok := util.ReadFileSafe(path); ok {
		if raw != want {
			if copilotShimOwned(raw) {
				oldDigest, digestExists := util.ReadFileSafe(copilotShimDigestPath())
				info, _ := os.Lstat(path)
				mode := os.FileMode(0o755)
				if info != nil {
					mode = info.Mode().Perm()
				}
				restore := func() {
					_ = writeCopilotFileMode(path, raw, mode)
					if digestExists {
						_ = writeCopilotFileMode(copilotShimDigestPath(), oldDigest, 0o600)
					} else {
						_ = os.Remove(copilotShimDigestPath())
					}
				}
				if err := writeCopilotFileMode(path, want, mode); err != nil {
					return false, err
				}
				if err := writeCopilotFileMode(copilotShimDigestPath(), copilotStashMarker(want), 0o600); err != nil {
					restore()
					return false, err
				}
				return true, nil
			}
			if util.IsWin {
				return false, fmt.Errorf("refusing to replace non-Tokless Copilot command %s", path)
			}
			stashed := copilotStashedPath()
			if _, err := os.Stat(stashed); err == nil {
				return false, fmt.Errorf("refusing to replace non-Tokless Copilot command %s: backup already exists", path)
			}
			if err := os.Rename(path, stashed); err != nil {
				return false, fmt.Errorf("stash real Copilot command: %w", err)
			}
			if err := writeCopilotFileMode(copilotStashMarkerPath(), copilotStashMarker(raw), 0o600); err != nil {
				_ = os.Rename(stashed, path)
				return false, fmt.Errorf("record real Copilot command: %w", err)
			}
			if err := writeCopilotFileMode(copilotShimDigestPath(), copilotStashMarker(want), 0o600); err != nil {
				_ = os.Rename(stashed, path)
				_ = os.Remove(copilotStashMarkerPath())
				return false, fmt.Errorf("record Copilot shim: %w", err)
			}
			if err := writeCopilotFileMode(path, want, 0o755); err != nil {
				_ = os.Rename(stashed, path)
				_ = os.Remove(copilotStashMarkerPath())
				_ = os.Remove(copilotShimDigestPath())
				return false, err
			}
			return true, nil
		}
		return false, nil
	}
	if err := util.EnsureDir(filepath.Dir(path)); err != nil {
		return false, err
	}
	mode := os.FileMode(0o755)
	if util.IsWin {
		mode = 0o644
	}
	if err := writeCopilotFileMode(path, want, mode); err != nil {
		return false, err
	}
	if err := writeCopilotFileMode(copilotShimDigestPath(), copilotStashMarker(want), 0o600); err != nil {
		_ = os.Remove(path)
		return false, err
	}
	return true, nil
}

func removeCopilotShim() bool {
	path := copilotShimPath()
	raw, ok := util.ReadFileSafe(path)
	if !ok || !copilotShimOwned(raw) {
		return false
	}
	shimInfo, _ := os.Lstat(path)
	shimMode := os.FileMode(0o755)
	if shimInfo != nil {
		shimMode = shimInfo.Mode().Perm()
	}
	if !util.IsWin {
		stashed := copilotStashedPath()
		if _, err := os.Stat(stashed); err == nil {
			marker, markerOK := util.ReadFileSafe(copilotStashMarkerPath())
			real, realOK := util.ReadFileSafe(stashed)
			info, statErr := os.Lstat(stashed)
			if !markerOK || !realOK || statErr != nil || !info.Mode().IsRegular() || strings.TrimSpace(marker) != strings.TrimSpace(copilotStashMarker(real)) {
				return false
			}
			markerRaw, markerExists := marker, markerOK
			digestRaw, digestExists := util.ReadFileSafe(copilotShimDigestPath())
			tmp := path + ".removing"
			if err := os.Rename(path, tmp); err != nil {
				return false
			}
			if err := os.Rename(stashed, path); err != nil {
				_ = os.Rename(tmp, path)
				return false
			}
			if err := os.Remove(tmp); err != nil {
				_ = os.Rename(path, stashed)
				_ = os.Rename(tmp, path)
				return false
			}
			if err := removeCopilotSidecars(); err != nil {
				var rollbackErrs []error
				if renameErr := os.Rename(path, stashed); renameErr != nil {
					rollbackErrs = append(rollbackErrs, renameErr)
				}
				if writeErr := writeCopilotFileMode(path, raw, shimMode); writeErr != nil {
					rollbackErrs = append(rollbackErrs, writeErr)
				}
				if markerExists {
					if writeErr := writeCopilotFileMode(copilotStashMarkerPath(), markerRaw, 0o600); writeErr != nil {
						rollbackErrs = append(rollbackErrs, writeErr)
					}
				}
				if digestExists {
					if writeErr := writeCopilotFileMode(copilotShimDigestPath(), digestRaw, 0o600); writeErr != nil {
						rollbackErrs = append(rollbackErrs, writeErr)
					}
				}
				if rollbackErr := errors.Join(rollbackErrs...); rollbackErr != nil {
					util.L.Err("Copilot shim rollback failed: " + rollbackErr.Error())
				}
				return false
			}
			return true
		}
	}
	if err := os.Remove(path); err != nil {
		return false
	}
	if err := os.Remove(copilotShimDigestPath()); err != nil && !os.IsNotExist(err) {
		_ = writeCopilotFileMode(path, raw, shimMode)
		return false
	}
	return true
}

func removeCopilotSidecars() error {
	if err := os.Remove(copilotStashMarkerPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(copilotShimDigestPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeCopilotSidecar(path string) bool {
	err := os.Remove(path)
	return err == nil || os.IsNotExist(err)
}

func copilotShimInstalled() bool {
	raw, ok := util.ReadFileSafe(copilotShimPath())
	return ok && copilotShimOwned(raw)
}

func envValue(env map[string]string, key string) string {
	return strings.TrimSpace(env[key])
}

func copilotCommandEnv() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	if cfg, ok := loadCopilotProviderConfig(); ok {
		if envValue(env, "COPILOT_PROVIDER_BASE_URL") == "" {
			env["COPILOT_PROVIDER_BASE_URL"] = cfg.BaseURL
		}
		if envValue(env, "COPILOT_PROVIDER_TYPE") == "" {
			env["COPILOT_PROVIDER_TYPE"] = cfg.Type
		}
		if envValue(env, "COPILOT_PROVIDER_MODEL_ID") == "" {
			env["COPILOT_PROVIDER_MODEL_ID"] = cfg.Model
		}
	}
	return env
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

func copilotCommandAlias(realCopilot string) (string, func(), error) {
	base := strings.ToLower(filepath.Base(realCopilot))
	if base == "copilot" || (util.IsWin && base == "copilot.exe") {
		return realCopilot, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "tokless-copilot-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	info, err := os.Stat(realCopilot)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	raw, err := os.ReadFile(realCopilot)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	name := "copilot"
	if util.IsWin {
		ext := strings.ToLower(filepath.Ext(realCopilot))
		if ext == ".exe" || ext == ".cmd" || ext == ".bat" {
			name += ext
		}
	}
	alias := filepath.Join(dir, name)
	if err := os.WriteFile(alias, raw, info.Mode().Perm()); err != nil {
		cleanup()
		return "", nil, err
	}
	return alias, cleanup, nil
}

func realCopilotPath(shim string, env map[string]string) string {
	name := "copilot"
	if util.IsWin {
		name += ".exe"
	}
	shimDir := filepath.Clean(filepath.Dir(shim))
	isRegular := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Mode().IsRegular()
	}
	if !util.IsWin {
		if path := copilotStashedPath(); filepath.Clean(filepath.Dir(path)) == shimDir {
			info, statErr := os.Lstat(path)
			real, realOK := util.ReadFileSafe(path)
			marker, markerOK := util.ReadFileSafe(copilotStashMarkerPath())
			if statErr == nil && info.Mode().IsRegular() && realOK && markerOK && strings.TrimSpace(marker) == strings.TrimSpace(copilotStashMarker(real)) {
				return path
			}
		}
	} else if path := filepath.Join(shimDir, name); filepath.Clean(path) != filepath.Clean(shim) {
		if isRegular(path) {
			return path
		}
	}
	for _, dir := range strings.Split(env["PATH"], string(os.PathListSeparator)) {
		if dir == "" || filepath.Clean(dir) == shimDir {
			continue
		}
		path := filepath.Join(dir, name)
		if isRegular(path) {
			return path
		}
		if util.IsWin {
			for _, ext := range []string{".cmd", ".bat"} {
				path = filepath.Join(dir, "copilot"+ext)
				if isRegular(path) {
					return path
				}
			}
		}
	}
	return ""
}

func RunCopilotCLI(args []string) int {
	env := copilotCommandEnv()
	shim := copilotShimPath()
	realCopilot := realCopilotPath(shim, env)
	if realCopilot == "" {
		fmt.Fprintln(os.Stderr, "tokless: real Copilot CLI not found on PATH")
		return 127
	}
	headroom := copilotHeadroomCommand()
	if headroom == "" {
		fmt.Fprintln(os.Stderr, "tokless: Headroom binary not found; run tokless first")
		return 127
	}
	command, cleanup, err := copilotCommandAlias(realCopilot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tokless: prepare real Copilot CLI: %v\n", err)
		return 1
	}
	defer cleanup()

	// Headroom resolves `copilot` through PATH. Hide Tokless shim so it reaches
	// the actual CLI instead of recursively invoking this dispatcher.
	var pathParts []string
	for _, dir := range strings.Split(env["PATH"], string(os.PathListSeparator)) {
		if dir != "" && filepath.Clean(dir) != filepath.Clean(filepath.Dir(shim)) {
			pathParts = append(pathParts, dir)
		}
	}
	env["PATH"] = strings.Join(pathParts, string(os.PathListSeparator))
	if command != realCopilot {
		pathParts = append([]string{filepath.Dir(command)}, pathParts...)
		env["PATH"] = strings.Join(pathParts, string(os.PathListSeparator))
	}
	env["HEADROOM_WRAP_SILENT"] = "1"
	env["COPILOT_PROVIDER_BASE_URL"] = envValue(env, "COPILOT_PROVIDER_BASE_URL")
	delete(env, "GROK_MODELS_BASE_URL")

	providerKey := envValue(env, "COPILOT_PROVIDER_API_KEY")
	bearer := envValue(env, "COPILOT_PROVIDER_BEARER_TOKEN")
	headers := envValue(env, "COPILOT_PROVIDER_HEADERS")
	baseURL := envValue(env, "COPILOT_PROVIDER_BASE_URL")
	providerType := envValue(env, "COPILOT_PROVIDER_TYPE")
	if providerType == "" {
		providerType = "openai"
	}
	wrapArgs := []string{"wrap", "copilot", "--port", strconv.Itoa(CopilotCLIProxyPort())}
	if (providerKey != "" || bearer != "" || headers != "") && (baseURL != "" || headers != "") {
		if providerType != "anthropic" && providerType != "openai" {
			fmt.Fprintf(os.Stderr, "Unsupported COPILOT_PROVIDER_TYPE: %s (expected anthropic or openai)\n", providerType)
			return 2
		}
		if model := envValue(env, "COPILOT_MODEL"); model == "" {
			env["COPILOT_MODEL"] = envValue(env, "COPILOT_PROVIDER_MODEL_ID")
		}
		wrapArgs = append(wrapArgs, "--provider-type", providerType)
		if providerType == "anthropic" {
			delete(env, "OPENAI_TARGET_API_URL")
			env["ANTHROPIC_TARGET_API_URL"] = baseURL
		} else {
			delete(env, "ANTHROPIC_TARGET_API_URL")
			env["OPENAI_TARGET_API_URL"] = baseURL
		}
	} else {
		delete(env, "OPENAI_TARGET_API_URL")
		delete(env, "ANTHROPIC_TARGET_API_URL")
	}
	wrapArgs = append(wrapArgs, "--")
	wrapArgs = append(wrapArgs, args...)

	cmd := exec.Command(headroom, wrapArgs...)
	cmd.Env = envList(env)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
