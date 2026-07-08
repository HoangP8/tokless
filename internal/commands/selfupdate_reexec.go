package commands

import (
	"os"
	"path/filepath"
	"strings"
	"github.com/HoangP8/tokless/internal/util"
)

const reexecEnvKey = "TOKLESS_REEXECED"

// reexecAfterSelfUpdate restarts from the on-disk binary so the rest of this
// run uses the version just installed.
func reexecAfterSelfUpdate() {
	if os.Getenv(reexecEnvKey) == "1" || os.Getenv("TOKLESS_TEST") == "1" {
		return
	}
	if err := reexecSelfImpl(); err != nil {
		util.L.Warn("could not restart with new binary — run " + util.C.Cyan("tokless") + " again (" + err.Error() + ")")
	}
}

// resolvedExecutable returns this binary's path (symlink-resolved when possible).
func resolvedExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil && real != "" {
		return real, nil
	}
	return exe, nil
}

// withEnvKV sets key=value in env (replace if present).
func withEnvKV(env []string, key, val string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			out = append(out, prefix+val)
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		out = append(out, prefix+val)
	}
	return out
}
