package util

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExecResult mirrors the TS { code, stdout, stderr } shape.
type ExecResult struct {
	Code   int
	Stdout string
	Stderr string
}

// RunOptions controls stdio handling for Run.
type RunOptions struct {
	Capture bool
	Quiet   bool
	Cwd     string
	Env     []string
}

// Run executes a command; Capture pipes stdio, Quiet discards it, else inherit.
func Run(cmd string, args []string, opts RunOptions) ExecResult {
	c := exec.Command(cmd, args...)
	if opts.Cwd != "" {
		c.Dir = opts.Cwd
	}
	if opts.Env != nil {
		c.Env = append(os.Environ(), opts.Env...)
	}
	var outBuf, errBuf bytes.Buffer
	if opts.Capture {
		c.Stdout = &outBuf
		c.Stderr = &errBuf
	} else if opts.Quiet {
		// discard
	} else {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
	}
	err := c.Run()
	res := ExecResult{Stdout: outBuf.String(), Stderr: errBuf.String()}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.Code = ee.ExitCode()
		} else {
			// spawn failure (ENOENT etc.)
			res.Code = 127
			res.Stderr += err.Error()
		}
		return res
	}
	res.Code = 0
	return res
}

// Which finds an executable on PATH, honoring PATHEXT on Windows.
func Which(bin string) string {
	pathEnv := os.Getenv("PATH")
	var exts []string
	sep := ":"
	if IsWin {
		sep = ";"
		pe := os.Getenv("PATHEXT")
		if pe == "" {
			pe = ".EXE;.CMD;.BAT"
		}
		exts = strings.Split(pe, ";")
	} else {
		exts = []string{""}
	}
	for _, dir := range strings.Split(pathEnv, sep) {
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			p := filepath.Join(dir, bin+ext)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return ""
}

// WhichAny returns the first found bin and its path.
func WhichAny(bins []string) (string, string) {
	for _, b := range bins {
		if p := Which(b); p != "" {
			return b, p
		}
	}
	return "", ""
}
