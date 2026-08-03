//go:build !windows

package agents

import "os"

func replaceKiloFile(tmpPath, path string) error {
	return os.Rename(tmpPath, path)
}
