package util

import (
	"os"
	"path/filepath"
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
