package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/HoangP8/tokless/internal/util"
)

const (
	defaultCopilotProxyPort    = 8789 // VS Code OAuth private backend.
	defaultCopilotCLIProxyPort = 8790 // Copilot CLI wrap private backend.
)

func copilotVSCodeSettingsFile() string {
	home := util.Home()
	if util.IsWin {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "Code", "User", "settings.json")
		}
		return filepath.Join(home, "AppData", "Roaming", "Code", "User", "settings.json")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	return filepath.Join(config, "Code", "User", "settings.json")
}

// CopilotProxyPort is the dedicated VS Code OAuth proxy port.
func CopilotProxyPort() int {
	if raw := strings.TrimSpace(os.Getenv("TOKLESS_COPILOT_PROXY_PORT")); raw != "" {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	return defaultCopilotProxyPort
}

// CopilotCLIProxyPort keeps CLI BYOK and VS Code OAuth proxy state isolated.
func CopilotCLIProxyPort() int {
	if raw := strings.TrimSpace(os.Getenv("TOKLESS_COPILOT_CLI_PROXY_PORT")); raw != "" {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	return defaultCopilotCLIProxyPort
}

func ValidateCopilotProxyPorts() error {
	if CopilotProxyPort() == CopilotCLIProxyPort() {
		return fmt.Errorf("Copilot CLI and VS Code proxy ports must differ (both are %d)", CopilotProxyPort())
	}
	return nil
}

func copilotNormalizeFenceBlock(block string) string {
	block = strings.ReplaceAll(block, "\r\n", "\n")
	return strings.TrimSuffix(block, "\n") + "\n"
}

const (
	copilotVSCodeMarkerHead = "// --- tokless:headroom copilot begin"
	copilotVSCodeMarkerFoot = "// --- tokless:headroom copilot end"
	copilotVSCodeProxyKey   = "github.copilot.advanced.debug.overrideProxyUrl"
	copilotVSCodeAuthKey    = "github.copilot.advanced.debug.overrideAuthType"
	copilotVSCodeCAPIKey    = "github.copilot.advanced.debug.overrideCapiUrl"
)

func copilotVSCodeProxyURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(CopilotProxyPort())
}

var copilotProjectWriteMu sync.Mutex
var copilotProxyOperationMu sync.Mutex

func writeCopilotVSCodeFile(path, content string) error {
	copilotProjectWriteMu.Lock()
	defer copilotProjectWriteMu.Unlock()
	return writeCopilotVSCodeFileLocked(path, content)
}

func writeCopilotVSCodeFileLocked(path, content string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		current, err := filepath.EvalSymlinks(path)
		if err != nil || current != target {
			return fmt.Errorf("Copilot VS Code symlink changed during write %s", path)
		}
		return writeCopilotFile(target, content)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return writeCopilotFile(path, content)
}

func clearCopilotProjectFile(path string) error {
	copilotProjectWriteMu.Lock()
	defer copilotProjectWriteMu.Unlock()
	return clearCopilotProjectFileLocked(path)
}

func clearCopilotProjectFileLocked(path string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		current, err := filepath.EvalSymlinks(path)
		if err != nil || current != target {
			return fmt.Errorf("Copilot project symlink changed during cleanup %s", path)
		}
		return writeCopilotFile(target, "")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ClearCopilotProjectFile(path string) error {
	return clearCopilotProjectFile(path)
}

func copilotVSCodeSpan(raw string) (start, end int, ok bool) {
	start = strings.Index(raw, copilotVSCodeMarkerHead)
	if start < 0 || strings.Count(raw, copilotVSCodeMarkerHead) != 1 || strings.Count(raw, copilotVSCodeMarkerFoot) != 1 {
		return 0, 0, false
	}
	endMarker := strings.Index(raw[start:], copilotVSCodeMarkerFoot)
	if endMarker < 0 {
		return 0, 0, false
	}
	end = start + endMarker + len(copilotVSCodeMarkerFoot)
	if lineStart := strings.LastIndex(raw[:start], "\n"); lineStart >= 0 {
		start = lineStart + 1
	}
	if lineEnd := strings.Index(raw[end:], "\n"); lineEnd >= 0 {
		end += lineEnd + 1
	}
	return start, end, true
}

func copilotVSCodeBlock(commaAdded bool) string {
	marker := copilotVSCodeMarkerHead
	if commaAdded {
		marker += " (comma-added)"
	}
	return "\t" + marker + "\n" +
		"\t\"" + copilotVSCodeProxyKey + "\": \"" + copilotVSCodeProxyURL() + "\",\n" +
		"\t\"" + copilotVSCodeCAPIKey + "\": \"" + copilotVSCodeProxyURL() + "\"\n" +
		"\t" + copilotVSCodeMarkerFoot
}

func jsoncRootObjectEnd(raw string) int {
	depth := 0
	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inLineComment {
			if c == '\n' || c == '\r' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if c == '*' && i+1 < len(raw) && raw[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '/':
			if i+1 < len(raw) && raw[i+1] == '/' {
				inLineComment = true
				i++
			} else if i+1 < len(raw) && raw[i+1] == '*' {
				inBlockComment = true
				i++
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func jsoncLastTokenEnd(raw string, limit int) int {
	last := 0
	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false
	for i := 0; i < limit; i++ {
		c := raw[i]
		if inLineComment {
			if c == '\n' || c == '\r' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if c == '*' && i+1 < limit && raw[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
				last = i + 1
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '/':
			if i+1 < limit && raw[i+1] == '/' {
				inLineComment = true
				i++
			} else if i+1 < limit && raw[i+1] == '*' {
				inBlockComment = true
				i++
			} else {
				last = i + 1
			}
		default:
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				last = i + 1
			}
		}
	}
	return last
}

func copilotVSCodeCurrentManaged(raw string) bool {
	start, end, ok := copilotVSCodeSpan(raw)
	if !ok {
		return false
	}
	block := copilotNormalizeFenceBlock(raw[start:end])
	return block == copilotVSCodeBlock(false)+"\n" || block == copilotVSCodeBlock(true)+"\n"
}

func copilotVSCodeLegacyManaged(raw string) bool {
	start, end, ok := copilotVSCodeSpan(raw)
	if !ok {
		return false
	}
	block := copilotNormalizeFenceBlock(raw[start:end])
	return block == copilotVSCodeLegacyBlock(false)+"\n" || block == copilotVSCodeLegacyBlock(true)+"\n"
}

func copilotVSCodeLegacyBlock(commaAdded bool) string {
	marker := copilotVSCodeMarkerHead
	if commaAdded {
		marker += " (comma-added)"
	}
	return "\t" + marker + "\n" +
		"\t\"" + copilotVSCodeProxyKey + "\": \"" + copilotVSCodeProxyURL() + "\",\n" +
		"\t\"" + copilotVSCodeAuthKey + "\": \"token\"\n" +
		"\t" + copilotVSCodeMarkerFoot
}

func ConfigureCopilotCLIProxy() (bool, string) {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	changed, file, _ := configureCopilotCLIProxySafe()
	return changed, file
}

func configureCopilotCLIProxySafe() (bool, string, error) {
	shim := copilotShimPath()
	changed, err := installCopilotShim()
	if err != nil {
		return false, shim, err
	}
	if err := saveCopilotProviderConfig(); err != nil {
		if changed {
			_ = removeCopilotShim()
		}
		return false, shim, err
	}
	return changed, shim, nil
}

// ConfigureCopilotVSCodeProxy configures only VS Code's managed settings.
func ConfigureCopilotVSCodeProxy() (changed, ok bool) {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	return configureCopilotVSCode()
}

// ConfigureCopilotProxy configures the documented Copilot CLI BYOK path.
func ConfigureCopilotProxy() (bool, string) {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	changed, file, _ := configureCopilotCLIProxySafe()
	return changed, file
}

// ConfigureCopilotAllProxy configures both the CLI launcher and VS Code
// settings, rolling back both surfaces if either one cannot be managed.
func ConfigureCopilotAllProxy() bool {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	cliRequired := CopilotCLIProxyApplicable()
	vsCodeRequired := CopilotVSCodeProxyApplicable()
	_, _ = configureCopilotProxyLocked()
	return (!cliRequired || CopilotCLIProxyWired()) && (!vsCodeRequired || CopilotVSCodeProxyWired())
}

func configureCopilotProxyLocked() (bool, string) {
	snapshot, err := snapshotCopilotProxyFiles()
	if err != nil {
		util.L.Err("copilot proxy snapshot: " + err.Error())
		return false, copilotShimPath()
	}
	changed := false
	file := copilotShimPath()
	if CopilotCLIProxyApplicable() {
		cliChanged, cliFile, cliErr := configureCopilotCLIProxySafe()
		file = cliFile
		if cliErr != nil {
			if rollbackErr := restoreCopilotProxyFiles(snapshot); rollbackErr != nil {
				util.L.Err("copilot proxy rollback failed: " + rollbackErr.Error())
			}
			return false, file
		}
		changed = changed || cliChanged
		if !cliChanged && !CopilotCLIProxyWired() {
			if rollbackErr := restoreCopilotProxyFiles(snapshot); rollbackErr != nil {
				util.L.Err("copilot proxy rollback failed: " + rollbackErr.Error())
			}
			return false, file
		}
	}
	if CopilotVSCodeProxyApplicable() {
		vsChanged, vsOK := configureCopilotVSCode()
		if !vsOK {
			if err := restoreCopilotProxyFiles(snapshot); err != nil {
				util.L.Err("copilot proxy rollback failed: " + err.Error())
			}
			return false, file
		}
		changed = changed || vsChanged
	}
	return changed || CopilotProxyWired(), file
}

// CopilotCLIProxyApplicable reports whether the Copilot CLI surface exists.
func CopilotCLIProxyApplicable() bool {
	return CopilotCLIProxyWired() || util.Which("copilot") != ""
}

// CopilotVSCodeProxyApplicable reports whether the Copilot VS Code surface exists.
func CopilotVSCodeProxyApplicable() bool {
	return CopilotVSCodeProxyWired() || vscodeExtensionInstalled("github.copilot-chat")
}

type copilotProxyFileSnapshot struct {
	path    string
	raw     string
	exists  bool
	mode    os.FileMode
	symlink string
}

type CopilotProxySnapshot struct {
	files []copilotProxyFileSnapshot
}

func SnapshotCopilotProxy() (CopilotProxySnapshot, error) {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	return snapshotCopilotProxyLocked()
}

func snapshotCopilotProxyLocked() (CopilotProxySnapshot, error) {
	files, err := snapshotCopilotProxyFiles()
	return CopilotProxySnapshot{files: files}, err
}

func (snapshot CopilotProxySnapshot) Restore() error {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	return restoreCopilotProxyFiles(snapshot.files)
}

func snapshotCopilotProxyFiles() ([]copilotProxyFileSnapshot, error) {
	paths := []string{copilotShimPath(), copilotShimDigestPath(), copilotStashedPath(), copilotStashMarkerPath(), copilotConfigPath(), copilotVSCodeSettingsFile()}
	result := make([]copilotProxyFileSnapshot, 0, len(paths))
	for _, path := range paths {
		s := copilotProxyFileSnapshot{path: path, mode: 0o644}
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if path != copilotVSCodeSettingsFile() && path != copilotIdeHooksFile("tokless-codegraph-index.json") {
					return nil, fmt.Errorf("refusing Copilot proxy snapshot path %s", path)
				}
				info, err = os.Stat(path)
				if err != nil {
					return nil, err
				}
				s.symlink, err = os.Readlink(path)
				if err != nil {
					return nil, err
				}
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("refusing Copilot proxy snapshot path %s", path)
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			s.raw, s.exists = string(contents), true
			s.mode = info.Mode().Perm()
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func restoreCopilotProxyFiles(snapshots []copilotProxyFileSnapshot) error {
	var errs []error
	for _, snapshot := range snapshots {
		if snapshot.exists {
			if err := rejectCopilotProxySymlinkPath(snapshot.path, snapshot.symlink != ""); err != nil {
				errs = append(errs, err)
				continue
			}
			if snapshot.symlink != "" {
				link, err := os.Readlink(snapshot.path)
				if err != nil || link != snapshot.symlink {
					errs = append(errs, fmt.Errorf("Copilot proxy symlink changed during rollback %s", snapshot.path))
					continue
				}
			} else if info, err := os.Lstat(snapshot.path); err == nil && info.Mode()&os.ModeSymlink != 0 {
				errs = append(errs, fmt.Errorf("Copilot proxy path became symlink during rollback %s", snapshot.path))
				continue
			}
			target := snapshot.path
			if snapshot.symlink != "" {
				target = snapshot.symlink
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(snapshot.path), target)
				}
			}
			if err := writeCopilotFileMode(target, snapshot.raw, snapshot.mode); err != nil {
				errs = append(errs, err)
			} else if err := os.Chmod(target, snapshot.mode); err != nil {
				errs = append(errs, err)
			}
		} else if info, err := os.Lstat(snapshot.path); err == nil {
			if !info.Mode().IsRegular() || !copilotProxyRollbackOwned(snapshot.path) {
				errs = append(errs, fmt.Errorf("Copilot proxy path changed during rollback %s", snapshot.path))
				continue
			}
			if err := os.Remove(snapshot.path); err != nil {
				errs = append(errs, err)
			}
		} else if !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func copilotProxyRollbackOwned(path string) bool {
	raw, ok := util.ReadFileSafe(path)
	if !ok {
		return false
	}
	switch path {
	case copilotShimPath():
		return copilotShimOwned(raw)
	case copilotShimDigestPath():
		return strings.TrimSpace(raw) == strings.TrimSpace(copilotStashMarker(copilotShimText()))
	case copilotStashedPath():
		marker, markerOK := util.ReadFileSafe(copilotStashMarkerPath())
		return markerOK && strings.TrimSpace(marker) == strings.TrimSpace(copilotStashMarker(raw))
	case copilotStashMarkerPath():
		stashed, stashedOK := util.ReadFileSafe(copilotStashedPath())
		return stashedOK && strings.TrimSpace(raw) == strings.TrimSpace(copilotStashMarker(stashed))
	case copilotConfigPath():
		var cfg copilotProviderConfig
		return json.Unmarshal([]byte(raw), &cfg) == nil && cfg.ManagedBy == "tokless"
	case copilotVSCodeSettingsFile():
		return copilotVSCodeCurrentManaged(raw) || copilotVSCodeLegacyManaged(raw)
	}
	return false
}

func rejectCopilotProxySymlinkPath(path string, allowLeaf bool) error {
	clean := filepath.Clean(path)
	for current := clean; current != filepath.Dir(current); current = filepath.Dir(current) {
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if allowLeaf && current == clean {
				continue
			}
			return fmt.Errorf("refusing Copilot proxy rollback symlink %s", current)
		}
	}
	return nil
}

// RemoveCopilotCLIProxy removes only Tokless's shell launcher.
func RemoveCopilotCLIProxy() bool {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	return removeCopilotShim()
}

// RemoveCopilotVSCodeProxy removes only Tokless's VS Code settings block.
func RemoveCopilotVSCodeProxy() (bool, error) {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	return removeCopilotVSCode()
}

func RemoveCopilotProxy() bool {
	copilotProxyOperationMu.Lock()
	defer copilotProxyOperationMu.Unlock()
	return removeCopilotProxyLocked()
}

func removeCopilotProxyLocked() bool {
	snapshot, err := snapshotCopilotProxyLocked()
	if err != nil {
		util.L.Err("copilot proxy snapshot: " + err.Error())
		return false
	}
	cliWiredBefore := CopilotCLIProxyWired()
	vsCodeWiredBefore := CopilotVSCodeProxyWired()
	removedCLI := removeCopilotShim()
	removedVSCode, err := removeCopilotVSCode()
	if err != nil || (cliWiredBefore && (!removedCLI || CopilotCLIProxyWired())) || (vsCodeWiredBefore && (!removedVSCode || CopilotVSCodeProxyWired())) {
		if rollbackErr := restoreCopilotProxyFiles(snapshot.files); rollbackErr != nil {
			util.L.Err("copilot proxy rollback failed: " + rollbackErr.Error())
		}
		return false
	}
	if raw, ok := util.ReadFileSafe(copilotConfigPath()); ok {
		var cfg copilotProviderConfig
		if json.Unmarshal([]byte(raw), &cfg) == nil && cfg.ManagedBy == "tokless" {
			if err := os.Remove(copilotConfigPath()); err != nil {
				if rollbackErr := restoreCopilotProxyFiles(snapshot.files); rollbackErr != nil {
					util.L.Err("copilot proxy rollback failed: " + rollbackErr.Error())
				}
				return false
			}
		}
	}
	return removedCLI || removedVSCode || cliWiredBefore || vsCodeWiredBefore
}

func CopilotProxyWired() bool {
	return CopilotCLIProxyWired() || CopilotVSCodeProxyWired()
}

// CopilotCLIProxyWired reports whether the shell launcher is Tokless-owned.
func CopilotCLIProxyWired() bool {
	return copilotShimInstalled()
}

// CopilotVSCodeProxyWired reports whether VS Code has Tokless-owned settings.
func CopilotVSCodeProxyWired() bool {
	settings, settingsOK := util.ReadFileSafe(copilotVSCodeSettingsFile())
	return settingsOK && (copilotVSCodeCurrentManaged(settings) || copilotVSCodeLegacyManaged(settings))
}

func detectCopilotProxy(cap ProxyCapability) ProxyDetection {
	cliState, cliDetail := detectCopilotCLIProxy()
	vsCodeState, vsCodeDetail := detectCopilotVSCodeProxy()
	for _, surface := range []struct {
		name   string
		state  ProxyConfigState
		detail string
	}{
		{"CLI", cliState, cliDetail},
		{"VS Code", vsCodeState, vsCodeDetail},
	} {
		switch surface.state {
		case ProxyStateConflict, ProxyStateForeignBYOK, ProxyStateUnreadable:
			return proxyDetection(cap.ID, surface.name+": "+surface.detail, surface.state)
		}
	}
	if cliState == ProxyStateManaged || vsCodeState == ProxyStateManaged {
		if cliState == ProxyStateUnconfigured || vsCodeState == ProxyStateUnconfigured {
			return proxyDetection(cap.ID, "one Copilot surface managed; another unconfigured", ProxyStateUnconfigured)
		}
		if cliState == ProxyStateManaged && vsCodeState == ProxyStateAbsent {
			return proxyDetection(cap.ID, cliDetail, ProxyStateManaged)
		}
		if vsCodeState == ProxyStateManaged && cliState == ProxyStateAbsent {
			return proxyDetection(cap.ID, vsCodeDetail, ProxyStateManaged)
		}
		return proxyDetection(cap.ID, "managed Copilot surfaces", ProxyStateManaged)
	}
	if cliState == ProxyStateAbsent && vsCodeState == ProxyStateAbsent {
		return proxyDetection(cap.ID, "Copilot surfaces absent", ProxyStateAbsent)
	}
	return proxyDetection(cap.ID, "Copilot surfaces not configured", ProxyStateUnconfigured)
}

func detectCopilotCLIProxy() (ProxyConfigState, string) {
	if CopilotCLIProxyWired() {
		return ProxyStateManaged, "managed executable launcher; dedicated Headroom wrapper port " + strconv.Itoa(CopilotCLIProxyPort())
	}
	if raw, ok := util.ReadFileSafe(copilotConfigPath()); ok {
		var cfg copilotProviderConfig
		if json.Unmarshal([]byte(raw), &cfg) != nil || cfg.ManagedBy != "tokless" {
			return ProxyStateConflict, "provider metadata is foreign or unreadable"
		}
	}
	if util.Which("copilot") != "" {
		return ProxyStateUnconfigured, "managed Copilot CLI launcher not configured"
	}
	return ProxyStateAbsent, "Copilot CLI absent"
}

func detectCopilotVSCodeProxy() (ProxyConfigState, string) {
	if CopilotVSCodeProxyWired() {
		return ProxyStateManaged, "managed VS Code settings; private Headroom wrapper port " + strconv.Itoa(CopilotProxyPort())
	}
	if !vscodeExtensionInstalled("github.copilot-chat") {
		return ProxyStateAbsent, "Copilot Chat extension absent"
	}
	raw, err := os.ReadFile(copilotVSCodeSettingsFile())
	if os.IsNotExist(err) {
		return ProxyStateUnconfigured, "VS Code settings not configured"
	}
	if err != nil {
		return ProxyStateUnreadable, "VS Code settings unreadable"
	}
	text := string(raw)
	if strings.Contains(text, copilotVSCodeMarkerHead) || strings.Contains(text, copilotVSCodeMarkerFoot) {
		if !copilotVSCodeCurrentManaged(text) && !copilotVSCodeLegacyManaged(text) {
			return ProxyStateConflict, "Tokless settings block differs"
		}
	}
	for _, key := range []string{copilotVSCodeProxyKey, copilotVSCodeAuthKey, copilotVSCodeCAPIKey} {
		if strings.Contains(text, `"`+key+`"`) {
			return ProxyStateConflict, "reserved setting differs"
		}
	}
	return ProxyStateUnconfigured, "VS Code settings not configured"
}

func configureCopilotVSCode() (changed, ok bool) {
	changed, err := configureCopilotVSCodeSettings()
	if err != nil {
		util.L.Sub("copilot VS Code settings: " + err.Error())
		return false, false
	}
	return changed, true
}

func configureCopilotVSCodeSettings() (bool, error) {
	copilotProjectWriteMu.Lock()
	defer copilotProjectWriteMu.Unlock()
	return configureCopilotVSCodeSettingsLocked()
}

func configureCopilotVSCodeSettingsLocked() (bool, error) {
	if !vscodeExtensionInstalled("github.copilot-chat") {
		return false, nil
	}
	path := copilotVSCodeSettingsFile()
	contents, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	raw := string(contents)
	if os.IsNotExist(err) {
		raw = "{}\n"
	}
	if util.TryParseJsonc(raw) == nil {
		return false, os.ErrInvalid
	}
	if strings.Contains(raw, copilotVSCodeMarkerHead) || strings.Contains(raw, copilotVSCodeMarkerFoot) {
		if !copilotVSCodeCurrentManaged(raw) && !copilotVSCodeLegacyManaged(raw) {
			return false, os.ErrExist
		}
		start, end, _ := copilotVSCodeSpan(raw)
		next := raw[:start] + copilotVSCodeBlock(strings.Contains(raw[start:end], "(comma-added)")) + raw[end:]
		if next == raw {
			return false, nil
		}
		if err := writeCopilotVSCodeFileLocked(path, next); err != nil {
			return false, err
		}
		return true, nil
	}
	if strings.Contains(raw, copilotVSCodeProxyKey) || strings.Contains(raw, copilotVSCodeAuthKey) || strings.Contains(raw, copilotVSCodeCAPIKey) {
		return false, os.ErrExist
	}
	close := jsoncRootObjectEnd(raw)
	if close < 0 {
		return false, os.ErrInvalid
	}
	insert := jsoncLastTokenEnd(raw, close)
	before := raw[:close]
	commaAdded := insert > 0 && raw[insert-1] != '{' && raw[insert-1] != ','
	separator := ""
	if commaAdded {
		separator = ","
	}
	lineSep := "\n"
	if strings.Contains(raw, "\r\n") {
		lineSep = "\r\n"
	}
	newline := ""
	if !strings.HasSuffix(before, "\n") && !strings.HasSuffix(before, "\r") {
		newline = lineSep
	}
	before = raw[:insert] + separator + raw[insert:close]
	if !strings.HasSuffix(before, "\n") && !strings.HasSuffix(before, "\r") {
		newline = lineSep
	} else {
		newline = ""
	}
	next := before + newline + strings.ReplaceAll(copilotVSCodeBlock(commaAdded), "\n", lineSep) + lineSep + raw[close:]
	if err := writeCopilotVSCodeFileLocked(path, next); err != nil {
		return false, err
	}
	return true, nil
}

func removeCopilotVSCode() (bool, error) {
	copilotProjectWriteMu.Lock()
	defer copilotProjectWriteMu.Unlock()
	return removeCopilotVSCodeLocked()
}

func removeCopilotVSCodeLocked() (bool, error) {
	path := copilotVSCodeSettingsFile()
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	raw := string(contents)
	if !strings.Contains(raw, copilotVSCodeMarkerHead) && !strings.Contains(raw, copilotVSCodeMarkerFoot) {
		return false, nil
	}
	if !copilotVSCodeCurrentManaged(raw) && !copilotVSCodeLegacyManaged(raw) {
		return false, os.ErrExist
	}
	start, end, _ := copilotVSCodeSpan(raw)
	prefix, suffix := raw[:start], raw[end:]
	if strings.Contains(raw[start:end], "(comma-added)") {
		if comma := jsoncLastComma(prefix); comma >= 0 {
			prefix = prefix[:comma] + prefix[comma+1:]
		}
	}
	next := prefix + suffix
	if util.TryParseJsonc(next) == nil {
		return false, os.ErrInvalid
	}
	if err := writeCopilotVSCodeFileLocked(path, next); err != nil {
		return false, err
	}
	return true, nil
}

func jsoncLastComma(raw string) int {
	last := -1
	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inLineComment {
			if c == '\n' || c == '\r' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if c == '*' && i+1 < len(raw) && raw[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '/':
			if i+1 < len(raw) && raw[i+1] == '/' {
				inLineComment = true
				i++
			} else if i+1 < len(raw) && raw[i+1] == '*' {
				inBlockComment = true
				i++
			}
		case ',':
			last = i
		}
	}
	return last
}
