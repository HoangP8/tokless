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

// headroomDistInfoVersion returns the highest installed dist-info version.
func headroomDistInfoVersion() *string {
	p := HeadroomPathsResolved()
	roots := []string{
		filepath.Join(p.Tools, "headroom-ai", "lib", "python*", "site-packages"),
		filepath.Join(p.Tools, "headroom-ai", "Lib", "site-packages"),
	}
	var best *string
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "headroom_ai-*.dist-info"))
		for _, m := range matches {
			base := filepath.Base(m)
			name := strings.TrimSuffix(strings.TrimPrefix(base, "headroom_ai-"), ".dist-info")
			if name == "" {
				continue
			}
			if best == nil || SemverCompare(&name, best) > 0 {
				v := name
				best = &v
			}
		}
	}
	return best
}
