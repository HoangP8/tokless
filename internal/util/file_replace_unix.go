//go:build !windows

package util

import "os"

func replaceFile(tmpPath, path string) error {
	return os.Rename(tmpPath, path)
}
