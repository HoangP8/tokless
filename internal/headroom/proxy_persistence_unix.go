//go:build !windows

package headroom

import (
	"os"
	"path/filepath"
)

func replaceProxyFile(tmpPath, path string) error {
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
