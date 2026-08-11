package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func detectVSCodeAgent(cli, configDir string, knownDirs []string, extensionID string) core.Detection {
	detection := detectAgent(cli, configDir, knownDirs, nil)
	if !vscodeExtensionInstalled(extensionID) {
		return detection
	}
	if detection.Source == "cli" {
		return core.Detection{Installed: true, Source: "cli+ide"}
	}
	return core.Detection{Installed: true, Source: "ide"}
}

func vscodeExtensionInstalled(id string) bool {
	for _, dir := range vscodeExtensionDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		obsolete := vscodeObsoleteExtensions(dir)
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			if entry.IsDir() && !obsolete[name] && (name == id || strings.HasPrefix(name, id+"-")) {
				return true
			}
		}
	}
	return false
}

func vscodeObsoleteExtensions(dir string) map[string]bool {
	raw, err := os.ReadFile(filepath.Join(dir, ".obsolete"))
	if err != nil {
		return nil
	}
	var obsolete map[string]bool
	if json.Unmarshal(raw, &obsolete) != nil {
		return nil
	}
	for key, removed := range obsolete {
		delete(obsolete, key)
		obsolete[strings.ToLower(key)] = removed
	}
	return obsolete
}

func vscodeExtensionDirs() []string {
	codeServerData := filepath.Join(util.Home(), ".local", "share")
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		codeServerData = dir
	}
	dirs := []string{
		filepath.Join(util.Home(), ".vscode", "extensions"),
		filepath.Join(util.Home(), ".vscode-insiders", "extensions"),
		filepath.Join(util.Home(), ".vscode-oss", "extensions"),
		filepath.Join(util.Home(), ".vscode-server", "extensions"),
		filepath.Join(util.Home(), ".vscode-server-insiders", "extensions"),
		filepath.Join(util.Home(), ".windsurf", "extensions"),
		filepath.Join(util.Home(), ".cursor", "extensions"),
		filepath.Join(codeServerData, "code-server", "extensions"),
	}
	if dir := os.Getenv("VSCODE_EXTENSIONS"); dir != "" {
		dirs = append(dirs, dir)
	}
	return dirs
}
