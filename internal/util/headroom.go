package util

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HeadroomPaths contains only directories owned by Tokless.
type HeadroomPaths struct {
	Root   string
	UV     string
	Tools  string
	Bin    string
	Python string
}

func HeadroomPathsResolved() HeadroomPaths {
	root := filepath.Join(ToklessDataDir(), "headroom")
	bin := filepath.Join(root, "bin")
	uv := filepath.Join(root, "uv", "uv")
	if IsWin {
		uv += ".exe"
	}
	return HeadroomPaths{Root: root, UV: uv, Tools: filepath.Join(root, "tools"), Bin: bin, Python: filepath.Join(root, "python")}
}

func HeadroomBin() string {
	p := filepath.Join(HeadroomPathsResolved().Bin, "headroom")
	if IsWin {
		return p + ".exe"
	}
	return p
}

func HeadroomEnv() []string {
	p := HeadroomPathsResolved()
	return []string{
		"UV_TOOL_DIR=" + p.Tools,
		"UV_TOOL_BIN_DIR=" + p.Bin,
		"UV_PYTHON_INSTALL_DIR=" + p.Python,
		"UV_MANAGED_PYTHON=1",
	}
}

func HeadroomUVBootstrapEnv() []string {
	p := HeadroomPathsResolved()
	return append(HeadroomEnv(), "UV_INSTALL_DIR="+filepath.Dir(p.UV), "UV_NO_MODIFY_PATH=1")
}

func McpSpawnFor(toolID string) McpSpawn {
	return PickMcpSpawn(toolID)
}

func HeadroomInstalled() bool {
	info, err := os.Stat(HeadroomBin())
	return err == nil && !info.IsDir()
}

// HeadroomInstalledVersion returns the installed Headroom version.
func HeadroomInstalledVersion() *string {
	if v := headroomDistInfoVersion(); v != nil {
		return v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res := Run(HeadroomBin(), []string{"--version"}, RunOptions{Capture: true, Ctx: ctx})
	if res.Code != 0 {
		return nil
	}
	_, version, ok := strings.Cut(strings.TrimSpace(res.Stdout), "version ")
	if !ok || version == "" {
		return nil
	}
	return &version
}

func HeadroomMetadataVersion() *string { return headroomDistInfoVersion() }

// headroomUVToolDirs returns candidate uv tool directories: the UV_TOOL_DIR
// override, the uv default tool dir, then the Tokless-managed tool dir.
func headroomUVToolDirs() []string {
	var dirs []string
	if env := os.Getenv("UV_TOOL_DIR"); env != "" {
		dirs = append(dirs, env)
	}
	if dir := uvDefaultToolDir(); dir != "" {
		dirs = append(dirs, dir)
	}
	dirs = append(dirs, HeadroomPathsResolved().Tools)
	return dirs
}

func uvDefaultToolDir() string {
	if IsWin {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(Home(), "AppData", "Local")
		}
		return filepath.Join(base, "uv", "tools")
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "uv", "tools")
	}
	return filepath.Join(Home(), ".local", "share", "uv", "tools")
}

// headroomDistInfoVersion returns the highest installed dist-info version.
func headroomDistInfoVersion() *string {
	var best *string
	for _, tools := range headroomUVToolDirs() {
		roots := []string{
			filepath.Join(tools, "headroom-ai", "lib", "python*", "site-packages"),
			filepath.Join(tools, "headroom-ai", "Lib", "site-packages"),
		}
		for _, root := range roots {
			matches, _ := filepath.Glob(filepath.Join(root, "headroom_ai-*.dist-info"))
			for _, m := range matches {
				name := headroomDistInfoDirVersion(m)
				if name == "" {
					continue
				}
				if best == nil || SemverCompare(&name, best) > 0 {
					v := name
					best = &v
				}
			}
		}
	}
	return best
}

// headroomDistInfoDirVersion reads a dist-info directory's version from its
// METADATA file, falling back to the directory name.
func headroomDistInfoDirVersion(dir string) string {
	if v := headroomMetadataFileVersion(filepath.Join(dir, "METADATA")); v != "" {
		return v
	}
	base := filepath.Base(dir)
	return strings.TrimSuffix(strings.TrimPrefix(base, "headroom_ai-"), ".dist-info")
}

func headroomMetadataFileVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
